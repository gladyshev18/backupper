package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"filippo.io/age"
	"github.com/klauspost/compress/gzip"

	"backupper/internal/chunk"
	"backupper/internal/model"
	"backupper/internal/storage"
)

const runDirLayout = "2006-01-02_15-04-05"

func (a *Agent) logf(r *model.Run, level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	a.state.WithRun(r, func(r *model.Run) {
		r.Log = append(r.Log, model.LogLine{Time: time.Now(), Level: level, Msg: msg})
		if len(r.Log) > 400 {
			r.Log = append(r.Log[:50], r.Log[len(r.Log)-300:]...)
		}
	})
}

func (a *Agent) serverName() string {
	c := a.state.Config()
	if c.Settings.ServerName != "" {
		return c.Settings.ServerName
	}
	h, _ := os.Hostname()
	return h
}

// JobDir is the job's folder relative to the storage root: <server>/<job>,
// or sites/<site> for a hosting site (it does not belong to the relaying server).
func (a *Agent) jobDir(job model.Job) string {
	if job.Kind == model.JobSite {
		return path.Join(model.SitesFolder, Slug(job.Name))
	}
	return path.Join(Slug(a.serverName()), Slug(job.Name))
}

func (a *Agent) openDests(job model.Job, runDir string) ([]dest, error) {
	if len(job.Storages) == 0 {
		return nil, errors.New("не выбрано ни одного облака")
	}
	var out []dest
	for _, id := range job.Storages {
		sc, ok := a.state.Storage(id)
		if !ok {
			return nil, fmt.Errorf("облако %s не найдено в настройках агента — откройте сервер в панели, чтобы синхронизировать", id)
		}
		st, err := storage.New(sc)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", sc.Name, err)
		}
		out = append(out, dest{id: id, st: st, dir: storage.Clean(path.Join(sc.Root, runDir))})
	}
	return out, nil
}

// PlanFor computes the chunk plan for a job with the current server state.
func (a *Agent) PlanFor(job model.Job, lang string) model.ChunkPlan {
	si := CollectSysInfo()
	var speeds []float64
	var maxFile int64
	for _, id := range job.Storages {
		st := a.state.Stats(id)
		speeds = append(speeds, st.SpeedBps)
		mf := st.MaxFile
		if sc, ok := a.state.Storage(id); ok && mf == 0 {
			mf = storage.KnownMaxFile(sc.Kind)
		}
		if mf > 0 && (maxFile == 0 || mf < maxFile) {
			maxFile = mf
		}
	}
	return chunk.Plan(chunk.Input{
		Settings:     job.Chunk,
		MemAvailable: si.MemAvailable,
		DiskFree:     si.DiskFree,
		SpeedBps:     chunk.CombinedSpeed(speeds),
		ExpectedSize: a.state.ExpectedSize(job.ID),
		MaxFileSize:  maxFile,
		Lang:         lang,
	})
}

type countReader struct {
	r  io.Reader
	fn func(n int)
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.fn(n)
	}
	return n, err
}

type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 4096 {
		t.buf = t.buf[len(t.buf)-4096:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

// execute performs one run of a job.
func (a *Agent) execute(ctx context.Context, run *model.Run, job model.Job) {
	cfg := a.state.Config()
	a.state.WithRun(run, func(r *model.Run) {
		r.Status = model.RunRunning
		r.StartedAt = time.Now()
		r.Progress = &model.Progress{Stage: "Подготовка", Since: r.StartedAt}
	})
	a.logf(run, "info", "Запуск задачи «%s» (%s)", job.Name, run.Trigger)

	status, errMsg := a.doRun(ctx, run, job, cfg)

	a.state.WithRun(run, func(r *model.Run) {
		r.Status = status
		r.Error = errMsg
		r.FinishedAt = time.Now()
		r.Progress = nil
	})
	dur := ruDuration(run.FinishedAt.Sub(run.StartedAt))
	switch status {
	case model.RunSuccess:
		a.logf(run, "info", "Готово за %s, загружено %s", dur, chunk.HumanBytes(run.Bytes))
	case model.RunWarning:
		a.logf(run, "warn", "Завершено с предупреждениями за %s", dur)
	default:
		a.logf(run, "error", "Ошибка: %s", errMsg)
	}
	a.state.SaveHistory()
	a.notify(job, run)
}

func ruDuration(d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	switch {
	case s < 60:
		return fmt.Sprintf("%d с", s)
	case s < 3600:
		return fmt.Sprintf("%d мин %d с", s/60, s%60)
	}
	return fmt.Sprintf("%d ч %d мин", s/3600, s%3600/60)
}

func (a *Agent) doRun(ctx context.Context, run *model.Run, job model.Job, cfg Config) (string, string) {
	if job.Encrypt && cfg.Settings.EncryptionPassword == "" {
		return model.RunFailed, "включено шифрование, но пароль шифрования не задан в настройках панели"
	}
	if len(job.Sources) == 0 {
		return model.RunFailed, "в задаче нет источников"
	}
	if err := needsEncryption(job, cfg.Storages); err != nil {
		return model.RunFailed, err.Error()
	}
	started := time.Now()
	runDir := path.Join(a.jobDir(job), started.Format(runDirLayout))
	dests, err := a.openDests(job, runDir)
	if err != nil {
		return model.RunFailed, err.Error()
	}
	plan := a.PlanFor(job, "ru")
	// The last successful run predicts this one; sources that can count their
	// size refine it per file (est).
	lastRaw, lastFiles := a.state.LastRaw(job.ID)
	est := &runEstimate{files: lastFiles}
	a.state.WithRun(run, func(r *model.Run) {
		r.EstRawBytes = lastRaw
		r.RemoteDir = runDir
		r.Storages = job.Storages
		r.ChunkSize = plan.Size
		r.ChunkBuffer = plan.Buffer
		r.Uploads = map[string]model.UploadStat{}
	})
	a.logf(run, "info", "Размер куска: %s, буфер: %s, пик памяти ~%s", chunk.HumanBytes(plan.Size), map[string]string{model.BufferMemory: "память", model.BufferDisk: "диск"}[plan.Buffer], chunk.HumanBytes(plan.PeakMemory))
	for _, r := range plan.Reasons {
		a.logf(run, "info", "  • %s", r)
	}
	for _, w := range plan.Warnings {
		a.logf(run, "warn", "  ⚠ %s", w)
	}

	for _, d := range dests {
		mctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		err := d.st.MkdirAll(mctx, d.dir)
		cancel()
		if err != nil {
			return model.RunFailed, fmt.Sprintf("облако %s: %v", d.id, err)
		}
	}
	if need := a.state.LastBytes(job.ID); need > 0 {
		for _, d := range dests {
			a.checkSpace(ctx, run, d, need, job.Policy())
		}
	}

	var recipient age.Recipient
	if job.Encrypt {
		rc, err := age.NewScryptRecipient(cfg.Settings.EncryptionPassword)
		if err != nil {
			return model.RunFailed, err.Error()
		}
		rc.SetWorkFactor(16)
		recipient = rc
	}

	level := job.Compression
	if level < 1 || level > 9 {
		level = 6
	}

	warnings, failures := 0, 0
	usedNames := map[string]int{}
	for _, src := range job.Sources {
		if ctx.Err() != nil {
			break
		}
		streams, cleanupSrc, err := a.planSource(ctx, src, func(lvl, f string, args ...any) {
			if lvl == "warn" {
				warnings++
			}
			a.logf(run, lvl, f, args...)
		})
		if err != nil {
			failures++
			a.logf(run, "error", "%s: %v", defaultName(src), err)
			a.state.WithRun(run, func(r *model.Run) {
				r.Files = append(r.Files, model.RunFile{Name: Slug(defaultName(src)), Source: defaultName(src), Error: err.Error()})
			})
			continue
		}
		for _, s := range streams {
			if ctx.Err() != nil {
				break
			}
			name := s.file + ".gz"
			if job.Encrypt {
				name += ".age"
			}
			if n := usedNames[name]; n > 0 {
				name = strings.Replace(name, ".", fmt.Sprintf("_%d.", n+1), 1)
			}
			usedNames[name]++
			rf, warn := a.runStream(ctx, run, s, name, dests, plan, level, recipient, est)
			rf.Source = defaultName(src)
			rf.Encrypted = job.Encrypt
			if rf.Error != "" {
				failures++
			} else if warn {
				warnings++
			}
			a.state.WithRun(run, func(r *model.Run) {
				r.Files = append(r.Files, rf)
				r.Bytes += rf.Size
				r.RawBytes += rf.RawSize
				// The file is done: its real size replaces the guess in the total, and its
				// bytes now count in the run totals, not in the progress (the UI adds both).
				if p := r.Progress; p != nil {
					if r.EstRawBytes > 0 {
						r.EstRawBytes += rf.RawSize - p.EstRaw
					}
					p.EstRaw, p.RawBytes, p.Bytes = 0, 0, 0
				}
			})
		}
		cleanupSrc()
	}

	if ctx.Err() != nil {
		a.cleanupDir(dests)
		return model.RunCanceled, "отменено"
	}

	status := model.RunSuccess
	switch {
	case failures > 0:
		status = model.RunFailed
	case warnings > 0:
		status = model.RunWarning
	}

	// Manifest lets the panel list and restore backups straight from the cloud.
	snap := a.state.Run(run.ID)
	man := model.Manifest{Version: 1, Server: a.serverName(), JobID: job.ID, JobName: job.Name, Status: status, StartedAt: started, Finished: time.Now(), Files: snap.Files}
	mb, _ := json.MarshalIndent(man, "", "  ")
	for _, d := range dests {
		mctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		err := d.st.Put(mctx, path.Join(d.dir, "manifest.json"), bytes.NewReader(mb), int64(len(mb)))
		cancel()
		if err != nil {
			a.logf(run, "error", "manifest.json → %s: %v", d.id, err)
			status = model.RunFailed
		}
	}

	// Remember upload speed per destination for the next chunk plan.
	snap = a.state.Run(run.ID)
	for id, u := range snap.Uploads {
		if u.Seconds > 1 && u.Bytes > 4*chunk.MB {
			a.state.RecordSpeed(id, float64(u.Bytes)/u.Seconds, 0)
			a.logf(run, "info", "Скорость загрузки в %s: %s/с", a.storageName(id), chunk.HumanBytes(int64(float64(u.Bytes)/u.Seconds)))
		}
	}

	if pol := job.Policy(); status != model.RunFailed && !pol.Empty() {
		for _, d := range dests {
			if why := rotationBlocked(d); why != "" {
				a.logf(run, "warn", "Ротация в %s пропущена: %s", a.storageName(d.id), why)
				continue
			}
			a.rotate(ctx, run, d, pol, false)
		}
	}

	if status == model.RunFailed {
		var errs []string
		for _, f := range snap.Files {
			if f.Error != "" {
				errs = append(errs, f.Source+": "+f.Error)
			}
		}
		if len(errs) == 0 {
			errs = append(errs, "не удалось записать manifest.json")
		}
		return status, strings.Join(errs, "; ")
	}
	return status, ""
}

// rotationBlocked is the reason a storage cannot delete old copies (Telegram
// may refuse to delete messages older than 48 hours).
func rotationBlocked(d dest) string {
	if rb, ok := d.st.(interface{ RotationBlocked() string }); ok {
		return rb.RotationBlocked()
	}
	return ""
}

func (a *Agent) storageName(id string) string {
	if s, ok := a.state.Storage(id); ok {
		return s.Name
	}
	return id
}

func (a *Agent) cleanupDir(dests []dest) {
	for _, d := range dests {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		_ = d.st.Delete(ctx, d.dir)
		cancel()
	}
}

// runEstimate is the expected size of a run, from its previous successful run.
type runEstimate struct {
	files map[string]int64 // uncompressed size per file name
}

// fileEst returns the expected size of a file: counted by the source when it
// knows it, else the size from the last run.
func (e *runEstimate) fileEst(name string, counted int64) int64 {
	if counted > 0 {
		return counted
	}
	return e.files[name]
}

// setFileEst records the expected size of the running file and moves the run total
// by the difference from the last run (only when there is a run total to correct).
func setFileEst(r *model.Run, e *runEstimate, name string, n int64) {
	if r.Progress == nil || n <= 0 || r.Progress.EstRaw == n {
		return
	}
	if r.EstRawBytes > 0 {
		prev := r.Progress.EstRaw
		if prev == 0 {
			prev = e.files[name]
		}
		r.EstRawBytes += n - prev
	}
	r.Progress.EstRaw = n
}

// runStream runs one dump command and streams it compressed (and encrypted) to the clouds.
func (a *Agent) runStream(parent context.Context, run *model.Run, s stream, name string, dests []dest, plan model.ChunkPlan, level int, recipient age.Recipient, est *runEstimate) (rf model.RunFile, warn bool) {
	rf = model.RunFile{Name: name}
	a.logf(run, "info", "%s → %s", s.label, name)
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	var mu sync.Mutex
	lastT, lastB := time.Now(), int64(0)
	up := newUploader(ctx, dests, name, plan, func(l, f string, args ...any) { a.logf(run, l, f, args...) })
	// Parts of a broken file cannot be restored and only take cloud space
	// (a cancelled run removes its whole folder instead).
	defer func() {
		if rf.Error != "" && parent.Err() == nil {
			if n, size := up.Purge(); n > 0 {
				a.logf(run, "info", "%s: удалены уже загруженные части (%d шт., %s) — без остальных они бесполезны", name, n, chunk.HumanBytes(size))
			}
		}
	}()
	up.onUp = func(id string, n int64, d time.Duration) {
		a.state.WithRun(run, func(r *model.Run) {
			st := r.Uploads[id]
			st.Bytes += n
			st.Seconds += d.Seconds()
			r.Uploads[id] = st
			if r.Progress != nil {
				r.Progress.Bytes += n / int64(len(dests))
				if d > 0 {
					r.Progress.SpeedBps = float64(n) / d.Seconds()
				}
			}
		})
	}
	up.onPart = func(p int) {
		a.state.WithRun(run, func(r *model.Run) {
			if r.Progress != nil {
				r.Progress.Part = p
				r.Progress.Stage = "Выгрузка"
			}
		})
	}

	var out io.Writer = up
	var enc io.WriteCloser
	var err error
	if recipient != nil {
		enc, err = age.Encrypt(up, recipient)
		if err != nil {
			up.Abort(err)
			rf.Error = err.Error()
			return rf, false
		}
		out = enc
	}

	a.state.WithRun(run, func(r *model.Run) {
		r.Progress = &model.Progress{Stage: "Дамп", Source: s.label, Since: time.Now()}
		setFileEst(r, est, name, est.fileEst(name, 0))
	})
	src, err := s.start(ctx, func(l, f string, args ...any) { a.logf(run, l, f, args...) })
	if err != nil {
		up.Abort(err)
		rf.Error = err.Error()
		a.logf(run, "error", "%s", rf.Error)
		return rf, false
	}
	defer src.cleanup()

	// Dumps from commands are compressed here; sites on hosting send gzip already.
	var gz io.WriteCloser = nopCloser{out}
	if !src.precompressed {
		gz, _ = gzip.NewWriterLevel(out, level)
	}
	counter := &countReader{r: src.r, fn: func(n int) {
		mu.Lock()
		lastB += int64(n)
		cur := lastB
		mu.Unlock()
		if time.Since(lastT) > 500*time.Millisecond {
			lastT = time.Now()
			if src.raw != nil {
				cur = src.raw()
			}
			var counted int64
			if src.est != nil {
				counted = src.est()
			}
			a.state.WithRun(run, func(r *model.Run) {
				if r.Progress != nil {
					r.Progress.RawBytes = cur
					setFileEst(r, est, name, est.fileEst(name, counted))
				}
			})
		}
	}}
	raw, copyErr := io.Copy(gz, counter)
	if copyErr == nil {
		copyErr = gz.Close()
	}
	if copyErr == nil && enc != nil {
		copyErr = enc.Close()
	}
	if copyErr != nil {
		cancel() // stop the dump: nobody reads its output anymore
	}
	warn, waitErr := src.wait()
	if src.raw != nil {
		raw = src.raw()
	}
	rf.RawSize = raw

	if copyErr != nil {
		up.Abort(copyErr)
		rf.Error = copyErr.Error()
		if upErr := up.failed(); upErr != nil {
			rf.Error = upErr.Error()
		}
		a.logf(run, "error", "%s: %s", name, rf.Error)
		return rf, false
	}
	if waitErr != nil {
		up.Abort(waitErr)
		rf.Error = waitErr.Error()
		a.logf(run, "error", "%s", rf.Error)
		return rf, false
	}

	a.state.WithRun(run, func(r *model.Run) {
		if r.Progress != nil {
			r.Progress.Stage = "Завершение выгрузки"
		}
	})
	parts, sum, err := up.Finish()
	if err != nil {
		rf.Error = err.Error()
		a.logf(run, "error", "%s: %v", name, err)
		return rf, false
	}
	rf.Parts = parts
	rf.SHA256 = sum
	for _, p := range parts {
		rf.Size += p.Size
	}
	a.logf(run, "info", "%s: %s → %s, частей: %d", name, chunk.HumanBytes(raw), chunk.HumanBytes(rf.Size), len(parts))
	return rf, warn
}

// rotate applies the job's retention policy to its folder in one cloud: complete
// copies kept by no rule and broken ones are removed. Before the upload (early)
// it only frees space and spares the newest complete copy, because the current
// run may still fail. It returns the number of removed folders.
func (a *Agent) rotate(ctx context.Context, run *model.Run, d dest, pol model.Retention, early bool) int {
	jobDir := path.Dir(d.dir)
	current := path.Base(d.dir)
	lctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	entries, err := d.st.List(lctx, jobDir)
	cancel()
	if err != nil {
		a.logf(run, "warn", "ротация в %s: %v", a.storageName(d.id), err)
		return 0
	}
	type folder struct {
		name string
		t    time.Time
		good bool
	}
	var dirs []folder
	for _, e := range entries {
		if e.IsDir {
			if t, err := time.Parse(runDirLayout, e.Name); err == nil {
				dirs = append(dirs, folder{name: e.Name, t: t})
			}
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].t.After(dirs[j].t) })
	var times []time.Time
	var goodIdx []int
	spare := -1 // the newest complete copy besides the current one
	for i := range dirs {
		f := &dirs[i]
		f.good = f.name == current
		if !f.good {
			man, err := readManifest(ctx, d.st, path.Join(jobDir, f.name))
			f.good = err == nil && (man.Status == model.RunSuccess || man.Status == model.RunWarning)
			if f.good && spare < 0 {
				spare = i
			}
		}
		if f.good {
			goodIdx = append(goodIdx, i)
			times = append(times, f.t)
		}
	}
	keep := make([]model.KeepReason, len(dirs))
	for k, r := range pol.Select(times) {
		keep[goodIdx[k]] = r
	}
	if early && spare >= 0 && keep[spare] == 0 {
		keep[spare] = model.KeepLast
	}
	// Broken runs newer than the current one cannot exist; older broken ones go.
	removed := 0
	var kept [5]int
	for i, f := range dirs {
		if f.name == current || keep[i] != 0 {
			for b := range kept {
				if keep[i]&(1<<b) != 0 {
					kept[b]++
				}
			}
			continue
		}
		dctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		err := d.st.Delete(dctx, path.Join(jobDir, f.name))
		cancel()
		if err != nil {
			a.logf(run, "warn", "не удалось удалить старую копию %s: %v", f.name, err)
			continue
		}
		removed++
	}
	if removed == 0 {
		return 0
	}
	if early {
		a.logf(run, "info", "Освобождаю место в %s заранее: удалено копий, которые всё равно ушли бы после бэкапа: %d", a.storageName(d.id), removed)
		return removed
	}
	detail := ""
	if pol.Calendar() {
		var by []string
		for b, label := range []string{"последних", "дневных", "недельных", "месячных", "годовых"} {
			if kept[b] > 0 {
				by = append(by, fmt.Sprintf("%s: %d", label, kept[b]))
			}
		}
		detail = " (" + strings.Join(by, ", ") + ")"
	}
	a.logf(run, "info", "Ротация %s: удалено старых копий: %d, хранится: %d%s", a.storageName(d.id), removed, len(dirs)-removed, detail)
	return removed
}

// checkSpace compares free cloud space with the size of the previous copy. When
// it is short, copies the retention would delete after this run go right away.
func (a *Agent) checkSpace(ctx context.Context, run *model.Run, d dest, need int64, pol model.Retention) {
	free := func() (int64, bool) {
		qctx, cancel := context.WithTimeout(ctx, time.Minute)
		defer cancel()
		q, err := d.st.Quota(qctx)
		if err != nil || q.Total <= 0 {
			return 0, false
		}
		return q.Total - q.Used, true
	}
	want := need + need/10
	f, ok := free()
	if !ok || f >= want {
		return
	}
	name := a.storageName(d.id)
	a.logf(run, "warn", "В %s свободно %s, а прошлая копия заняла %s", name, chunk.HumanBytes(f), chunk.HumanBytes(need))
	if !pol.Empty() && a.rotate(ctx, run, d, pol, true) > 0 {
		if f, ok = free(); ok && f >= want {
			a.logf(run, "info", "Теперь в %s свободно %s — должно хватить", name, chunk.HumanBytes(f))
			return
		}
	}
	a.logf(run, "warn", "Места в %s может не хватить: освободите облако или храните меньше копий. Если архив стал меньше прошлого (например, после новых исключений), бэкап пройдёт", name)
}

func readManifest(ctx context.Context, st storage.Storage, dir string) (model.Manifest, error) {
	var m model.Manifest
	gctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	rc, err := st.Get(gctx, path.Join(dir, "manifest.json"))
	if err != nil {
		return m, err
	}
	defer rc.Close()
	err = json.NewDecoder(io.LimitReader(rc, 8<<20)).Decode(&m)
	return m, err
}
