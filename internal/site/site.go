// Package site backs up sites on shared hosting where there is no shell:
// a one-off PHP script is uploaded over FTP/FTPS/SFTP, asked over HTTPS for the
// database dump and the files piece by piece, and deleted right after.
//
// Every request is signed (HMAC-SHA256) and every response is encrypted and
// authenticated (AES-256-GCM) with a secret that exists only for this run.
// Each response is one gzip member; their concatenation is a valid .gz file.
package site

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"backupper/internal/cms"
	"backupper/internal/model"
)

//go:embed script.php
var scriptTpl []byte

const placeholder = "__BK_SECRET__"

// Script names are random and distinctive, so leftovers of a crashed run can be
// recognised and removed without touching anything else on the hosting.
var staleRe = regexp.MustCompile(`^backupper_[0-9a-f]{16}(\.list)?\.php$`)

type Logf func(level, format string, args ...any)

type Session struct {
	src    model.SiteSource
	keys   HostKeys
	remote Remote
	logf   Logf

	name   string
	secret string
	macKey []byte
	encKey []byte
	url    string
	http   *http.Client
	skew   time.Duration

	Info     model.SiteInfo
	sec      float64
	maxBytes int64
	excludes []string
}

type meta struct {
	State json.RawMessage `json:"state"`
	Done  bool            `json:"done"`
	Raw   int64           `json:"raw"`
	Est   float64         `json:"est"` // files: size of the whole tar, sent with the file list
	Log   []string        `json:"log"`
	Err   string          `json:"err"`
	Info  *model.SiteInfo `json:"info"`
}

func (s *Session) scriptPath() string { return path.Join(s.src.Root, s.name+".php") }
func (s *Session) listPath() string   { return path.Join(s.src.Root, s.name+".list.php") }

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func keyFrom(prefix, secret string) []byte {
	h := sha256.Sum256([]byte(prefix + secret))
	return h[:]
}

// Script renders the PHP script for a secret (exported for tests).
func Script(secret string) []byte {
	return bytes.Replace(scriptTpl, []byte(placeholder), []byte(secret), 1)
}

func validate(src model.SiteSource) error {
	u, err := url.Parse(strings.TrimSpace(src.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("укажите адрес сайта, например https://site.ru")
	}
	if strings.TrimSpace(src.Root) == "" {
		return errors.New("укажите папку сайта на FTP, например /public_html")
	}
	if !src.DB && !src.Files {
		return errors.New("выберите, что сохранять: базу данных и/или файлы")
	}
	return nil
}

// Open uploads the script and checks the hosting. Close must be called in any case.
func Open(ctx context.Context, src model.SiteSource, keys HostKeys, logf Logf) (*Session, error) {
	if err := validate(src); err != nil {
		return nil, err
	}
	src.Root = "/" + strings.Trim(strings.ReplaceAll(src.Root, "\\", "/"), "/")
	if src.Root == "/" {
		src.Root = ""
	}
	s := &Session{src: src, keys: keys, logf: logf, sec: 20, maxBytes: 8 << 20}
	s.secret = randHex(32)
	s.name = "backupper_" + randHex(8)
	s.macKey = keyFrom("mac", s.secret)
	s.encKey = keyFrom("enc", s.secret)
	s.url = strings.TrimRight(strings.TrimSpace(src.URL), "/") + "/" + s.name + ".php"
	jar, _ := cookiejar.New(nil)
	s.http = &http.Client{
		Jar: jar,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: src.Insecure},
			TLSHandshakeTimeout:   30 * time.Second,
			ResponseHeaderTimeout: 15 * time.Minute,
		},
		// A redirect would turn the POST into a GET; report it instead of following.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	r, err := Dial(ctx, src, keys)
	if err != nil {
		return nil, err
	}
	s.remote = r
	s.removeStale()
	if err := s.remote.Upload(s.scriptPath(), Script(s.secret)); err != nil {
		s.remote.Close()
		return nil, fmt.Errorf("не удалось загрузить скрипт в %s: %w", s.scriptPath(), err)
	}
	logf("info", "Скрипт загружен на хостинг: %s", s.scriptPath())

	if err := s.checkClock(ctx); err != nil {
		s.Close()
		return nil, err
	}
	var m meta
	if _, err := s.call(ctx, "info", s.baseParams(), &m); err != nil {
		s.Close()
		return nil, err
	}
	if m.Info == nil {
		s.Close()
		return nil, errors.New("скрипт не вернул сведения о хостинге")
	}
	s.Info = *m.Info
	s.tune()
	return s, nil
}

func (s *Session) removeStale() {
	names, err := s.remote.List(s.rootOrDot())
	if err != nil {
		return
	}
	for _, n := range names {
		if staleRe.MatchString(n) {
			if err := s.remote.Remove(path.Join(s.src.Root, n)); err == nil {
				s.logf("warn", "Удалён забытый скрипт прошлого запуска: %s", n)
			}
		}
	}
}

func (s *Session) rootOrDot() string {
	if s.src.Root == "" {
		return "/"
	}
	return s.src.Root
}

// checkClock learns the hosting clock from the Date header: requests carry a timestamp.
func (s *Session) checkClock(ctx context.Context) error {
	var resp *http.Response
	for try := 0; ; try++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
		req.Header.Set("User-Agent", userAgent)
		var err error
		resp, err = s.http.Do(req)
		if err != nil {
			return fmt.Errorf("сайт %s не отвечает: %w", s.src.URL, friendlyTLS(err))
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if try > 0 || !s.passChallenge(body) {
			break
		}
	}
	if loc := resp.Header.Get("Location"); resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return fmt.Errorf("сайт перенаправляет на %s — укажите в адресе сайта конечный адрес (например, с https или без www)", loc)
	}
	if t, err := http.ParseTime(resp.Header.Get("Date")); err == nil {
		s.skew = time.Until(t)
	}
	return nil
}

const userAgent = "Mozilla/5.0 (compatible; Backupper/1.0)"

// Bot protection of some hostings (Beget) answers every request without its cookie
// with a tiny page that sets it from JavaScript and reloads, and refuses POST with 405.
var challengeRe = regexp.MustCompile(`document\.cookie\s*=\s*['"]([A-Za-z0-9_.\-]+)=([^;'"]*)`)

// passChallenge stores the cookie such a page sets; false if body is not one.
func (s *Session) passChallenge(body []byte) bool {
	if len(body) > 4096 || !bytes.Contains(body, []byte("location.reload")) {
		return false
	}
	m := challengeRe.FindSubmatch(body)
	if m == nil {
		return false
	}
	u, _ := url.Parse(s.url)
	s.http.Jar.SetCookies(u, []*http.Cookie{{Name: string(m[1]), Value: string(m[2]), Path: "/"}})
	s.logf("info", "Сайт защищён от ботов проверкой cookie (%s) — агент её проходит", m[1])
	return true
}

func (s *Session) baseParams() map[string]any {
	p := map[string]any{"cms": s.src.CMS, "sec": s.sec, "max": s.maxBytes}
	if s.src.CMS == "" {
		p["cms"] = "auto"
	}
	if s.src.DBName != "" {
		p["db"] = map[string]string{"host": s.src.DBHost, "name": s.src.DBName, "user": s.src.DBUser, "pass": s.src.DBPassword}
	}
	// "info" found which settings work (CMS files or the panel's): keep to them.
	if db := s.Info.DB; db != nil && db.Error == "" && db.From != "" {
		p["dbFrom"] = map[bool]string{true: "panel", false: "cms"}[db.From == "панель"]
	}
	return p
}

// tune adapts the request size and duration to the hosting limits.
func (s *Session) tune() {
	if tl := s.Info.TimeLimit; tl > 0 {
		s.sec = min(20, float64(tl)*0.5)
		if s.sec < 3 {
			s.sec = 3
		}
	}
	if ml := s.Info.MemoryLimit; ml > 0 {
		s.maxBytes = min(8<<20, max(512<<10, ml/8))
	}
}

// Warnings explains what may go wrong on this hosting before the backup runs.
func (s *Session) Warnings() []string {
	var w []string
	i := s.Info
	if s.src.DB {
		switch {
		case !i.PDOMySQL:
			w = append(w, "на хостинге нет расширения pdo_mysql — базу сохранить не получится")
		case i.DB == nil && i.DBError != "":
			w = append(w, i.DBError)
		case i.DB == nil:
			w = append(w, "не найдены настройки базы данных — укажите их вручную")
		case i.DB.Error != "":
			w = append(w, "не удалось подключиться к базе: "+i.DB.Error)
		case i.DB.Note != "":
			w = append(w, "доступ к базе из файлов CMS не подошёл ("+i.DB.Note+") — используется указанный вручную")
		}
	}
	if !i.Deflate {
		w = append(w, "в PHP нет потокового сжатия (deflate_init) — бэкап будет медленнее и потребует больше памяти")
	}
	return w
}

func (s *Session) sign(action, params string) url.Values {
	t := strconv.FormatInt(time.Now().Add(s.skew).Unix(), 10)
	n := randHex(8)
	mac := hmac.New(sha256.New, s.macKey)
	mac.Write([]byte(action + "|" + t + "|" + n + "|" + params))
	return url.Values{"a": {action}, "p": {params}, "t": {t}, "n": {n}, "s": {hex.EncodeToString(mac.Sum(nil))}}
}

type retryable struct{ error }

// post sends one signed request; every attempt is signed anew (fresh nonce).
func (s *Session) post(ctx context.Context, action, params string) (*http.Response, []byte, error) {
	form := s.sign(action, params)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	resp, err := s.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, retryable{fmt.Errorf("запрос к скрипту: %w", err)}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		return nil, nil, retryable{fmt.Errorf("ответ скрипта оборвался: %w", err)}
	}
	return resp, body, nil
}

func (s *Session) call(ctx context.Context, action string, params map[string]any, m *meta) ([]byte, error) {
	pj, _ := json.Marshal(params)
	resp, body, err := s.post(ctx, action, string(pj))
	if err == nil && s.passChallenge(body) {
		resp, body, err = s.post(ctx, action, string(pj))
	}
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == 404:
		return nil, fmt.Errorf("по адресу %s скрипт не найден. Проверьте, что папка %s на FTP — это корень сайта %s", s.url, s.rootOrDot(), s.src.URL)
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return nil, fmt.Errorf("сайт перенаправляет запрос на %s — укажите конечный адрес сайта", resp.Header.Get("Location"))
	case resp.StatusCode == 405:
		return nil, fmt.Errorf("хостинг не принимает POST-запросы к сайту (HTTP 405) — скорее всего, мешает защита от ботов или DDoS: отключите её для сайта в панели хостинга (%s)", snippet(body))
	case resp.StatusCode != 200:
		return nil, retryable{fmt.Errorf("хостинг ответил HTTP %d: %s", resp.StatusCode, snippet(body))}
	}
	plain, err := s.open(body)
	if err != nil {
		if bytes.HasPrefix(bytes.TrimSpace(body), []byte("<?php")) {
			return nil, errors.New("сервер отдаёт PHP-файл как текст — PHP по этому адресу не выполняется")
		}
		return nil, retryable{fmt.Errorf("вместо ответа скрипта пришло: %s", snippet(body))}
	}
	if len(plain) < 4 {
		return nil, retryable{errors.New("пустой ответ скрипта")}
	}
	ml := binary.BigEndian.Uint32(plain[:4])
	if int(ml) > len(plain)-4 {
		return nil, retryable{errors.New("повреждённый ответ скрипта")}
	}
	if err := json.Unmarshal(plain[4:4+ml], m); err != nil {
		return nil, err
	}
	if m.Err != "" {
		if strings.Contains(m.Err, "Allowed memory size") || strings.Contains(m.Err, "Maximum execution time") {
			return nil, retryable{fmt.Errorf("скрипт упёрся в лимит хостинга: %s", m.Err)}
		}
		return nil, errors.New(m.Err)
	}
	return plain[4+ml:], nil
}

func (s *Session) open(body []byte) ([]byte, error) {
	if len(body) < 12+16 {
		return nil, errors.New("short")
	}
	block, err := aes.NewCipher(s.encKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, body[:12], body[12:], nil)
}

func snippet(b []byte) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	if s == "" {
		return "(пусто)"
	}
	return s
}

// Stream returns the gzip stream of the database dump ("db") or of the files ("files").
// raw receives the uncompressed byte count as the stream advances, est the expected
// uncompressed total once the script knows it (either may be nil).
func (s *Session) Stream(ctx context.Context, action string, raw, est func(int64)) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		var state json.RawMessage
		for {
			params := s.baseParams()
			if action == "files" {
				params["exclude"] = s.excludes
				params["cmsExcludes"] = s.src.CMSExcludes
				if s.src.CMSExcludes {
					params["presets"] = cms.Presets
				}
			}
			if state != nil {
				params["state"] = state
			}
			var m meta
			data, err := s.callRetry(ctx, action, params, &m)
			if err != nil {
				pw.CloseWithError(err)
				return
			}
			for _, l := range m.Log {
				s.logf("info", "хостинг: %s", l)
			}
			if raw != nil && m.Raw > 0 {
				raw(m.Raw)
			}
			if est != nil && m.Est > 0 {
				est(int64(m.Est))
			}
			if len(data) > 0 {
				if _, err := pw.Write(data); err != nil {
					return
				}
			}
			if m.Done {
				pw.Close()
				return
			}
			state = m.State
		}
	}()
	return pr
}

// SetExcludes sets file patterns skipped in the archive.
func (s *Session) SetExcludes(p []string) { s.excludes = p }

func (s *Session) callRetry(ctx context.Context, action string, params map[string]any, m *meta) ([]byte, error) {
	backoff := []time.Duration{3 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second}
	for attempt := 0; ; attempt++ {
		*m = meta{}
		data, err := s.call(ctx, action, params, m)
		var re retryable
		if err == nil || !errors.As(err, &re) || attempt >= len(backoff) {
			if err != nil && errors.As(err, &re) {
				err = re.error
			}
			return data, err
		}
		// The hosting may have killed the script for time or memory: ask for less next time.
		if s.maxBytes > 512<<10 {
			s.maxBytes /= 2
		}
		if s.sec > 5 {
			s.sec = max(5, s.sec/2)
		}
		params["max"], params["sec"] = s.maxBytes, s.sec
		s.logf("warn", "хостинг: %v — повтор через %s, порция уменьшена до %d КБ", re.error, backoff[attempt], s.maxBytes>>10)
		select {
		case <-time.After(backoff[attempt]):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Close removes the script and its temporary list from the hosting.
func (s *Session) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	var m meta
	_, _ = s.call(ctx, "cleanup", map[string]any{}, &m)
	cancel()

	remove := func() error {
		err := s.remote.Remove(s.scriptPath())
		_ = s.remote.Remove(s.listPath())
		return err
	}
	err := remove()
	if err != nil {
		// The control connection often times out during a long backup: reconnect once.
		s.remote.Close()
		rctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		r, derr := Dial(rctx, s.src, s.keys)
		cancel()
		if derr == nil {
			s.remote = r
			err = remove()
		} else {
			err = derr
		}
	}
	if err != nil {
		s.logf("error", "НЕ удалось удалить скрипт %s с хостинга: %v. Он защищён подписью, но удалите его вручную (или он удалится при следующем запуске)", s.scriptPath(), err)
	} else {
		s.logf("info", "Скрипт удалён с хостинга")
	}
	s.remote.Close()
}
