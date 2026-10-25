package agent

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/robfig/cron/v3"

	"backupper/internal/chunk"
	"backupper/internal/model"
	"backupper/internal/storage"
)

func writeJSONResp(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func httpErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(v)
}

// Handler returns the HTTP API protected by the bearer token.
func (a *Agent) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/info", a.hInfo)
	mux.HandleFunc("GET /api/sysinfo", func(w http.ResponseWriter, r *http.Request) { writeJSONResp(w, CollectSysInfo()) })
	mux.HandleFunc("PUT /api/sync", a.hSync)
	mux.HandleFunc("PUT /api/jobs/{id}", a.hSaveJob)
	mux.HandleFunc("DELETE /api/jobs/{id}", a.hDeleteJob)
	mux.HandleFunc("POST /api/jobs/{id}/run", a.hRunJob)
	mux.HandleFunc("POST /api/plan", a.hPlan)
	mux.HandleFunc("GET /api/runs", a.hRuns)
	mux.HandleFunc("GET /api/runs/{id}", a.hRun)
	mux.HandleFunc("POST /api/runs/{id}/cancel", a.hCancel)
	mux.HandleFunc("GET /api/active", func(w http.ResponseWriter, r *http.Request) { writeJSONResp(w, a.Active()) })
	mux.HandleFunc("POST /api/storages/test", a.hTestStorage)
	mux.HandleFunc("GET /api/backups", a.hBackups)
	mux.HandleFunc("DELETE /api/backups", a.hDeleteBackup)
	mux.HandleFunc("GET /api/download", a.hDownload)
	mux.HandleFunc("POST /api/probe", a.hProbe)
	mux.HandleFunc("POST /api/telegram/test", a.hTelegram)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := a.state.Config().Token
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			httpErr(w, http.StatusUnauthorized, errors.New("неверный токен агента"))
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (a *Agent) hInfo(w http.ResponseWriter, r *http.Request) {
	cfg := a.state.Config()
	writeJSONResp(w, model.AgentInfo{
		Version:  a.version,
		Sys:      CollectSysInfo(),
		Settings: cfg.Settings,
		Storages: cfg.Storages,
		Jobs:     a.JobViews(),
	})
}

func (a *Agent) hSync(w http.ResponseWriter, r *http.Request) {
	var p model.SyncPayload
	if err := decode(r, &p); err != nil {
		httpErr(w, 400, err)
		return
	}
	err := a.state.update(func(c *Config) error {
		if p.Settings.ServerName == "" {
			p.Settings.ServerName = c.Settings.ServerName
		}
		c.Settings = p.Settings
		c.Storages = p.Storages
		return nil
	})
	if err != nil {
		httpErr(w, 500, err)
		return
	}
	writeJSONResp(w, map[string]bool{"ok": true})
}

func validateJob(j *model.Job, others []model.Job) error {
	j.Name = strings.TrimSpace(j.Name)
	if j.Name == "" {
		return errors.New("укажите название задачи")
	}
	for _, o := range others {
		if o.ID != j.ID && Slug(o.Name) == Slug(j.Name) {
			return fmt.Errorf("задача с названием «%s» уже есть", o.Name)
		}
	}
	if len(j.Sources) == 0 {
		return errors.New("добавьте хотя бы один источник")
	}
	if len(j.Storages) == 0 {
		return errors.New("выберите хотя бы одно облако")
	}
	for i := range j.Sources {
		if j.Sources[i].ID == "" {
			j.Sources[i].ID = NewID()
		}
	}
	if j.Schedule.Enabled {
		expr, err := j.Schedule.CronExpr()
		if err != nil {
			return err
		}
		if _, err := cron.ParseStandard(expr); err != nil {
			return fmt.Errorf("неверное расписание: %v", err)
		}
	}
	if j.Keep < 0 {
		j.Keep = 0
	}
	// Keep mirrors the simple count so an older agent rotates the same way;
	// with calendar rules it is 0 and an older agent deletes nothing.
	j.Retention = j.Retention.Normalized()
	if j.Retention.Calendar() {
		j.Keep = 0
	} else if !j.Retention.Empty() {
		j.Keep = j.Retention.Last
	}
	j.Chunk = j.Chunk.Normalized()
	return nil
}

func (a *Agent) hSaveJob(w http.ResponseWriter, r *http.Request) {
	var j model.Job
	if err := decode(r, &j); err != nil {
		httpErr(w, 400, err)
		return
	}
	j.ID = r.PathValue("id")
	if j.ID == "" || j.ID == "new" {
		j.ID = NewID()
	}
	err := a.state.update(func(c *Config) error {
		if err := validateJob(&j, c.Jobs); err != nil {
			return err
		}
		for i := range c.Jobs {
			if c.Jobs[i].ID == j.ID {
				c.Jobs[i] = j
				return nil
			}
		}
		c.Jobs = append(c.Jobs, j)
		return nil
	})
	if err != nil {
		httpErr(w, 400, err)
		return
	}
	a.Reschedule()
	writeJSONResp(w, j)
}

func (a *Agent) hDeleteJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := a.state.update(func(c *Config) error {
		for i := range c.Jobs {
			if c.Jobs[i].ID == id {
				c.Jobs = append(c.Jobs[:i], c.Jobs[i+1:]...)
				return nil
			}
		}
		return errors.New("задача не найдена")
	})
	if err != nil {
		httpErr(w, 404, err)
		return
	}
	a.Reschedule()
	writeJSONResp(w, map[string]bool{"ok": true})
}

func (a *Agent) hRunJob(w http.ResponseWriter, r *http.Request) {
	run, err := a.Enqueue(r.PathValue("id"), "вручную")
	if err != nil {
		httpErr(w, 409, err)
		return
	}
	writeJSONResp(w, run)
}

func (a *Agent) hPlan(w http.ResponseWriter, r *http.Request) {
	var j model.Job
	if err := decode(r, &j); err != nil {
		httpErr(w, 400, err)
		return
	}
	writeJSONResp(w, a.PlanFor(j, r.URL.Query().Get("lang")))
}

func (a *Agent) hRuns(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 100
	}
	writeJSONResp(w, a.state.Runs(r.URL.Query().Get("job"), limit, false))
}

func (a *Agent) hRun(w http.ResponseWriter, r *http.Request) {
	run := a.state.Run(r.PathValue("id"))
	if run == nil {
		httpErr(w, 404, errors.New("запуск не найден"))
		return
	}
	writeJSONResp(w, run)
}

func (a *Agent) hCancel(w http.ResponseWriter, r *http.Request) {
	writeJSONResp(w, map[string]bool{"ok": a.Cancel(r.PathValue("id"))})
}

func (a *Agent) hTestStorage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StorageID string `json:"storageId"`
		Speed     bool   `json:"speed"`
	}
	if err := decode(r, &req); err != nil {
		httpErr(w, 400, err)
		return
	}
	sc, ok := a.state.Storage(req.StorageID)
	if !ok {
		httpErr(w, 404, errors.New("облако не найдено на агенте — синхронизируйте настройки"))
		return
	}
	var size int64
	if req.Speed {
		size = 16 * chunk.MB
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	res := storage.Test(ctx, sc, size)
	if res.OK {
		a.state.RecordSpeed(sc.ID, res.SpeedBps, res.MaxFile)
	}
	writeJSONResp(w, res)
}

func (a *Agent) storageFor(id string) (model.Storage, storage.Storage, error) {
	sc, ok := a.state.Storage(id)
	if !ok {
		return sc, nil, errors.New("облако не найдено")
	}
	st, err := storage.New(sc)
	return sc, st, err
}

// safeRel rejects paths that try to leave the storage root.
func safeRel(p string) (string, error) {
	c := strings.TrimPrefix(storage.Clean(p), "/")
	if c == "" || strings.Contains(p, "..") {
		return "", errors.New("неверный путь")
	}
	return c, nil
}

func (a *Agent) hBackups(w http.ResponseWriter, r *http.Request) {
	job, ok := a.state.Job(r.URL.Query().Get("job"))
	if !ok {
		httpErr(w, 404, errors.New("задача не найдена"))
		return
	}
	sc, st, err := a.storageFor(r.URL.Query().Get("storage"))
	if err != nil {
		httpErr(w, 400, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	rel := a.jobDir(job)
	jobDir := storage.Clean(path.Join(sc.Root, rel))
	entries, err := st.List(ctx, jobDir)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSONResp(w, []model.BackupEntry{})
			return
		}
		httpErr(w, 502, err)
		return
	}
	var names []string
	for _, e := range entries {
		if _, err := time.Parse(runDirLayout, e.Name); e.IsDir && err == nil {
			names = append(names, e.Name)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	if len(names) > 60 {
		names = names[:60]
	}
	out := []model.BackupEntry{}
	for _, n := range names {
		be := model.BackupEntry{Dir: path.Join(rel, n), Name: n}
		be.Started, _ = time.ParseInLocation(runDirLayout, n, time.Local)
		if m, err := readManifest(ctx, st, path.Join(jobDir, n)); err == nil {
			be.Status, be.Files, be.Complete = m.Status, m.Files, true
			for _, f := range m.Files {
				be.Size += f.Size
			}
		} else {
			be.Status = "incomplete"
		}
		out = append(out, be)
	}
	writeJSONResp(w, out)
}

func (a *Agent) hDeleteBackup(w http.ResponseWriter, r *http.Request) {
	sc, st, err := a.storageFor(r.URL.Query().Get("storage"))
	if err != nil {
		httpErr(w, 400, err)
		return
	}
	rel, err := safeRel(r.URL.Query().Get("dir"))
	if err != nil || strings.Count(rel, "/") < 2 {
		httpErr(w, 400, errors.New("неверный путь копии"))
		return
	}
	if err := st.Delete(r.Context(), storage.Clean(path.Join(sc.Root, rel))); err != nil {
		httpErr(w, 502, err)
		return
	}
	writeJSONResp(w, map[string]bool{"ok": true})
}

// hDownload streams a file glued back from its parts, optionally decrypted.
func (a *Agent) hDownload(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sc, st, err := a.storageFor(q.Get("storage"))
	if err != nil {
		httpErr(w, 400, err)
		return
	}
	rel, err := safeRel(q.Get("dir"))
	if err != nil {
		httpErr(w, 400, err)
		return
	}
	dir := storage.Clean(path.Join(sc.Root, rel))
	man, err := readManifest(r.Context(), st, dir)
	if err != nil {
		httpErr(w, 404, fmt.Errorf("manifest.json: %w", err))
		return
	}
	var file *model.RunFile
	for i := range man.Files {
		if man.Files[i].Name == q.Get("file") {
			file = &man.Files[i]
		}
	}
	if file == nil || len(file.Parts) == 0 {
		httpErr(w, 404, errors.New("файл не найден в копии"))
		return
	}

	pr, pw := io.Pipe()
	go func() {
		for _, p := range file.Parts {
			rc, err := st.Get(r.Context(), path.Join(dir, p.Name))
			if err != nil {
				pw.CloseWithError(fmt.Errorf("%s: %w", p.Name, err))
				return
			}
			_, err = io.Copy(pw, rc)
			rc.Close()
			if err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		pw.Close()
	}()
	defer pr.Close()

	var body io.Reader = pr
	name := file.Name
	if file.Encrypted && q.Get("decrypt") == "1" {
		pass := a.state.Config().Settings.EncryptionPassword
		id, err := age.NewScryptIdentity(pass)
		if err != nil {
			httpErr(w, 400, err)
			return
		}
		dr, err := age.Decrypt(pr, id)
		if err != nil {
			httpErr(w, 400, fmt.Errorf("расшифровка: %w (пароль шифрования изменился?)", err))
			return
		}
		body = dr
		name = strings.TrimSuffix(name, ".age")
	} else {
		w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	_, _ = io.Copy(w, body)
}

func (a *Agent) hProbe(w http.ResponseWriter, r *http.Request) {
	var src model.Source
	if err := decode(r, &src); err != nil {
		httpErr(w, 400, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	writeJSONResp(w, a.probe(ctx, src))
}

func (a *Agent) hTelegram(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	t := a.state.Config().Settings.Telegram
	if err := SendTelegram(ctx, t, "🔔 Backupper: проверка уведомлений с сервера <b>"+a.serverName()+"</b>"); err != nil {
		httpErr(w, 400, err)
		return
	}
	writeJSONResp(w, map[string]bool{"ok": true})
}
