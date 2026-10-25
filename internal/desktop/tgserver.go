package desktop

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"backupper/internal/model"
	"backupper/internal/storage"
)

// The Bot API runs from the aiogram image (a build of the official tdlib/telegram-bot-api)
// as the relay user, so the files it downloads belong to that user and agents can
// read and delete them over SFTP.
const (
	tgImage   = "docker.io/aiogram/telegram-bot-api:latest"
	tgService = "backupper-tg-bot-api"
	tgData    = "/var/lib/backupper-tg"
	tgHome    = "/home/" + model.TelegramRelayUser
)

// TgServerCheck is what the panel learns about a candidate Bot API server.
type TgServerCheck struct {
	OK           bool     `json:"ok"`
	Error        string   `json:"error,omitempty"`
	OS           string   `json:"os"`
	Arch         string   `json:"arch"`
	Root         bool     `json:"root"`
	Sudo         bool     `json:"sudo"`
	Systemd      bool     `json:"systemd"`
	Runtime      string   `json:"runtime"` // docker | podman | "" (will be installed)
	MemTotal     int64    `json:"memTotal"`
	MemAvailable int64    `json:"memAvailable"`
	DiskFree     int64    `json:"diskFree"` // on /var/lib
	TelegramOK   bool     `json:"telegramOk"`
	TelegramMs   int      `json:"telegramMs"` // TLS handshake with api.telegram.org
	SpeedBps     float64  `json:"speedBps"`   // download from telegram.org
	AgentVersion string   `json:"agentVersion"`
	Installed    bool     `json:"installed"`
	Running      bool     `json:"running"`
	Fingerprint  string   `json:"fingerprint"`
	HostKey      string   `json:"hostKey"`
	Warnings     []string `json:"warnings,omitempty"`
}

func adminServer(ts *model.TelegramServer) Server {
	return Server{Host: ts.Host, Port: ts.Port, User: ts.AdminUser, KeyPath: ts.AdminKeyPath, Passphrase: ts.AdminPassphrase, HostKey: ts.HostKey}
}

func dialAdmin(ts *model.TelegramServer) (*ssh.Client, string, error) {
	if ts == nil || strings.TrimSpace(ts.Host) == "" {
		return nil, "", errors.New("укажите адрес сервера Bot API")
	}
	return Dial(adminServer(ts))
}

const tgCheckScript = `echo "arch=$(uname -m)"
echo "uid=$(id -u)"
echo "sudo=$(sudo -n true >/dev/null 2>&1 && echo yes || echo no)"
echo "systemd=$(command -v systemctl >/dev/null && echo yes || echo no)"
echo "os=$(. /etc/os-release 2>/dev/null && echo "$PRETTY_NAME" || uname -s)"
echo "runtime=$( (command -v docker >/dev/null && echo docker) || (command -v podman >/dev/null && echo podman) || echo none)"
awk '/MemTotal:/{print "mem="$2*1024} /MemAvailable:/{print "avail="$2*1024}' /proc/meminfo
echo "disk=$(df -Pk /var/lib 2>/dev/null | awk 'NR==2{print $4*1024}')"
echo "agent=$(test -x /usr/local/bin/backupper-agent && /usr/local/bin/backupper-agent version || echo none)"
echo "installed=$(test -f /etc/systemd/system/` + tgService + `.service && echo yes || echo no)"
echo "running=$(systemctl is-active ` + tgService + ` 2>/dev/null || echo no)"
curl -s -o /dev/null -m 20 -w "tg=%{http_code} %{time_appconnect}\n" https://api.telegram.org/ || echo "tg=000 0"
curl -sL -o /dev/null -m 30 -r 0-52428799 -w "speed=%{speed_download}\n" https://telegram.org/dl/desktop/linux || echo "speed=0"
`

// CheckTelegramServer looks at a server before the Bot API goes there: access,
// resources and whether Telegram is reachable from it. It changes nothing.
func CheckTelegramServer(ts *model.TelegramServer) TgServerCheck {
	var r TgServerCheck
	c, seen, err := dialAdmin(ts)
	r.HostKey, r.Fingerprint = seen, Fingerprint(seen)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	defer c.Close()
	out, err := Exec(c, tgCheckScript, nil)
	if err != nil && out == "" {
		r.Error = err.Error()
		return r
	}
	kv := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(l), "="); ok {
			kv[k] = strings.TrimSpace(v)
		}
	}
	num := func(k string) int64 { n, _ := strconv.ParseFloat(kv[k], 64); return int64(n) }
	r.Arch, r.OS = kv["arch"], kv["os"]
	r.Root, r.Sudo, r.Systemd = kv["uid"] == "0", kv["sudo"] == "yes", kv["systemd"] == "yes"
	if kv["runtime"] != "none" {
		r.Runtime = kv["runtime"]
	}
	r.MemTotal, r.MemAvailable, r.DiskFree = num("mem"), num("avail"), num("disk")
	if v := kv["agent"]; v != "none" {
		r.AgentVersion = v
	}
	r.Installed, r.Running = kv["installed"] == "yes", kv["running"] == "active"
	if code, tls, ok := strings.Cut(kv["tg"], " "); ok && code != "000" {
		r.TelegramOK = true
		f, _ := strconv.ParseFloat(tls, 64)
		r.TelegramMs = int(f * 1000)
	}
	r.SpeedBps, _ = strconv.ParseFloat(kv["speed"], 64)

	if !r.Root && !r.Sudo {
		r.Error = "нужен вход под root или пользователь с sudo без пароля (NOPASSWD)"
		return r
	}
	if !r.Systemd {
		r.Error = "на сервере нет systemd"
		return r
	}
	if !r.TelegramOK {
		r.Error = "с сервера не открывается api.telegram.org — выберите сервер, откуда Telegram доступен"
		return r
	}
	if r.Arch != "x86_64" && r.Arch != "aarch64" {
		r.Warnings = append(r.Warnings, "образ Bot API собран для x86_64 и arm64, а здесь "+r.Arch)
	}
	if r.DiskFree > 0 && r.DiskFree < 3<<30 {
		r.Warnings = append(r.Warnings, "мало места в /var/lib: при скачивании копии части по 2 ГБ временно ложатся на диск")
	}
	if r.MemTotal > 0 && r.MemTotal < 512<<20 {
		r.Warnings = append(r.Warnings, "меньше 512 МБ памяти: Bot API может работать нестабильно")
	}
	if r.TelegramMs > 1000 {
		r.Warnings = append(r.Warnings, "соединение с Telegram медленное (больше секунды на TLS)")
	}
	if r.SpeedBps > 0 && r.SpeedBps < 2<<20 {
		r.Warnings = append(r.Warnings, "скорость скачивания с серверов Telegram ниже 2 МБ/с")
	}
	r.OK = true
	return r
}

// newRelayKey makes the key agents use to log in as the relay user.
func newRelayKey() (private, authorized string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	block, err := ssh.MarshalPrivateKey(priv, "backupper-agents")
	if err != nil {
		return "", "", err
	}
	sp, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(block)), strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sp))), nil
}

func authorizedFor(privatePEM string) (string, error) {
	s, err := ssh.ParsePrivateKey([]byte(privatePEM))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(s.PublicKey()))), nil
}

func installScript(apiPort int, authorized, apiID, apiHash string) string {
	port := strconv.Itoa(apiPort)
	return `set -e
say() { echo "== $*"; }
U=` + model.TelegramRelayUser + `
DATA=` + tgData + `
HOMED=` + tgHome + `
IMAGE=` + tgImage + `
if command -v docker >/dev/null 2>&1; then RT=$(command -v docker)
elif command -v podman >/dev/null 2>&1; then RT=$(command -v podman)
else
  say "Устанавливаю контейнерный движок"
  if command -v apt-get >/dev/null 2>&1; then export DEBIAN_FRONTEND=noninteractive; apt-get update -q >/dev/null && apt-get install -y -q docker.io >/dev/null
  elif command -v dnf >/dev/null 2>&1; then dnf install -y -q podman >/dev/null
  elif command -v yum >/dev/null 2>&1; then yum install -y -q podman >/dev/null
  else echo "не удалось определить пакетный менеджер — установите Docker вручную" >&2; exit 1; fi
  RT=$(command -v docker || command -v podman)
fi
AFTER="network-online.target"
case "$RT" in *docker) systemctl enable --now docker >/dev/null 2>&1 || true; AFTER="network-online.target docker.service";; esac
say "Движок: $RT"

say "Пользователь $U (только проброс порта Bot API и SFTP)"
NOLOGIN=$(command -v nologin || echo /sbin/nologin)
id -u $U >/dev/null 2>&1 || useradd --system --create-home --home-dir $HOMED --shell $NOLOGIN $U
mkdir -p $HOMED/.ssh $DATA/tmp
printf '%s\n' 'restrict,port-forwarding,permitopen="127.0.0.1:` + port + `",command="internal-sftp" ` + authorized + `' > $HOMED/.ssh/authorized_keys
chown -R $U:$U $HOMED/.ssh $DATA
chmod 755 $HOMED; chmod 700 $HOMED/.ssh $DATA; chmod 600 $HOMED/.ssh/authorized_keys
command -v restorecon >/dev/null 2>&1 && restorecon -R $HOMED >/dev/null 2>&1 || true
if command -v sshd >/dev/null 2>&1 && sshd -T 2>/dev/null | grep -qi '^allowtcpforwarding no'; then
  echo "ВНИМАНИЕ: в sshd_config AllowTcpForwarding no — агенты не смогут подключиться к Bot API" >&2
fi

install -d -m 700 /etc/backupper-tg
( umask 077; printf 'TELEGRAM_API_ID=%s\nTELEGRAM_API_HASH=%s\n' '` + apiID + `' '` + apiHash + `' > /etc/backupper-tg/env )

say "Загружаю образ $IMAGE"
$RT pull -q $IMAGE >/dev/null
VOL="$DATA:$DATA"
case "$RT" in *podman) VOL="$VOL:Z";; esac
UG="$(id -u $U):$(id -g $U)"

cat > /etc/systemd/system/` + tgService + `.service <<UNIT
[Unit]
Description=Telegram Bot API server (local mode) for Backupper
After=$AFTER
Wants=network-online.target

[Service]
ExecStartPre=-$RT rm -f ` + tgService + `
ExecStart=$RT run --rm --name ` + tgService + ` --user $UG --env-file /etc/backupper-tg/env -p 127.0.0.1:` + port + `:8081 -v $VOL --entrypoint /usr/local/bin/telegram-bot-api $IMAGE --local --http-port=8081 --dir=$DATA --temp-dir=$DATA/tmp
ExecStop=$RT stop -t 20 ` + tgService + `
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
UNIT

# Parts are read and deleted by agents at once; this only catches leftovers.
cat > /etc/systemd/system/backupper-tg-clean.service <<UNIT
[Unit]
Description=Remove old downloads of the Backupper Bot API server

[Service]
Type=oneshot
ExecStart=/bin/sh -c 'find $DATA -type f -path "$DATA/*/documents/*" -mmin +120 -delete; find $DATA/tmp -type f -mmin +360 -delete'
UNIT
cat > /etc/systemd/system/backupper-tg-clean.timer <<UNIT
[Unit]
Description=Hourly cleanup of Backupper Bot API downloads

[Timer]
OnCalendar=hourly
Persistent=true

[Install]
WantedBy=timers.target
UNIT

systemctl daemon-reload
systemctl enable --now backupper-tg-clean.timer >/dev/null 2>&1
systemctl enable ` + tgService + ` >/dev/null 2>&1
systemctl restart ` + tgService + `
say "Служба ` + tgService + ` запущена"
`
}

// shellSafe keeps api_id/api_hash inside single quotes of the install script.
func shellSafe(s string) bool {
	return s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
	}) < 0
}

// InstallTelegramServer puts telegram-bot-api on the server as a systemd service
// listening on 127.0.0.1, creates the relay user for agents, moves the bot off the
// cloud Bot API (logOut) and checks the whole way an agent will use. It returns
// the storage with the relay key and the pinned host key filled in.
func InstallTelegramServer(ctx context.Context, s model.Storage) (model.Storage, string, error) {
	var log strings.Builder
	ts := s.TgServer
	if ts == nil {
		return s, "", errors.New("не настроен сервер Bot API")
	}
	if s.Token == "" {
		return s, "", errors.New("укажите токен бота от @BotFather")
	}
	if !shellSafe(ts.APIID) || !shellSafe(ts.APIHash) {
		return s, "", errors.New("укажите api_id и api_hash с my.telegram.org")
	}
	chk := CheckTelegramServer(ts)
	if !chk.OK {
		return s, "", errors.New(chk.Error)
	}
	ts.HostKey = chk.HostKey
	c, _, err := dialAdmin(ts)
	if err != nil {
		return s, "", err
	}
	defer c.Close()
	sudo, err := sudoPrefix(ServerCheck{Root: chk.Root, Sudo: chk.Sudo})
	if err != nil {
		return s, "", err
	}
	if ts.APIPort == 0 {
		ts.APIPort = 8081
	}
	ts.User = model.TelegramRelayUser
	var authorized string
	if ts.Key != "" {
		authorized, err = authorizedFor(ts.Key) // reinstall keeps the key agents already have
	}
	if ts.Key == "" || err != nil {
		ts.Key, authorized, err = newRelayKey()
		if err != nil {
			return s, "", err
		}
	}

	// The cloud Bot API must let the bot go before a local server serves it.
	if out, err := Exec(c, "curl -s -m 30 -K -", strings.NewReader(`url = "https://api.telegram.org/bot`+s.Token+`/logOut"`+"\n")); err == nil {
		var res struct {
			OK          bool   `json:"ok"`
			Description string `json:"description"`
		}
		_ = json.Unmarshal([]byte(out), &res)
		if res.OK {
			log.WriteString("== Бот отключён от облачного Bot API (logOut)\n")
		} else {
			fmt.Fprintf(&log, "== logOut: %s (бот уже отключён от облачного API — это нормально)\n", res.Description)
		}
	} else {
		fmt.Fprintf(&log, "== logOut не выполнен: %v\n", err)
	}

	out, err := Exec(c, sudo+"sh -s", strings.NewReader(installScript(ts.APIPort, authorized, ts.APIID, ts.APIHash)))
	log.WriteString(out)
	if err != nil {
		return s, log.String(), fmt.Errorf("установка Bot API: %w", err)
	}

	// Through the admin connection: is the service answering?
	hc := &http.Client{Transport: &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		return c.Dial("tcp", "127.0.0.1:"+strconv.Itoa(ts.APIPort))
	}}, Timeout: 20 * time.Second}
	var bot string
	deadline := time.Now().Add(2 * time.Minute)
	for {
		bot, err = getMe(ctx, hc, ts.APIPort, s.Token)
		if err == nil || time.Now().After(deadline) || ctx.Err() != nil {
			break
		}
		time.Sleep(3 * time.Second)
	}
	if err != nil {
		jl, _ := Exec(c, sudo+"journalctl -u "+tgService+" -n 20 --no-pager", nil)
		log.WriteString(jl)
		return s, log.String(), fmt.Errorf("Bot API не отвечает: %w", err)
	}
	fmt.Fprintf(&log, "== Bot API отвечает, бот @%s\n", bot)

	// And the way agents go: relay user, port forwarding, SFTP.
	s.TgServer = ts
	ts.Installed = time.Now().Format(time.RFC3339)
	if name, err := storage.TelegramBot(ctx, s.ForAgent()); err != nil {
		return s, log.String(), fmt.Errorf("Bot API работает, но доступ для агентов не проходит: %w", err)
	} else {
		fmt.Fprintf(&log, "== Доступ для агентов через %s@%s проверен (@%s)\n", ts.User, ts.Host, name)
	}
	return s, log.String(), nil
}

func getMe(ctx context.Context, hc *http.Client, port int, token string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(port)+"/bot"+token+"/getMe", nil)
	resp, err := hc.Do(req)
	if err != nil {
		return "", errors.New(strings.ReplaceAll(err.Error(), token, "…"))
	}
	defer resp.Body.Close()
	var res struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      struct {
			Username string `json:"username"`
		} `json:"result"`
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := json.Unmarshal(b, &res); err != nil {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if !res.OK {
		return "", errors.New(res.Description)
	}
	return res.Result.Username, nil
}
