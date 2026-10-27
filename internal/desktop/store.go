// Package desktop is the panel side: local settings, SSH connections to
// servers, agent installation and calls to the agent API through the tunnel.
package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"backupper/internal/model"
)

type Server struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	KeyPath    string `json:"keyPath"`
	Passphrase string `json:"passphrase,omitempty"`
	AgentPort  int    `json:"agentPort"`
	Token      string `json:"token,omitempty"`
	HostKey    string `json:"hostKey,omitempty"` // pinned on first connection
}

type Settings struct {
	EncryptionPassword string         `json:"encryptionPassword"`
	Telegram           model.Telegram `json:"telegram"`
	Theme              string         `json:"theme"` // system | light | dark
	YandexClientID     string         `json:"yandexClientId"`
	Language           string         `json:"language"` // ru | en; empty = by Windows language
}

type Config struct {
	Servers  []Server        `json:"servers"`
	Storages []model.Storage `json:"storages"`
	Settings Settings        `json:"settings"`
}

// Store keeps the panel config in %APPDATA%\Backupper, encrypted with the
// Windows account key (DPAPI) because it holds passwords and tokens.
type Store struct {
	mu   sync.Mutex
	path string
	cfg  Config
}

func OpenStore() (*Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	dir = filepath.Join(dir, "Backupper")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "panel.dat")}
	if b, err := os.ReadFile(s.path); err == nil {
		plain, err := unprotect(b)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(plain, &s.cfg); err != nil {
			return nil, err
		}
	}
	if s.cfg.Settings.Theme == "" {
		s.cfg.Settings.Theme = "system"
	}
	return s, nil
}

func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(s.cfg)
	var c Config
	_ = json.Unmarshal(b, &c)
	return c
}

func (s *Store) Update(fn func(c *Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(s.cfg)
	var next Config
	_ = json.Unmarshal(b, &next)
	if err := fn(&next); err != nil {
		return err
	}
	plain, err := json.Marshal(next)
	if err != nil {
		return err
	}
	enc, err := protect(plain)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, enc, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	// Keep a fresh copy: fn may have stored pointers (e.g. Storage.TgServer) that the
	// caller still holds and changes afterwards, which would silently alter the config.
	var saved Config
	_ = json.Unmarshal(plain, &saved)
	s.cfg = saved
	return nil
}

func (s *Store) Server(id string) (Server, bool) {
	for _, sv := range s.Get().Servers {
		if sv.ID == id {
			return sv, true
		}
	}
	return Server{}, false
}
