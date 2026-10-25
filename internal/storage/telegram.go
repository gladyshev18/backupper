package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"backupper/internal/model"
)

// TelegramMaxFile is the upload limit of a Bot API server in --local mode.
const TelegramMaxFile = 2000 << 20

// Telegram keeps backups as documents in a chat (usually a private channel)
// through an own Bot API server. The Bot API cannot read chat history, so the
// folder tree lives in a local index on the agent; every finished copy also
// carries it in its manifest.json, and a pinned message per server lists the copies.
//
// During a run each part goes up as a silent "uploading" message. When the
// manifest arrives the parts are re-sent by file_id (no second upload) as albums
// captioned "part N of M", the temporary messages are deleted and manifest.json
// is posted last: a copy without a manifest message is incomplete.
type Telegram struct {
	id    string
	token string
	chat  string
	root  string
	tr    tgTransport
	ix    *tgIndex
}

func newTelegram(s model.Storage) (*Telegram, error) {
	t, err := tgClient(s)
	if err != nil {
		return nil, err
	}
	if s.ChatID == "" {
		return nil, errors.New("не указан канал Telegram")
	}
	t.ix = tgIndexFor(s.ID, s.ChatID)
	return t, nil
}

// tgClient can only call the Bot API: no chat and no index.
func tgClient(s model.Storage) (*Telegram, error) {
	if s.Token == "" {
		return nil, errors.New("не указан токен бота Telegram")
	}
	tr, err := transportFor(s.TgServer)
	if err != nil {
		return nil, err
	}
	return &Telegram{id: s.ID, token: s.Token, chat: s.ChatID, root: Clean(s.Root), tr: tr}, nil
}

// ---- Bot API calls ----

type tgError struct {
	Code       int
	Desc       string
	RetryAfter int
}

func (e *tgError) Error() string {
	switch e.Code {
	case 401:
		return "Telegram: неверный токен бота или бот не переведён на свой сервер Bot API (logOut)"
	case 413:
		return "Telegram: файл слишком большой (HTTP 413) — уменьшите размер куска"
	}
	return fmt.Sprintf("Telegram: %s (%d)", e.Desc, e.Code)
}

func tgIs(err error, text string) bool {
	var te *tgError
	return errors.As(err, &te) && strings.Contains(strings.ToLower(te.Desc), text)
}

// redacted hides the bot token that net/http puts into URL errors.
type redacted struct {
	msg string
	err error
}

func (r *redacted) Error() string { return r.msg }
func (r *redacted) Unwrap() error { return r.err }

type tgBody func() (io.Reader, int64, string, error)

func jsonBody(params map[string]any) tgBody {
	if params == nil {
		params = map[string]any{}
	}
	b, _ := json.Marshal(params)
	return func() (io.Reader, int64, string, error) {
		return bytes.NewReader(b), int64(len(b)), "application/json", nil
	}
}

// fileBody builds a multipart request with a known length around the part, so
// the part is streamed from its buffer instead of being copied once more.
func fileBody(fields map[string]string, field, name string, r io.ReadSeeker, size int64) tgBody {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		_ = mw.WriteField(k, fields[k])
	}
	_, _ = mw.CreateFormFile(field, name)
	n := buf.Len()
	_ = mw.Close()
	head := append([]byte(nil), buf.Bytes()[:n]...)
	tail := append([]byte(nil), buf.Bytes()[n:]...)
	ctype := mw.FormDataContentType()
	return func() (io.Reader, int64, string, error) {
		if _, err := r.Seek(0, io.SeekStart); err != nil {
			return nil, 0, "", err
		}
		return io.MultiReader(bytes.NewReader(head), io.LimitReader(r, size), bytes.NewReader(tail)), int64(len(head)) + size + int64(len(tail)), ctype, nil
	}
}

// do calls a Bot API method and waits out flood limits (429 with retry_after).
func (t *Telegram) do(ctx context.Context, method string, body tgBody, out any) error {
	for attempt := 0; ; attempt++ {
		r, n, ctype, err := body()
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.tr.base()+"/bot"+t.token+"/"+method, r)
		if err != nil {
			return &redacted{msg: strings.ReplaceAll(err.Error(), t.token, "…"), err: err}
		}
		req.ContentLength = n
		req.Header.Set("Content-Type", ctype)
		resp, err := t.tr.client().Do(req)
		if err != nil {
			return &redacted{msg: strings.ReplaceAll(err.Error(), t.token, "…"), err: err}
		}
		var res struct {
			OK          bool            `json:"ok"`
			Result      json.RawMessage `json:"result"`
			ErrorCode   int             `json:"error_code"`
			Description string          `json:"description"`
			Parameters  struct {
				RetryAfter int `json:"retry_after"`
			} `json:"parameters"`
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if err != nil {
			return err
		}
		if json.Unmarshal(raw, &res) != nil {
			res.Description = strings.TrimSpace(string(raw))
			if len(res.Description) > 300 {
				res.Description = res.Description[:300]
			}
		}
		if res.OK {
			if out != nil {
				return json.Unmarshal(res.Result, out)
			}
			return nil
		}
		if res.ErrorCode == 0 {
			res.ErrorCode = resp.StatusCode
		}
		te := &tgError{Code: res.ErrorCode, Desc: res.Description, RetryAfter: res.Parameters.RetryAfter}
		if te.Code != 429 || attempt >= 6 {
			return te
		}
		wait := time.Duration(min(max(te.RetryAfter, 1), 600)+1) * time.Second
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (t *Telegram) call(ctx context.Context, method string, params map[string]any, out any) error {
	return t.do(ctx, method, jsonBody(params), out)
}

type tgMessage struct {
	MessageID int   `json:"message_id"`
	Date      int64 `json:"date"`
	Document  *struct {
		FileID   string `json:"file_id"`
		FileSize int64  `json:"file_size"`
	} `json:"document"`
	CaptionEntities []struct {
		Type   string `json:"type"`
		Offset int    `json:"offset"`
		Length int    `json:"length"`
	} `json:"caption_entities"`
}

// ---- local index ----

type tgFile struct {
	MessageID int    `json:"messageId"`
	FileID    string `json:"fileId"`
	Size      int64  `json:"size"`
	Staging   bool   `json:"staging,omitempty"` // temporary message while the run uploads
	Data      []byte `json:"data,omitempty"`    // small files (manifest.json) are kept here too
}

type tgRun struct {
	Created time.Time          `json:"created"`
	Files   map[string]*tgFile `json:"files"`
	Done    bool               `json:"done"` // manifest posted
	Status  string             `json:"status,omitempty"`
	JobName string             `json:"jobName,omitempty"`
	Bytes   int64              `json:"bytes,omitempty"`
	Parts   int                `json:"parts,omitempty"`
}

// tgProbe is a service message sent once; deleting it two days later shows whether
// the bot may delete messages older than 48 hours, which rotation depends on.
type tgProbe struct {
	MessageID int       `json:"messageId"`
	Sent      time.Time `json:"sent"`
	Result    string    `json:"result,omitempty"` // "" unknown, ok, no
}

type tgIndex struct {
	mu   sync.Mutex
	file string

	Chat  string            `json:"chat"`
	Runs  map[string]*tgRun `json:"runs"` // folder → its messages
	TOC   map[string]int    `json:"toc"`  // server folder → pinned table of contents
	Probe tgProbe           `json:"probe"`
}

var (
	tgMu      sync.Mutex
	tgDataDir string
	tgIndexes = map[string]*tgIndex{}
)

// SetDataDir tells where Telegram storages keep their message index (the agent
// data dir). Without it the index lives in memory only.
func SetDataDir(dir string) {
	tgMu.Lock()
	defer tgMu.Unlock()
	tgDataDir = dir
	tgIndexes = map[string]*tgIndex{}
}

func tgIndexFor(id, chat string) *tgIndex {
	tgMu.Lock()
	defer tgMu.Unlock()
	if id == "" {
		id = "unsaved"
	}
	key := id + "\x00" + chat
	if ix := tgIndexes[key]; ix != nil {
		return ix
	}
	ix := &tgIndex{}
	if tgDataDir != "" {
		ix.file = filepath.Join(tgDataDir, "telegram-"+safeName(id)+".json")
		if b, err := os.ReadFile(ix.file); err == nil {
			_ = json.Unmarshal(b, ix)
		}
	}
	if ix.Chat != chat {
		// Another chat: messages of the old one cannot be reached any more.
		ix.Runs, ix.TOC, ix.Probe = nil, nil, tgProbe{}
	}
	ix.Chat = chat
	if ix.Runs == nil {
		ix.Runs = map[string]*tgRun{}
	}
	if ix.TOC == nil {
		ix.TOC = map[string]int{}
	}
	tgIndexes[key] = ix
	return ix
}

func safeName(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_') {
			return r
		}
		return '_'
	}, s)
}

// save writes the index; the caller holds ix.mu.
func (ix *tgIndex) save() {
	if ix.file == "" {
		return
	}
	b, err := json.Marshal(ix)
	if err != nil {
		return
	}
	tmp := ix.file + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, ix.file)
	}
}

func (ix *tgIndex) run(dir string) *tgRun {
	r := ix.Runs[dir]
	if r == nil {
		r = &tgRun{Created: time.Now(), Files: map[string]*tgFile{}}
		ix.Runs[dir] = r
	}
	return r
}

// TelegramNote reports a known problem of a Telegram storage from the agent's
// index without network calls ("" when there is none).
func TelegramNote(s model.Storage) string {
	if s.Kind != model.StorageTelegram {
		return ""
	}
	ix := tgIndexFor(s.ID, s.ChatID)
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return ix.blocked()
}

func (ix *tgIndex) blocked() string {
	if ix.Probe.Result == "no" {
		return "бот не может удалять сообщения старше 48 часов, поэтому старые копии в Telegram не удаляются (ротация отключена). Дайте боту право «Удалять сообщения» в канале"
	}
	return ""
}

// RotationBlocked tells the runner to skip rotation for this storage.
func (t *Telegram) RotationBlocked() string {
	t.ix.mu.Lock()
	defer t.ix.mu.Unlock()
	return t.ix.blocked()
}

// ---- paths, captions ----

// parts splits a cloud path into folders below the storage root.
func (t *Telegram) parts(p string) []string {
	rel := strings.Trim(strings.TrimPrefix(Clean(p), t.root), "/")
	if rel == "" {
		return nil
	}
	return strings.Split(rel, "/")
}

// hashtag makes a searchable tag: Telegram links only letters, digits and "_",
// and a tag of digits alone is not linked at all.
func hashtag(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return ""
	}
	if strings.IndexFunc(out, func(r rune) bool { return !unicode.IsDigit(r) }) < 0 {
		out = "n" + out
	}
	return "#" + out
}

// tags is "#server #job #d20261025" for a run folder.
func (t *Telegram) tags(dir string) string {
	ps := t.parts(dir)
	var out []string
	for i, p := range ps {
		if i < 2 {
			if h := hashtag(p); h != "" {
				out = append(out, h)
			}
		} else if i == 2 && len(p) >= 10 {
			out = append(out, "#d"+strings.ReplaceAll(p[:10], "-", ""))
		}
	}
	return strings.Join(out, " ")
}

func partNo(name string) (string, int) {
	i := strings.LastIndex(name, ".part")
	if i < 0 {
		return name, 0
	}
	n, err := strconv.Atoi(name[i+5:])
	if err != nil {
		return name, 0
	}
	return name[:i], n
}

func humanSize(n int64) string {
	units := []string{"КБ", "МБ", "ГБ", "ТБ"}
	if n < 1024 {
		return fmt.Sprintf("%d Б", n)
	}
	v, i := float64(n)/1024, 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	return strings.Replace(fmt.Sprintf("%.1f %s", v, units[i]), ".", ",", 1)
}

// link points to a message of the chat for people who open the table of contents.
func (t *Telegram) link(msg int) string {
	switch {
	case strings.HasPrefix(t.chat, "-100"):
		return fmt.Sprintf("https://t.me/c/%s/%d", strings.TrimPrefix(t.chat, "-100"), msg)
	case strings.HasPrefix(t.chat, "@"):
		return fmt.Sprintf("https://t.me/%s/%d", strings.TrimPrefix(t.chat, "@"), msg)
	}
	return ""
}

// ---- Storage ----

func (t *Telegram) MkdirAll(context.Context, string) error { return nil }

func (t *Telegram) Put(ctx context.Context, p string, r io.ReadSeeker, size int64) error {
	p = Clean(p)
	dir, name := path.Dir(p), path.Base(p)
	if name == "manifest.json" {
		if _, err := r.Seek(0, io.SeekStart); err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(r, 8<<20))
		if err != nil {
			return err
		}
		return t.finish(ctx, dir, data)
	}
	t.ensureProbe(ctx)
	file, no := partNo(name)
	title := file
	if no > 0 {
		title = fmt.Sprintf("%s, часть %d", file, no)
	}
	caption := strings.TrimSpace("⏳ "+t.tags(dir)) + "\n" + title + " — идёт загрузка, копия ещё не готова"
	var msg tgMessage
	err := t.do(ctx, "sendDocument", fileBody(map[string]string{
		"chat_id":                        t.chat,
		"caption":                        caption,
		"disable_notification":           "true",
		"disable_content_type_detection": "true",
	}, "document", name, r, size), &msg)
	if err != nil {
		return err
	}
	if msg.Document == nil {
		return errors.New("Telegram не вернул файл отправленного документа")
	}
	t.ix.mu.Lock()
	run := t.ix.run(dir)
	old := run.Files[name]
	run.Files[name] = &tgFile{MessageID: msg.MessageID, FileID: msg.Document.FileID, Size: size, Staging: true}
	t.ix.save()
	t.ix.mu.Unlock()
	if old != nil {
		_ = t.deleteIDs(ctx, []int{old.MessageID})
	}
	return nil
}

// groups splits n parts into albums of 2–10 documents (a lone part is sent alone).
func groups(n int) []int {
	g := (n + 9) / 10
	var out []int
	for i := 0; i < g; i++ {
		size := n / g
		if i < n%g {
			size++
		}
		out = append(out, size)
	}
	return out
}

// finish turns the uploaded parts of a run into albums and posts the manifest.
func (t *Telegram) finish(ctx context.Context, dir string, data []byte) error {
	var man model.Manifest
	if err := json.Unmarshal(data, &man); err != nil {
		return fmt.Errorf("manifest.json: %w", err)
	}
	tags := t.tags(dir)
	idx := &model.TelegramIndex{Chat: t.chat, Messages: map[string]model.TelegramRef{}}
	var stale []int
	parts := 0
	var total int64
	for _, f := range man.Files {
		n := len(f.Parts)
		if n == 0 {
			continue
		}
		t.ix.mu.Lock()
		run := t.ix.run(dir)
		var have []tgFile
		for _, p := range f.Parts {
			if e := run.Files[p.Name]; e != nil {
				have = append(have, *e)
			}
		}
		t.ix.mu.Unlock()
		if len(have) != n {
			return fmt.Errorf("%s: в Telegram найдено %d частей из %d", f.Name, len(have), n)
		}
		at := 0
		for _, g := range groups(n) {
			var media []map[string]any
			for k := at; k < at+g; k++ {
				media = append(media, map[string]any{
					"type":    "document",
					"media":   have[k].FileID,
					"caption": fmt.Sprintf("%s\n%s — часть %d из %d · %s", tags, f.Name, k+1, n, humanSize(f.Parts[k].Size)),
				})
			}
			var msgs []tgMessage
			var err error
			if g == 1 {
				var m tgMessage
				err = t.call(ctx, "sendDocument", map[string]any{"chat_id": t.chat, "document": media[0]["media"], "caption": media[0]["caption"], "disable_notification": true}, &m)
				msgs = []tgMessage{m}
			} else {
				err = t.call(ctx, "sendMediaGroup", map[string]any{"chat_id": t.chat, "media": media, "disable_notification": true}, &msgs)
			}
			if err != nil {
				return fmt.Errorf("%s: альбом частей: %w", f.Name, err)
			}
			if len(msgs) != g {
				return fmt.Errorf("%s: Telegram вернул %d сообщений вместо %d", f.Name, len(msgs), g)
			}
			t.ix.mu.Lock()
			run := t.ix.run(dir)
			for i, m := range msgs {
				k := at + i
				e := tgFile{MessageID: m.MessageID, FileID: have[k].FileID, Size: f.Parts[k].Size}
				if m.Document != nil {
					e.FileID = m.Document.FileID
				}
				if have[k].Staging {
					stale = append(stale, have[k].MessageID)
				}
				run.Files[f.Parts[k].Name] = &e
				idx.Messages[f.Parts[k].Name] = model.TelegramRef{MessageID: e.MessageID, FileID: e.FileID}
			}
			t.ix.save()
			t.ix.mu.Unlock()
			at += g
		}
		parts += n
		total += f.Size
	}

	// Leftovers of failed attempts that the manifest does not mention.
	t.ix.mu.Lock()
	for name, e := range t.ix.run(dir).Files {
		if e.Staging {
			stale = append(stale, e.MessageID)
			delete(t.ix.run(dir).Files, name)
		}
	}
	t.ix.mu.Unlock()

	man.Telegram = idx
	out, _ := json.MarshalIndent(man, "", "  ")
	var m tgMessage
	err := t.do(ctx, "sendDocument", fileBody(map[string]string{
		"chat_id":                        t.chat,
		"caption":                        t.summary(tags, man, parts, total),
		"disable_content_type_detection": "true",
	}, "document", "manifest.json", bytes.NewReader(out), int64(len(out))), &m)
	if err != nil {
		return fmt.Errorf("manifest.json: %w", err)
	}
	t.ix.mu.Lock()
	run := t.ix.run(dir)
	e := &tgFile{MessageID: m.MessageID, Size: int64(len(out)), Data: out}
	if m.Document != nil {
		e.FileID = m.Document.FileID
	}
	run.Files["manifest.json"] = e
	run.Done, run.Status, run.JobName, run.Bytes, run.Parts = true, man.Status, man.JobName, total, parts
	t.ix.save()
	t.ix.mu.Unlock()

	_ = t.deleteIDs(ctx, stale)
	if ps := t.parts(dir); len(ps) > 0 {
		t.updateTOC(ctx, ps[0])
	}
	t.checkProbe(ctx)
	return nil
}

func (t *Telegram) summary(tags string, man model.Manifest, parts int, total int64) string {
	var head string
	switch man.Status {
	case model.RunSuccess:
		head = "✅ Копия полная"
	case model.RunWarning:
		head = "⚠️ Копия полная, но с предупреждениями"
	default:
		head = "❌ Копия неполная: часть файлов не выгружена"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%s\n%s · %s\n", tags, head, man.JobName, man.StartedAt.Format("02.01.2006 15:04"))
	for _, f := range man.Files {
		if f.Error != "" {
			fmt.Fprintf(&b, "✖ %s — ошибка\n", f.Name)
		} else {
			fmt.Fprintf(&b, "• %s — %d ч., %s\n", f.Name, len(f.Parts), humanSize(f.Size))
		}
	}
	fmt.Fprintf(&b, "Всего: %d ч., %s. manifest.json — оглавление и контрольные суммы; без него копия не завершена.", parts, humanSize(total))
	s := b.String()
	if r := []rune(s); len(r) > 1000 {
		s = string(r[:999]) + "…"
	}
	return s
}

func (t *Telegram) lookup(p string) *tgFile {
	p = Clean(p)
	t.ix.mu.Lock()
	defer t.ix.mu.Unlock()
	if r := t.ix.Runs[path.Dir(p)]; r != nil {
		if e := r.Files[path.Base(p)]; e != nil {
			c := *e
			return &c
		}
	}
	return nil
}

func (t *Telegram) Get(ctx context.Context, p string) (io.ReadCloser, error) {
	e := t.lookup(p)
	if e == nil {
		return nil, ErrNotFound
	}
	if e.Data != nil {
		return io.NopCloser(bytes.NewReader(e.Data)), nil
	}
	var f struct {
		FilePath string `json:"file_path"`
	}
	// A --local server first downloads the file from Telegram to its disk.
	if err := t.call(ctx, "getFile", map[string]any{"file_id": e.FileID}, &f); err != nil {
		return nil, err
	}
	if !path.IsAbs(f.FilePath) && !filepath.IsAbs(f.FilePath) {
		return nil, errors.New("Bot API работает не в режиме --local: файлы больше 20 МБ не скачать")
	}
	return t.tr.open(ctx, f.FilePath)
}

func (t *Telegram) List(_ context.Context, dir string) ([]Entry, error) {
	dir = Clean(dir)
	t.ix.mu.Lock()
	defer t.ix.mu.Unlock()
	var out []Entry
	seen := map[string]bool{}
	prefix := strings.TrimSuffix(dir, "/") + "/"
	for d, r := range t.ix.Runs {
		if d == dir {
			for name, e := range r.Files {
				out = append(out, Entry{Name: name, Size: e.Size, Modified: r.Created})
			}
			continue
		}
		if !strings.HasPrefix(d, prefix) {
			continue
		}
		child := strings.SplitN(strings.TrimPrefix(d, prefix), "/", 2)[0]
		if !seen[child] {
			seen[child] = true
			out = append(out, Entry{Name: child, IsDir: true, Modified: r.Created})
		}
	}
	if out == nil {
		return nil, ErrNotFound
	}
	return out, nil
}

// Delete removes a file or a folder with every run below it.
func (t *Telegram) Delete(ctx context.Context, p string) error {
	p = Clean(p)
	prefix := strings.TrimSuffix(p, "/") + "/"
	t.ix.mu.Lock()
	var ids []int
	var dirs []string
	var oldest time.Time
	var check int // a single (non-album) message to see whether the deletion happened
	for d, r := range t.ix.Runs {
		if d != p && !strings.HasPrefix(d, prefix) {
			continue
		}
		dirs = append(dirs, d)
		for _, e := range r.Files {
			ids = append(ids, e.MessageID)
		}
		if m := r.Files["manifest.json"]; m != nil && r.Done {
			check = m.MessageID
		}
		if oldest.IsZero() || r.Created.Before(oldest) {
			oldest = r.Created
		}
	}
	var file string
	if len(dirs) == 0 {
		if r := t.ix.Runs[path.Dir(p)]; r != nil && r.Files[path.Base(p)] != nil {
			file = p
			ids = append(ids, r.Files[path.Base(p)].MessageID)
			oldest = r.Created
		}
	}
	t.ix.mu.Unlock()
	if len(ids) == 0 {
		return nil
	}
	err := t.deleteIDs(ctx, ids)
	old := time.Since(oldest) > 47*time.Hour
	if err == nil && old && check != 0 && t.exists(ctx, check) {
		err = errors.New("Telegram не удалил сообщения старше 48 часов")
	}
	if err != nil {
		if old {
			t.ix.mu.Lock()
			t.ix.Probe.Result = "no"
			t.ix.save()
			t.ix.mu.Unlock()
			return fmt.Errorf("%w: %s", err, t.RotationBlocked())
		}
		return err
	}
	t.ix.mu.Lock()
	servers := map[string]bool{}
	for _, d := range dirs {
		if r := t.ix.Runs[d]; r != nil && r.Done {
			if ps := t.parts(d); len(ps) > 0 {
				servers[ps[0]] = true
			}
		}
		delete(t.ix.Runs, d)
	}
	if file != "" {
		if r := t.ix.Runs[path.Dir(file)]; r != nil {
			delete(r.Files, path.Base(file))
			if len(r.Files) == 0 {
				delete(t.ix.Runs, path.Dir(file))
			}
		}
	}
	t.ix.save()
	t.ix.mu.Unlock()
	for s := range servers {
		t.updateTOC(ctx, s)
	}
	return nil
}

func (t *Telegram) deleteIDs(ctx context.Context, ids []int) error {
	for len(ids) > 0 {
		n := min(len(ids), 100)
		if err := t.call(ctx, "deleteMessages", map[string]any{"chat_id": t.chat, "message_ids": ids[:n]}, nil); err != nil {
			return err
		}
		ids = ids[n:]
	}
	return nil
}

// exists tells whether a message is still in the chat: an empty keyboard edit
// answers "not modified" for an existing message and "not found" for a deleted one.
func (t *Telegram) exists(ctx context.Context, id int) bool {
	err := t.call(ctx, "editMessageReplyMarkup", map[string]any{"chat_id": t.chat, "message_id": id, "reply_markup": map[string]any{"inline_keyboard": [][]any{}}}, nil)
	return err == nil || tgIs(err, "not modified")
}

func (t *Telegram) Quota(context.Context) (Quota, error) {
	return Quota{MaxFile: TelegramMaxFile}, nil
}

// ---- 48-hour probe ----

const probeText = "🛠 Backupper: служебное сообщение. Через двое суток бот попробует удалить его, чтобы проверить, может ли он удалять старые копии."

func (t *Telegram) ensureProbe(ctx context.Context) {
	t.ix.mu.Lock()
	// Only an agent keeps the index long enough to come back to the probe; a check
	// from the panel would leave a new one in the chat every time.
	skip := t.ix.Probe.MessageID != 0 || t.ix.file == ""
	t.ix.mu.Unlock()
	if skip {
		return
	}
	var m tgMessage
	if t.call(ctx, "sendMessage", map[string]any{"chat_id": t.chat, "text": probeText, "disable_notification": true}, &m) != nil {
		return
	}
	t.ix.mu.Lock()
	t.ix.Probe = tgProbe{MessageID: m.MessageID, Sent: time.Now()}
	t.ix.save()
	t.ix.mu.Unlock()
}

func (t *Telegram) checkProbe(ctx context.Context) {
	t.ix.mu.Lock()
	pr := t.ix.Probe
	t.ix.mu.Unlock()
	if pr.MessageID == 0 || pr.Result != "" || time.Since(pr.Sent) < 49*time.Hour {
		return
	}
	res := "ok"
	if err := t.deleteIDs(ctx, []int{pr.MessageID}); err != nil || t.exists(ctx, pr.MessageID) {
		res = "no"
	}
	t.ix.mu.Lock()
	t.ix.Probe.Result = res
	t.ix.save()
	t.ix.mu.Unlock()
}

// ---- pinned table of contents ----

func (t *Telegram) tocText(server string) string {
	t.ix.mu.Lock()
	type item struct {
		when time.Time
		r    *tgRun
	}
	jobs := map[string][]item{}
	names := map[string]string{}
	for d, r := range t.ix.Runs {
		ps := t.parts(d)
		if !r.Done || len(ps) < 3 || ps[0] != server {
			continue
		}
		when, err := time.ParseInLocation("2006-01-02_15-04-05", ps[2], time.Local)
		if err != nil {
			when = r.Created
		}
		jobs[ps[1]] = append(jobs[ps[1]], item{when, r})
		if r.JobName != "" {
			names[ps[1]] = r.JobName
		}
	}
	t.ix.mu.Unlock()
	var keys []string
	for k := range jobs {
		keys = append(keys, k)
		sort.Slice(jobs[k], func(i, j int) bool { return jobs[k][i].when.After(jobs[k][j].when) })
	}
	sort.Strings(keys)
	build := func(limit int) string {
		var b strings.Builder
		if server == model.SitesFolder {
			fmt.Fprintf(&b, "🗂 <b>Бэкапы сайтов на хостинге</b>\n<i>обновлено %s</i>\n", time.Now().Format("02.01.2006 15:04"))
		} else {
			fmt.Fprintf(&b, "🗂 <b>Бэкапы сервера %s</b>\n<i>обновлено %s</i>\n", html.EscapeString(server), time.Now().Format("02.01.2006 15:04"))
		}
		for _, k := range keys {
			name := names[k]
			if name == "" {
				name = k
			}
			fmt.Fprintf(&b, "\n<b>%s</b> %s\n", html.EscapeString(name), hashtag(k))
			for i, it := range jobs[k] {
				if i >= limit {
					fmt.Fprintf(&b, "… и ещё %d\n", len(jobs[k])-limit)
					break
				}
				icon := map[string]string{model.RunSuccess: "✅", model.RunWarning: "⚠️"}[it.r.Status]
				if icon == "" {
					icon = "❌"
				}
				line := fmt.Sprintf("%s %s · %s · %d ч.", icon, it.when.Format("02.01.2006 15:04"), humanSize(it.r.Bytes), it.r.Parts)
				if m := it.r.Files["manifest.json"]; m != nil {
					if l := t.link(m.MessageID); l != "" {
						line += fmt.Sprintf(` · <a href="%s">манифест</a>`, l)
					}
				}
				b.WriteString(line + "\n")
			}
		}
		b.WriteString("\nКопия полная, если есть все части «N из M» и manifest.json после них. Поиск: " + hashtag(server) + " #dГГГГММДД")
		return b.String()
	}
	for limit := 15; ; limit = limit * 2 / 3 {
		s := build(limit)
		if len([]rune(s)) <= 4000 || limit <= 1 {
			return s
		}
	}
}

// updateTOC edits the pinned list of copies of a server, creating it when needed.
func (t *Telegram) updateTOC(ctx context.Context, server string) {
	text := t.tocText(server)
	t.ix.mu.Lock()
	id := t.ix.TOC[server]
	t.ix.mu.Unlock()
	params := map[string]any{"chat_id": t.chat, "text": text, "parse_mode": "HTML", "link_preview_options": map[string]any{"is_disabled": true}}
	if id != 0 {
		p := map[string]any{"message_id": id}
		for k, v := range params {
			p[k] = v
		}
		err := t.call(ctx, "editMessageText", p, nil)
		if err == nil || tgIs(err, "not modified") || !tgIs(err, "not found") {
			return
		}
	}
	var m tgMessage
	params["disable_notification"] = true
	if t.call(ctx, "sendMessage", params, &m) != nil {
		return
	}
	_ = t.call(ctx, "pinChatMessage", map[string]any{"chat_id": t.chat, "message_id": m.MessageID, "disable_notification": true}, nil)
	t.ix.mu.Lock()
	t.ix.TOC[server] = m.MessageID
	t.ix.save()
	t.ix.mu.Unlock()
}

// ---- checks for the panel ----

// Check verifies the bot and its rights in the chat. Missing optional rights
// come back as notes.
func (t *Telegram) Check(ctx context.Context) ([]string, error) {
	var me struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}
	if err := t.call(ctx, "getMe", nil, &me); err != nil {
		return nil, err
	}
	var chat struct {
		Type  string `json:"type"`
		Title string `json:"title"`
	}
	if err := t.call(ctx, "getChat", map[string]any{"chat_id": t.chat}, &chat); err != nil {
		return nil, fmt.Errorf("канал %s: %w — добавьте бота @%s администратором", t.chat, err, me.Username)
	}
	var mem struct {
		Status         string `json:"status"`
		CanPost        bool   `json:"can_post_messages"`
		CanEdit        bool   `json:"can_edit_messages"`
		CanDelete      bool   `json:"can_delete_messages"`
		CanPin         bool   `json:"can_pin_messages"`
		CanSendMessage *bool  `json:"can_send_messages"`
	}
	if err := t.call(ctx, "getChatMember", map[string]any{"chat_id": t.chat, "user_id": me.ID}, &mem); err != nil {
		return nil, err
	}
	if mem.Status != "administrator" && mem.Status != "creator" {
		return nil, fmt.Errorf("бот @%s не администратор в «%s»", me.Username, chat.Title)
	}
	var notes []string
	if chat.Type == "channel" {
		if !mem.CanPost {
			return nil, fmt.Errorf("у бота @%s нет права «Публикация сообщений» в «%s»", me.Username, chat.Title)
		}
		if !mem.CanEdit {
			notes = append(notes, "нет права «Редактирование сообщений»: оглавление в закреплённом сообщении не будет обновляться")
		}
	} else if !mem.CanPin {
		notes = append(notes, "нет права «Закрепление сообщений»: оглавление не будет закреплено")
	}
	if !mem.CanDelete {
		notes = append(notes, "нет права «Удаление сообщений»: старые копии не будут удаляться (ротация не сработает)")
	}
	if why := t.RotationBlocked(); why != "" {
		notes = append(notes, why)
	}
	return notes, nil
}

// TelegramBot checks the way to the Bot API and returns the bot's username.
func TelegramBot(ctx context.Context, s model.Storage) (string, error) {
	t, err := tgClient(s)
	if err != nil {
		return "", err
	}
	var me struct {
		Username string `json:"username"`
	}
	err = t.call(ctx, "getMe", nil, &me)
	return me.Username, err
}

// TelegramChat is a chat the bot has seen in its updates.
type TelegramChat struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type"`
}

// TelegramChats lists chats from the bot's pending updates: after the bot is made
// an admin of a channel, the channel shows up here with its numeric id.
func TelegramChats(ctx context.Context, s model.Storage) ([]TelegramChat, error) {
	t, err := tgClient(s)
	if err != nil {
		return nil, err
	}
	var ups []map[string]json.RawMessage
	if err := t.call(ctx, "getUpdates", map[string]any{"timeout": 0, "allowed_updates": []string{"my_chat_member", "channel_post", "message", "edited_channel_post"}}, &ups); err != nil {
		return nil, err
	}
	seen := map[int64]bool{}
	out := []TelegramChat{}
	for _, u := range ups {
		for _, k := range []string{"my_chat_member", "channel_post", "edited_channel_post", "message"} {
			var v struct {
				Chat TelegramChat `json:"chat"`
			}
			if raw, ok := u[k]; ok && json.Unmarshal(raw, &v) == nil && v.Chat.ID != 0 && !seen[v.Chat.ID] {
				if v.Chat.Title == "" {
					v.Chat.Title = "личный чат"
				}
				seen[v.Chat.ID] = true
				out = append(out, v.Chat)
			}
		}
	}
	return out, nil
}
