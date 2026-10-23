package agent

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	BinPath     = "/usr/local/bin/backupper-agent"
	UnitPath    = "/etc/systemd/system/backupper-agent.service"
	DefaultConf = "/etc/backupper/config.json"
	DefaultData = "/var/lib/backupper"
	DefaultPort = 7171
)

// Nice/IOScheduling keep backups from slowing down the sites on the server.
const unit = `[Unit]
Description=Backupper agent (scheduled backups to Yandex Disk / Mail.ru Cloud)
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=` + BinPath + ` serve
Restart=always
RestartSec=5
Nice=10
IOSchedulingClass=best-effort
IOSchedulingPriority=7
Environment=HOME=/root

[Install]
WantedBy=multi-user.target
`

type InstallResult struct {
	Token   string `json:"token"`
	Listen  string `json:"listen"`
	Version string `json:"version"`
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst) // works even while the old binary is running
}

func systemctl(args ...string) error {
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %v %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Install copies the binary, writes the config and the systemd unit, and (re)starts the service.
func Install(confPath string, port int, name, version string) (InstallResult, error) {
	var res InstallResult
	if os.Geteuid() != 0 {
		return res, errors.New("установка требует root (запустите через sudo)")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return res, errors.New("на сервере нет systemd — автоматическая установка не поддерживается")
	}
	self, err := os.Executable()
	if err != nil {
		return res, err
	}
	if self, err = filepath.EvalSymlinks(self); err != nil {
		return res, err
	}
	if self != BinPath {
		if err := copyFile(self, BinPath, 0o755); err != nil {
			return res, fmt.Errorf("копирование в %s: %w", BinPath, err)
		}
	}

	cfg, err := LoadConfig(confPath)
	if err != nil && !os.IsNotExist(err) {
		return res, fmt.Errorf("чтение %s: %w", confPath, err)
	}
	if cfg.Token == "" {
		b := make([]byte, 24)
		rand.Read(b)
		cfg.Token = hex.EncodeToString(b)
	}
	if port <= 0 {
		port = DefaultPort
	}
	cfg.Listen = fmt.Sprintf("127.0.0.1:%d", port)
	if name != "" {
		cfg.Settings.ServerName = name
	}
	if err := os.MkdirAll(filepath.Dir(confPath), 0o700); err != nil {
		return res, err
	}
	if err := SaveConfig(confPath, cfg); err != nil {
		return res, err
	}
	if err := os.MkdirAll(DefaultData, 0o700); err != nil {
		return res, err
	}
	if err := os.WriteFile(UnitPath, []byte(unit), 0o644); err != nil {
		return res, err
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", "backupper-agent"}, {"restart", "backupper-agent"}} {
		if err := systemctl(args...); err != nil {
			return res, err
		}
	}
	return InstallResult{Token: cfg.Token, Listen: cfg.Listen, Version: version}, nil
}

func Uninstall(confPath string, purge bool) error {
	if os.Geteuid() != 0 {
		return errors.New("удаление требует root")
	}
	_ = systemctl("disable", "--now", "backupper-agent")
	_ = os.Remove(UnitPath)
	_ = systemctl("daemon-reload")
	_ = os.Remove(BinPath)
	if purge {
		_ = os.RemoveAll(filepath.Dir(confPath))
		_ = os.RemoveAll(DefaultData)
	}
	return nil
}

func PrintJSON(v any) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}
