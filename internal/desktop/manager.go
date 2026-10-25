package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"backupper/internal/model"
)

// ErrNoAgent means SSH works but the agent is not installed (or not running).
var ErrNoAgent = errors.New("агент не установлен на сервере")

type conn struct {
	ssh    *ssh.Client
	http   *http.Client
	synced bool
}

// Manager keeps one SSH connection per server and tunnels agent API calls through it.
type Manager struct {
	store *Store
	mu    sync.Mutex
	conns map[string]*conn
	dial  sync.Map // serverID -> *sync.Mutex, so one server is dialed once at a time
}

func NewManager(store *Store) *Manager {
	return &Manager{store: store, conns: map[string]*conn{}}
}

func (m *Manager) lockFor(id string) *sync.Mutex {
	v, _ := m.dial.LoadOrStore(id, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func alive(c *ssh.Client) bool {
	done := make(chan error, 1)
	go func() {
		_, _, err := c.SendRequest("keepalive@openssh.com", true, nil)
		done <- err
	}()
	select {
	case err := <-done:
		return err == nil
	case <-time.After(5 * time.Second):
		return false
	}
}

// SSH returns a live SSH connection to the server, reconnecting if needed.
func (m *Manager) SSH(id string) (*ssh.Client, error) {
	c, err := m.get(id)
	if err != nil {
		return nil, err
	}
	return c.ssh, nil
}

func (m *Manager) get(id string) (*conn, error) {
	l := m.lockFor(id)
	l.Lock()
	defer l.Unlock()
	m.mu.Lock()
	c := m.conns[id]
	m.mu.Unlock()
	if c != nil && alive(c.ssh) {
		return c, nil
	}
	if c != nil {
		c.ssh.Close()
	}
	s, ok := m.store.Server(id)
	if !ok {
		return nil, errors.New("сервер не найден")
	}
	client, seen, err := Dial(s)
	if err != nil {
		return nil, err
	}
	if s.HostKey == "" && seen != "" {
		_ = m.store.Update(func(cfg *Config) error {
			for i := range cfg.Servers {
				if cfg.Servers[i].ID == id {
					cfg.Servers[i].HostKey = seen
				}
			}
			return nil
		})
	}
	port := s.AgentPort
	if port == 0 {
		port = 7171
	}
	c = &conn{ssh: client}
	c.http = &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return client.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port))
		},
		MaxIdleConns:    4,
		IdleConnTimeout: 60 * time.Second,
	}}
	m.mu.Lock()
	m.conns[id] = c
	m.mu.Unlock()
	return c, nil
}

// Drop closes the connection (after settings change or on errors).
func (m *Manager) Drop(id string) {
	m.mu.Lock()
	c := m.conns[id]
	delete(m.conns, id)
	m.mu.Unlock()
	if c != nil {
		c.ssh.Close()
	}
}

func (m *Manager) Connected() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for id := range m.conns {
		out = append(out, id)
	}
	return out
}

func (m *Manager) CloseAll() {
	for _, id := range m.Connected() {
		m.Drop(id)
	}
}

// forAgents drops what only the panel needs (admin access to the Bot API server).
func forAgents(in []model.Storage) []model.Storage {
	out := make([]model.Storage, len(in))
	for i, s := range in {
		out[i] = s.ForAgent()
	}
	return out
}

func (m *Manager) SyncPayload(s Server) model.SyncPayload {
	cfg := m.store.Get()
	return model.SyncPayload{
		Settings: model.Settings{
			ServerName:         s.Name,
			EncryptionPassword: cfg.Settings.EncryptionPassword,
			Telegram:           cfg.Settings.Telegram,
		},
		Storages: forAgents(cfg.Storages),
	}
}

func (m *Manager) request(ctx context.Context, id, method, path string, body any) (*http.Response, error) {
	s, ok := m.store.Server(id)
	if !ok {
		return nil, errors.New("сервер не найден")
	}
	if s.Token == "" {
		return nil, ErrNoAgent
	}
	c, err := m.get(id)
	if err != nil {
		return nil, err
	}
	if !c.synced && path != "/api/sync" {
		if err := m.callConn(ctx, c, s, http.MethodPut, "/api/sync", m.SyncPayload(s), nil); err != nil {
			return nil, err
		}
		c.synced = true
	}
	return m.doConn(ctx, c, s, method, path, body)
}

func (m *Manager) doConn(ctx context.Context, c *conn, s Server, method, path string, body any) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://agent"+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "connect failed") || strings.Contains(err.Error(), "Connection refused") {
			return nil, fmt.Errorf("%w (служба backupper-agent не отвечает)", ErrNoAgent)
		}
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		var e struct {
			Error string `json:"error"`
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		if json.Unmarshal(b, &e) == nil && e.Error != "" {
			return nil, errors.New(e.Error)
		}
		return nil, fmt.Errorf("агент ответил HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

func (m *Manager) callConn(ctx context.Context, c *conn, s Server, method, path string, body, out any) error {
	resp, err := m.doConn(ctx, c, s, method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Call performs an agent API call, with one reconnect on a broken tunnel.
func (m *Manager) Call(id, method, path string, body, out any, timeout time.Duration) error {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	resp, err := m.request(ctx, id, method, path, body)
	if err != nil && !errors.Is(err, ErrNoAgent) && ctx.Err() == nil && isNetErr(err) {
		m.Drop(id)
		resp, err = m.request(ctx, id, method, path, body)
	}
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Stream opens a long response (downloads); the caller closes the body.
func (m *Manager) Stream(ctx context.Context, id, path string) (*http.Response, error) {
	return m.request(ctx, id, http.MethodGet, path, nil)
}

// Resync pushes settings and storages to every connected server.
func (m *Manager) Resync() map[string]error {
	errs := map[string]error{}
	for _, id := range m.Connected() {
		s, ok := m.store.Server(id)
		if !ok || s.Token == "" {
			continue
		}
		if err := m.Call(id, http.MethodPut, "/api/sync", m.SyncPayload(s), nil, 20*time.Second); err != nil {
			errs[id] = err
		}
	}
	return errs
}

func isNetErr(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "EOF") || strings.Contains(msg, "closed") || strings.Contains(msg, "reset")
}
