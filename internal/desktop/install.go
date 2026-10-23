package desktop

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"backupper/agentbin"
	"backupper/internal/model"
)

const agentBin = "/usr/local/bin/backupper-agent"

// ServerCheck is what the panel learns about a server over plain SSH.
type ServerCheck struct {
	OK             bool   `json:"ok"`
	Error          string `json:"error,omitempty"`
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	Root           bool   `json:"root"`
	Sudo           bool   `json:"sudo"`
	Systemd        bool   `json:"systemd"`
	AgentInstalled bool   `json:"agentInstalled"`
	AgentVersion   string `json:"agentVersion"`
	Fingerprint    string `json:"fingerprint"`
}

func goArch(uname string) string {
	switch strings.TrimSpace(uname) {
	case "x86_64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	}
	return ""
}

// Probe checks SSH access, privileges and whether the agent is present.
func Probe(c *ssh.Client) ServerCheck {
	var r ServerCheck
	script := `uname -m; id -u; (sudo -n true >/dev/null 2>&1 && echo sudo-ok || echo sudo-no); ` +
		`(command -v systemctl >/dev/null && echo systemd-ok || echo systemd-no); ` +
		`(. /etc/os-release 2>/dev/null && echo "$PRETTY_NAME" || uname -s); ` +
		`(test -x ` + agentBin + ` && ` + agentBin + ` version || echo none)`
	out, err := Exec(c, script, nil)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for len(lines) < 6 {
		lines = append(lines, "")
	}
	r.Arch = strings.TrimSpace(lines[0])
	r.Root = strings.TrimSpace(lines[1]) == "0"
	r.Sudo = strings.TrimSpace(lines[2]) == "sudo-ok"
	r.Systemd = strings.TrimSpace(lines[3]) == "systemd-ok"
	r.OS = strings.TrimSpace(lines[4])
	if v := strings.TrimSpace(lines[5]); v != "none" && v != "" {
		r.AgentInstalled = true
		r.AgentVersion = v
	}
	r.OK = true
	return r
}

func sudoPrefix(chk ServerCheck) (string, error) {
	if chk.Root {
		return "", nil
	}
	if chk.Sudo {
		return "sudo -n ", nil
	}
	return "", errors.New("нужен вход под root или пользователь с sudo без пароля (NOPASSWD)")
}

type installOut struct {
	Token   string `json:"token"`
	Listen  string `json:"listen"`
	Version string `json:"version"`
}

// InstallAgent uploads the embedded agent and registers it as a systemd service.
func (m *Manager) InstallAgent(id string) (string, error) {
	s, ok := m.store.Server(id)
	if !ok {
		return "", errors.New("сервер не найден")
	}
	m.Drop(id)
	c, err := m.SSH(id)
	if err != nil {
		return "", err
	}
	chk := Probe(c)
	if !chk.OK {
		return "", errors.New(chk.Error)
	}
	if !chk.Systemd {
		return "", errors.New("на сервере нет systemd — установка агента не поддерживается")
	}
	sudo, err := sudoPrefix(chk)
	if err != nil {
		return "", err
	}
	arch := goArch(chk.Arch)
	if arch == "" {
		return "", fmt.Errorf("архитектура %s не поддерживается", chk.Arch)
	}
	bin, err := agentbin.Binary(arch)
	if err != nil {
		return "", err
	}
	// Home directory, not /tmp: /tmp is often mounted noexec.
	tmp := "$HOME/.backupper-agent.new"
	if _, err := Exec(c, "cat > "+tmp+" && chmod 755 "+tmp, bytes.NewReader(bin)); err != nil {
		return "", fmt.Errorf("загрузка агента: %w", err)
	}
	port := s.AgentPort
	if port == 0 {
		port = 7171
	}
	cmd := fmt.Sprintf("%s%s install --port %d --name %s; rc=$?; rm -f %s; exit $rc", sudo, tmp, port, shellQuote(s.Name), tmp)
	out, err := Exec(c, cmd, nil)
	if err != nil {
		return "", fmt.Errorf("установка агента: %w", err)
	}
	var res installOut
	if err := json.Unmarshal([]byte(strings.TrimSpace(lastLine(out))), &res); err != nil || res.Token == "" {
		return "", fmt.Errorf("неожиданный ответ установщика: %s", out)
	}
	if err := m.store.Update(func(cfg *Config) error {
		for i := range cfg.Servers {
			if cfg.Servers[i].ID == id {
				cfg.Servers[i].Token = res.Token
				cfg.Servers[i].AgentPort = port
			}
		}
		return nil
	}); err != nil {
		return "", err
	}
	m.Drop(id)
	// Wait for the service to come up.
	var info model.AgentInfo
	deadline := time.Now().Add(15 * time.Second)
	for {
		err = m.Call(id, http.MethodGet, "/api/info", nil, &info, 10*time.Second)
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		return "", fmt.Errorf("агент установлен, но не отвечает: %w (проверьте journalctl -u backupper-agent)", err)
	}
	return info.Version, nil
}

// AdoptAgent reads the token of an agent that is already installed (e.g. from another PC).
func (m *Manager) AdoptAgent(id string) error {
	c, err := m.SSH(id)
	if err != nil {
		return err
	}
	chk := Probe(c)
	if !chk.AgentInstalled {
		return ErrNoAgent
	}
	sudo, err := sudoPrefix(chk)
	if err != nil {
		return err
	}
	out, err := Exec(c, sudo+agentBin+" token", nil)
	if err != nil {
		return err
	}
	var res installOut
	if err := json.Unmarshal([]byte(strings.TrimSpace(lastLine(out))), &res); err != nil || res.Token == "" {
		return fmt.Errorf("не удалось прочитать токен агента: %s", out)
	}
	port := 7171
	if i := strings.LastIndex(res.Listen, ":"); i >= 0 {
		fmt.Sscanf(res.Listen[i+1:], "%d", &port)
	}
	m.Drop(id)
	return m.store.Update(func(cfg *Config) error {
		for i := range cfg.Servers {
			if cfg.Servers[i].ID == id {
				cfg.Servers[i].Token = res.Token
				cfg.Servers[i].AgentPort = port
			}
		}
		return nil
	})
}

func (m *Manager) UninstallAgent(id string, purge bool) error {
	c, err := m.SSH(id)
	if err != nil {
		return err
	}
	chk := Probe(c)
	sudo, err := sudoPrefix(chk)
	if err != nil {
		return err
	}
	cmd := sudo + agentBin + " uninstall"
	if purge {
		cmd += " --purge"
	}
	if _, err := Exec(c, cmd, nil); err != nil {
		return err
	}
	m.Drop(id)
	return m.store.Update(func(cfg *Config) error {
		for i := range cfg.Servers {
			if cfg.Servers[i].ID == id {
				cfg.Servers[i].Token = ""
			}
		}
		return nil
	})
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}
