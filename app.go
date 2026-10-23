package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	"backupper/internal/desktop"
	"backupper/internal/model"
	"backupper/internal/storage"
	"backupper/internal/version"
)

// App is bound to the frontend: every exported method is callable from JS.
type App struct {
	ctx   context.Context
	store *desktop.Store
	mgr   *desktop.Manager
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
	st := State{Version: version.Version, Storages: cfg.Storages, Settings: cfg.Settings, Keys: desktop.DefaultKeys()}
	if st.Storages == nil {
		st.Storages = []model.Storage{}
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

func (a *App) Active(id string) ([]*model.Run, error) {
	var runs []*model.Run
	err := a.mgr.Call(id, http.MethodGet, "/api/active", nil, &runs, 15*time.Second)
	return runs, err
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
	return runs, err
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
	return out, err
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

func (a *App) SaveStorage(s model.Storage) (string, error) {
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		return "", errors.New("укажите название")
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
				if s.Password == "" {
					s.Password = c.Storages[i].Password
				}
				if s.Token == "" {
					s.Token = c.Storages[i].Token
				}
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
	for _, old := range a.store.Get().Storages {
		if old.ID == s.ID && s.ID != "" {
			if s.Password == "" {
				s.Password = old.Password
			}
			if s.Token == "" {
				s.Token = old.Token
			}
		}
	}
	if strings.TrimSpace(s.Root) == "" {
		s.Root = "/Backupper"
	}
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
	defer cancel()
	return storage.Test(ctx, s, 0)
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
