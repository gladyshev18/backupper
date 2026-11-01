package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"backupper/internal/agent"
	"backupper/internal/cms"
	"backupper/internal/desktop"
	"backupper/internal/model"
	"backupper/internal/storage"
	"backupper/internal/version"
)

// App is bound to the frontend: every exported method is callable from JS.
type App struct {
	ctx            context.Context
	store          *desktop.Store
	mgr            *desktop.Manager
	siteTransferMu sync.Mutex
}

func NewApp(store *desktop.Store) *App {
	return &App{store: store, mgr: desktop.NewManager(store)}
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

func (a *App) shutdown(ctx context.Context) { a.mgr.CloseAll() }

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ---- panel state ----

type ServerView struct {
	desktop.Server
	HasToken    bool   `json:"hasToken"`
	Fingerprint string `json:"fingerprint"`
}

type State struct {
	Version  string            `json:"version"`
	Servers  []ServerView      `json:"servers"`
	Storages []model.Storage   `json:"storages"`
	Settings desktop.Settings  `json:"settings"`
	Keys     []string          `json:"keys"`
	Kinds    map[string]string `json:"kinds"`
}

func (a *App) GetState() State {
	cfg := a.store.Get()
	st := State{Version: version.Version, Settings: cfg.Settings, Keys: desktop.DefaultKeys()}
	st.Storages = []model.Storage{}
	for _, s := range cfg.Storages {
		if s.TgServer != nil {
			// The relay key stays in the panel; SaveStorage keeps it.
			ts := *s.TgServer
			ts.Key = ""
			s.TgServer = &ts
		}
		st.Storages = append(st.Storages, s)
	}
	for _, s := range cfg.Servers {
		v := ServerView{Server: s, HasToken: s.Token != "", Fingerprint: desktop.Fingerprint(s.HostKey)}
		v.Token = ""
		st.Servers = append(st.Servers, v)
	}
	if st.Servers == nil {
		st.Servers = []ServerView{}
	}
	return st
}

// ---- servers ----

func (a *App) SaveServer(s desktop.Server) (string, error) {
	s.Name = strings.TrimSpace(s.Name)
	s.Host = strings.TrimSpace(s.Host)
	if s.Host == "" {
		return "", errors.New("укажите адрес сервера")
	}
	if s.Name == "" {
		s.Name = s.Host
	}
	if s.Port == 0 {
		s.Port = 22
	}
	if s.User == "" {
		s.User = "root"
	}
	if s.AgentPort == 0 {
		s.AgentPort = 7171
	}
	isNew := s.ID == ""
	if isNew {
		s.ID = newID()
	}
	err := a.store.Update(func(c *desktop.Config) error {
		for i := range c.Servers {
			if c.Servers[i].ID == s.ID {
				old := c.Servers[i]
				s.Token = old.Token
				if old.Host != s.Host || old.Port != s.Port {
					s.HostKey = "" // new address: pin the key again
				} else {
					s.HostKey = old.HostKey
				}
				if s.Passphrase == "" {
					s.Passphrase = old.Passphrase
				}
				c.Servers[i] = s
				return nil
			}
		}
		c.Servers = append(c.Servers, s)
		return nil
	})
	if err != nil {
		return "", err
	}
	a.mgr.Drop(s.ID)
	return s.ID, nil
}

func (a *App) DeleteServer(id string) error {
	a.mgr.Drop(id)
	return a.store.Update(func(c *desktop.Config) error {
		for i := range c.Servers {
			if c.Servers[i].ID == id {
				c.Servers = append(c.Servers[:i], c.Servers[i+1:]...)
				return nil
			}
		}
		return nil
	})
}

func (a *App) ResetHostKey(id string) error {
	a.mgr.Drop(id)
	return a.store.Update(func(c *desktop.Config) error {
		for i := range c.Servers {
			if c.Servers[i].ID == id {
				c.Servers[i].HostKey = ""
			}
		}
		return nil
	})
}

// CheckServer tests SSH and, if the agent is already installed, adopts it.
func (a *App) CheckServer(id string) desktop.ServerCheck {
	c, err := a.mgr.SSH(id)
	if err != nil {
		return desktop.ServerCheck{Error: err.Error()}
	}
	chk := desktop.Probe(c)
	if s, ok := a.store.Server(id); ok {
		chk.Fingerprint = desktop.Fingerprint(s.HostKey)
		if chk.AgentInstalled && s.Token == "" {
			if err := a.mgr.AdoptAgent(id); err != nil {
				chk.Error = "агент найден, но не удалось подключиться к нему: " + err.Error()
			}
		}
	}
	return chk
}

func (a *App) InstallAgent(id string) (string, error) { return a.mgr.InstallAgent(id) }

func (a *App) UninstallAgent(id string, purge bool) error { return a.mgr.UninstallAgent(id, purge) }

func (a *App) PickKeyFile() (string, error) {
	home, _ := os.UserHomeDir()
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "Выберите приватный SSH-ключ",
		DefaultDirectory: home + `\.ssh`,
	})
}

// ---- agent data ----

type ServerStatus struct {
	ID       string           `json:"id"`
	Online   bool             `json:"online"`
	Error    string           `json:"error,omitempty"`
	NoAgent  bool             `json:"noAgent"`
	Info     *model.AgentInfo `json:"info,omitempty"`
	Active   []*model.Run     `json:"active"`
	Recent   []*model.Run     `json:"recent"`
	Outdated bool             `json:"outdated"`
}

func (a *App) status(id string) ServerStatus {
	st := ServerStatus{ID: id, Active: []*model.Run{}, Recent: []*model.Run{}}
	var info model.AgentInfo
	if err := a.mgr.Call(id, http.MethodGet, "/api/info", nil, &info, 20*time.Second); err != nil {
		st.Error = err.Error()
		st.NoAgent = errors.Is(err, desktop.ErrNoAgent)
		return st
	}
	st.Online = true
	st.Info = &info
	st.Outdated = info.Version != version.Version
	_ = a.mgr.Call(id, http.MethodGet, "/api/active", nil, &st.Active, 15*time.Second)
	_ = a.mgr.Call(id, http.MethodGet, "/api/runs?limit=20", nil, &st.Recent, 15*time.Second)
	return st
}

func (a *App) ServerStatus(id string) ServerStatus { return a.status(id) }

// Overview polls all servers in parallel.
func (a *App) Overview() []ServerStatus {
	servers := a.store.Get().Servers
	out := make([]ServerStatus, len(servers))
	var wg sync.WaitGroup
	for i, s := range servers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = a.status(s.ID)
		}()
	}
	wg.Wait()
	return out
}

// Lists below are never nil: agents answer "null" when there is nothing yet, and the
// UI takes null for "still loading".

func (a *App) Active(id string) ([]*model.Run, error) {
	var runs []*model.Run
	err := a.mgr.Call(id, http.MethodGet, "/api/active", nil, &runs, 15*time.Second)
	return nonNil(runs), err
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func (a *App) SaveJob(serverID string, job model.Job) (model.Job, error) {
	id := job.ID
	if id == "" {
		id = "new"
	}
	var out model.Job
	err := a.mgr.Call(serverID, http.MethodPut, "/api/jobs/"+url.PathEscape(id), job, &out, 0)
	return out, err
}

func (a *App) DeleteJob(serverID, jobID string) error {
	return a.mgr.Call(serverID, http.MethodDelete, "/api/jobs/"+url.PathEscape(jobID), nil, nil, 0)
}

func (a *App) siteJob(serverID, jobID string) (model.Job, bool, error) {
	var info model.AgentInfo
	if err := a.mgr.Call(serverID, http.MethodGet, "/api/info", nil, &info, 0); err != nil {
		return model.Job{}, false, err
	}
	if info.Version != version.Version {
		return model.Job{}, false, errors.New("обновите агент до версии панели")
	}
	for _, v := range info.Jobs {
		if v.ID == jobID {
			return v.Job, true, nil
		}
	}
	return model.Job{}, false, nil
}

func (a *App) pauseSite(serverID, jobID string) (model.Job, error) {
	var job model.Job
	err := a.mgr.Call(serverID, http.MethodPost, "/api/jobs/"+url.PathEscape(jobID)+"/site-pause", nil, &job, 0)
	return job, err
}

func (a *App) copySiteData(source, oldID, target, newID, name string) error {
	q := url.Values{"job": {oldID}, "name": {name}}
	var data json.RawMessage
	if err := a.mgr.Call(source, http.MethodGet, "/api/site-transfer?"+q.Encode(), nil, &data, 0); err != nil {
		return fmt.Errorf("чтение старого сервера: %w", err)
	}
	if err := a.mgr.Call(target, http.MethodPost, "/api/jobs/"+url.PathEscape(newID)+"/site-transfer", data, nil, 0); err != nil {
		return fmt.Errorf("запись на новый сервер: %w", err)
	}
	return nil
}

// MoveSite is retryable: the destination records the source and the import phase.
func (a *App) MoveSite(source, target string, desired model.Job) (model.Job, error) {
	a.siteTransferMu.Lock()
	defer a.siteTransferMu.Unlock()
	if source == target || desired.ID == "" || desired.Kind != model.JobSite {
		return model.Job{}, errors.New("неверные параметры переноса")
	}
	dst, exists, err := a.siteJob(target, desired.ID)
	if err != nil {
		return dst, err
	}
	old, oldExists, err := a.siteJob(source, desired.ID)
	if err != nil {
		return dst, err
	}
	if exists && !dst.Migrating && !oldExists && dst.Kind == model.JobSite && dst.Name == desired.Name {
		return dst, nil // The final response may have been lost after activation.
	}
	if exists && (!dst.Migrating || dst.MigrationSource != source) {
		return dst, errors.New("на новом сервере уже есть задача с таким идентификатором")
	}
	if oldExists {
		if old.Kind != model.JobSite || old.Name != desired.Name {
			return dst, errors.New("при переносе нельзя менять название сайта")
		}
		if _, err := a.pauseSite(source, old.ID); err != nil {
			return dst, err
		}
	} else if !exists || !dst.MigrationImported {
		return dst, errors.New("исходный сайт не найден, а перенос ещё не завершён")
	}
	if !exists {
		desired.Migrating, desired.MigrationSource, desired.MigrationImported = true, source, false
		dst, err = a.SaveJob(target, desired)
		if err != nil {
			return dst, fmt.Errorf("исходный сайт приостановлен; повторите перенос: %w", err)
		}
	}
	if !dst.MigrationImported {
		if err := a.copySiteData(source, desired.ID, target, dst.ID, dst.Name); err != nil {
			return dst, fmt.Errorf("сайты приостановлены; повторите перенос: %w", err)
		}
	}
	if oldExists {
		if err := a.DeleteJob(source, old.ID); err != nil {
			return dst, fmt.Errorf("данные перенесены, но старый сайт не удалён; повторите перенос: %w", err)
		}
	}
	dst.Migrating, dst.MigrationSource, dst.MigrationImported = false, "", false
	if dst, err = a.SaveJob(target, dst); err != nil {
		return dst, fmt.Errorf("данные перенесены, но сайт приостановлен; повторите перенос: %w", err)
	}
	return dst, nil
}

// RestoreSiteData handles sites already moved by older panel versions.
func (a *App) RestoreSiteData(source, oldID, target, newID string) error {
	a.siteTransferMu.Lock()
	defer a.siteTransferMu.Unlock()
	if source == target || oldID == "" {
		return errors.New("выберите другой сервер и исходную задачу")
	}
	if _, _, err := a.siteJob(source, oldID); err != nil {
		return err
	}
	job, ok, err := a.siteJob(target, newID)
	if err != nil {
		return err
	}
	if !ok || job.Kind != model.JobSite {
		return errors.New("сайт не найден")
	}
	if job.MigrationSource != "" {
		return errors.New("сначала завершите перенос сайта")
	}
	if _, err = a.pauseSite(target, newID); err != nil {
		return err
	}
	if err = a.copySiteData(source, oldID, target, newID, job.Name); err != nil {
		return fmt.Errorf("сайт приостановлен; повторите восстановление: %w", err)
	}
	job.Migrating = false
	_, err = a.SaveJob(target, job)
	return err
}

func (a *App) RunJob(serverID, jobID string) (*model.Run, error) {
	var run model.Run
	err := a.mgr.Call(serverID, http.MethodPost, "/api/jobs/"+url.PathEscape(jobID)+"/run", nil, &run, 0)
	return &run, err
}

func (a *App) CancelRun(serverID, runID string) error {
	return a.mgr.Call(serverID, http.MethodPost, "/api/runs/"+url.PathEscape(runID)+"/cancel", nil, nil, 0)
}

func (a *App) Runs(serverID, jobID string, limit int) ([]*model.Run, error) {
	var runs []*model.Run
	q := url.Values{"job": {jobID}, "limit": {fmt.Sprint(limit)}}
	err := a.mgr.Call(serverID, http.MethodGet, "/api/runs?"+q.Encode(), nil, &runs, 0)
	return nonNil(runs), err
}

func (a *App) Run(serverID, runID string) (*model.Run, error) {
	var run model.Run
	err := a.mgr.Call(serverID, http.MethodGet, "/api/runs/"+url.PathEscape(runID), nil, &run, 0)
	return &run, err
}

func (a *App) Plan(serverID string, job model.Job, lang string) (model.ChunkPlan, error) {
	var p model.ChunkPlan
	err := a.mgr.Call(serverID, http.MethodPost, "/api/plan?lang="+url.QueryEscape(lang), job, &p, 0)
	return p, err
}

func (a *App) Probe(serverID string, src model.Source) (model.ProbeResult, error) {
	var p model.ProbeResult
	err := a.mgr.Call(serverID, http.MethodPost, "/api/probe", src, &p, 45*time.Second)
	return p, err
}

// SpeedTest measures upload speed from the server to a cloud (feeds the chunk planner).
func (a *App) SpeedTest(serverID, storageID string) (model.StorageTest, error) {
	var r model.StorageTest
	err := a.mgr.Call(serverID, http.MethodPost, "/api/storages/test", map[string]any{"storageId": storageID, "speed": true}, &r, 6*time.Minute)
	return r, err
}

func (a *App) Backups(serverID, jobID, storageID string) ([]model.BackupEntry, error) {
	var out []model.BackupEntry
	q := url.Values{"job": {jobID}, "storage": {storageID}}
	err := a.mgr.Call(serverID, http.MethodGet, "/api/backups?"+q.Encode(), nil, &out, 3*time.Minute)
	return nonNil(out), err
}

func (a *App) DeleteBackup(serverID, storageID, dir string) error {
	q := url.Values{"storage": {storageID}, "dir": {dir}}
	return a.mgr.Call(serverID, http.MethodDelete, "/api/backups?"+q.Encode(), nil, nil, 3*time.Minute)
}

type progressWriter struct {
	a     *App
	key   string
	total int64
	done  int64
	last  time.Time
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if time.Since(p.last) > 300*time.Millisecond {
		p.last = time.Now()
		runtime.EventsEmit(p.a.ctx, "download", map[string]any{"key": p.key, "done": p.done, "total": p.total})
	}
	return len(b), nil
}

// DownloadBackup asks where to save, then streams the file glued from its parts.
func (a *App) DownloadBackup(serverID, storageID, dir, file string, decrypt bool, size int64) (string, error) {
	name := file
	if decrypt {
		name = strings.TrimSuffix(name, ".age")
	}
	dest, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: "Сохранить копию", DefaultFilename: name})
	if err != nil || dest == "" {
		return "", err
	}
	q := url.Values{"storage": {storageID}, "dir": {dir}, "file": {file}}
	if decrypt {
		q.Set("decrypt", "1")
	}
	resp, err := a.mgr.Stream(a.ctx, serverID, "/api/download?"+q.Encode())
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	f, err := os.Create(dest + ".part")
	if err != nil {
		return "", err
	}
	pw := &progressWriter{a: a, key: dir + "/" + file, total: size}
	_, err = io.Copy(io.MultiWriter(f, pw), resp.Body)
	f.Close()
	if err != nil {
		os.Remove(dest + ".part")
		return "", fmt.Errorf("скачивание прервано: %w", err)
	}
	os.Remove(dest)
	if err := os.Rename(dest+".part", dest); err != nil {
		return "", err
	}
	runtime.EventsEmit(a.ctx, "download", map[string]any{"key": pw.key, "done": pw.done, "total": pw.done, "finished": true})
	return dest, nil
}

func (a *App) RevealFile(path string) {
	_ = runtimeOpenExplorer(path)
}

// ---- clouds ----

// withSaved fills secrets the form leaves blank from the saved copy of the storage.
func (a *App) withSaved(s model.Storage) model.Storage {
	if s.ID == "" {
		return s
	}
	for _, old := range a.store.Get().Storages {
		if old.ID != s.ID {
			continue
		}
		if s.Password == "" {
			s.Password = old.Password
		}
		if s.Token == "" {
			s.Token = old.Token
		}
		if s.TgServer != nil && old.TgServer != nil {
			ts, o := *s.TgServer, old.TgServer
			if ts.Key == "" {
				ts.Key = o.Key
			}
			if ts.AdminPassphrase == "" {
				ts.AdminPassphrase = o.AdminPassphrase
			}
			if ts.APIHash == "" {
				ts.APIHash = o.APIHash
			}
			if ts.Host == o.Host && ts.HostKey == "" {
				ts.HostKey = o.HostKey
			}
			s.TgServer = &ts
		}
	}
	return s
}

func (a *App) SaveStorage(s model.Storage) (string, error) {
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		return "", errors.New("укажите название")
	}
	s = a.withSaved(s)
	if s.Kind == model.StorageTelegram {
		s.ChatID = strings.TrimSpace(s.ChatID)
		if s.ChatID == "" {
			return "", errors.New("укажите канал Telegram")
		}
		if s.TgServer == nil || s.TgServer.Key == "" {
			return "", errors.New("сначала установите Bot API на сервер")
		}
	}
	if strings.TrimSpace(s.Root) == "" {
		s.Root = "/Backupper"
	}
	s.Root = storage.Clean(s.Root)
	if s.ID == "" {
		s.ID = newID()
	}
	err := a.store.Update(func(c *desktop.Config) error {
		for i := range c.Storages {
			if c.Storages[i].ID == s.ID {
				c.Storages[i] = s
				return nil
			}
		}
		c.Storages = append(c.Storages, s)
		return nil
	})
	if err != nil {
		return "", err
	}
	a.mgr.Resync()
	return s.ID, nil
}

func (a *App) DeleteStorage(id string) error {
	err := a.store.Update(func(c *desktop.Config) error {
		for i := range c.Storages {
			if c.Storages[i].ID == id {
				c.Storages = append(c.Storages[:i], c.Storages[i+1:]...)
				return nil
			}
		}
		return nil
	})
	if err == nil {
		a.mgr.Resync()
	}
	return err
}

// TestStorage checks credentials from this PC (the unsaved form values are used,
// falling back to saved secrets when the fields were left blank).
func (a *App) TestStorage(s model.Storage) model.StorageTest {
	s = a.withSaved(s)
	if strings.TrimSpace(s.Root) == "" {
		s.Root = "/Backupper"
	}
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
	defer cancel()
	return storage.Test(ctx, s, 0)
}

// ---- Telegram Bot API server ----

// CheckTelegramServer looks at a server for the Bot API (read-only).
func (a *App) CheckTelegramServer(s model.Storage) desktop.TgServerCheck {
	s = a.withSaved(s)
	return desktop.CheckTelegramServer(s.TgServer)
}

type TgInstallResult struct {
	OK      bool          `json:"ok"`
	Error   string        `json:"error,omitempty"`
	Log     string        `json:"log"`
	Storage model.Storage `json:"storage"`
}

// InstallTelegramServer installs the Bot API and saves the storage with the
// relay key, so agents get it with the next sync.
func (a *App) InstallTelegramServer(s model.Storage) TgInstallResult {
	s = a.withSaved(s)
	if s.Token != "" && s.Token == a.store.Get().Settings.Telegram.BotToken {
		return TgInstallResult{Error: "этот бот уже шлёт уведомления через облачный Bot API — после переноса на свой сервер они перестанут работать. Создайте для хранения отдельного бота"}
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Minute)
	defer cancel()
	s.Kind = model.StorageTelegram
	out, log, err := desktop.InstallTelegramServer(ctx, s)
	res := TgInstallResult{Log: log}
	if err != nil {
		res.Error = err.Error()
		return res
	}
	if out.ID == "" {
		out.ID = newID()
	}
	if strings.TrimSpace(out.Root) == "" {
		out.Root = "/Backupper"
	}
	if strings.TrimSpace(out.Name) == "" {
		out.Name = "Telegram"
	}
	err = a.store.Update(func(c *desktop.Config) error {
		for i := range c.Storages {
			if c.Storages[i].ID == out.ID {
				c.Storages[i] = out
				return nil
			}
		}
		c.Storages = append(c.Storages, out)
		return nil
	})
	if err != nil {
		res.Error = err.Error()
		return res
	}
	a.mgr.Resync()
	out.TgServer.Key = ""
	res.OK, res.Storage = true, out
	return res
}

// TelegramChats lists chats where the bot was added, to pick the channel.
func (a *App) TelegramChats(s model.Storage) ([]storage.TelegramChat, error) {
	s = a.withSaved(s)
	ctx, cancel := context.WithTimeout(a.ctx, time.Minute)
	defer cancel()
	return storage.TelegramChats(ctx, s)
}

// ---- settings ----

func (a *App) SaveSettings(s desktop.Settings) (map[string]string, error) {
	err := a.store.Update(func(c *desktop.Config) error {
		c.Settings = s
		return nil
	})
	if err != nil {
		return nil, err
	}
	errs := map[string]string{}
	for id, e := range a.mgr.Resync() {
		errs[id] = e.Error()
	}
	return errs, nil
}

func (a *App) TestTelegram(t model.Telegram) error {
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	return agent.SendTelegram(ctx, t, "🔔 Backupper: уведомления настроены. Сюда будут приходить сообщения об ошибках бэкапов.")
}

func (a *App) OpenURL(u string) { runtime.BrowserOpenURL(a.ctx, u) }

// NextRuns previews the schedule in the server's time zone.
func (a *App) NextRuns(s model.Schedule, tzOffset int, count int) ([]string, error) {
	expr, err := s.CronExpr()
	if err != nil {
		return nil, err
	}
	sched, err := cron.ParseStandard(expr)
	if err != nil {
		return nil, fmt.Errorf("неверное расписание: %v", err)
	}
	loc := time.FixedZone("server", tzOffset)
	t := time.Now().In(loc)
	out := []string{}
	for i := 0; i < count; i++ {
		t = sched.Next(t)
		if t.IsZero() {
			break
		}
		out = append(out, t.Format(time.RFC3339))
	}
	return out, nil
}

// CMSPresets lists what "skip CMS caches and backups" leaves out of archives.
func (a *App) CMSPresets() []cms.Preset { return cms.Presets }

// RetentionPreview is the steady state of a retention policy: which copies stay
// once the schedule has run long enough to fill every rule.
type RetentionPreview struct {
	Ages    []float64 `json:"ages"`    // days before the newest copy, newest first
	Reasons []int     `json:"reasons"` // model.KeepReason bits per copy
	Assumed bool      `json:"assumed"` // manual or invalid schedule: daily runs assumed
}

func (a *App) RetentionPreview(s model.Schedule, tzOffset int, r model.Retention) RetentionPreview {
	var res RetentionPreview
	r = r.Normalized()
	if r.Empty() {
		return res
	}
	var sched cron.Schedule
	if s.Enabled {
		if expr, err := s.CronExpr(); err == nil {
			sched, _ = cron.ParseStandard(expr)
		}
	}
	if sched == nil {
		sched, _ = cron.ParseStandard("0 3 * * *")
		res.Assumed = true
	}
	days := max(r.Daily+1, r.Weekly*7+7, r.Monthly*31+31, r.Yearly*366+366)
	days = min(days, 20*366)
	t := time.Now().In(time.FixedZone("server", tzOffset))
	end := t.AddDate(0, 0, days)
	var times []time.Time
	for len(times) < 200000 {
		t = sched.Next(t)
		if t.IsZero() || (t.After(end) && len(times) > r.Last) {
			break
		}
		times = append(times, t)
	}
	if len(times) == 0 {
		return res
	}
	keep := r.Select(times)
	newest := times[len(times)-1]
	for i := len(times) - 1; i >= 0; i-- {
		if keep[i] != 0 {
			res.Ages = append(res.Ages, newest.Sub(times[i]).Hours()/24)
			res.Reasons = append(res.Reasons, int(keep[i]))
		}
	}
	return res
}
