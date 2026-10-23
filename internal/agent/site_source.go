package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"

	"backupper/internal/chunk"
	"backupper/internal/model"
	"backupper/internal/site"
)

// hostKeys pins SFTP host keys of hostings on first connection (known_hosts.json).
type hostKeys struct{ st *State }

func (h hostKeys) Check(addr string, key ssh.PublicKey) error {
	p := filepath.Join(h.st.paths.DataDir, "known_hosts.json")
	h.st.mu.Lock()
	defer h.st.mu.Unlock()
	known := map[string]string{}
	_ = readJSON(p, &known)
	got := base64.StdEncoding.EncodeToString(key.Marshal())
	if old, ok := known[addr]; ok {
		if old != got {
			return fmt.Errorf("ключ SFTP-сервера %s изменился (%s). Если хостинг менял сервер — удалите запись из %s", addr, ssh.FingerprintSHA256(key), p)
		}
		return nil
	}
	known[addr] = got
	b, _ := json.MarshalIndent(known, "", "  ")
	_ = os.MkdirAll(h.st.paths.DataDir, 0o700)
	return os.WriteFile(p, b, 0o600)
}

func (a *Agent) hostKeys() site.HostKeys { return hostKeys{a.state} }

func describeSite(i model.SiteInfo) string {
	parts := []string{"PHP " + i.PHP}
	if i.CMS != "" {
		parts = append(parts, map[string]string{"bitrix": "1С-Битрикс", "wordpress": "WordPress"}[i.CMS])
	}
	if i.TimeLimit > 0 {
		parts = append(parts, fmt.Sprintf("лимит времени %d с", i.TimeLimit))
	}
	if i.MemoryLimit > 0 {
		parts = append(parts, "память "+chunk.HumanBytes(i.MemoryLimit))
	}
	if i.DB != nil && i.DB.Error == "" {
		parts = append(parts, fmt.Sprintf("база %s (%d табл., %s, из %s)", i.DB.Name, i.DB.Tables, chunk.HumanBytes(int64(i.DB.Size)), i.DB.From))
	}
	return strings.Join(parts, ", ")
}

// planSource expands any source into streams; the cleanup runs after all of them.
func (a *Agent) planSource(ctx context.Context, src model.Source, logf func(level, format string, a ...any)) ([]stream, func(), error) {
	if src.Type != model.SourceSite {
		streams, err := planStreams(ctx, src, logf)
		return streams, func() {}, err
	}
	if src.Site == nil {
		return nil, nil, fmt.Errorf("не заданы параметры подключения к хостингу")
	}
	cfg := *src.Site
	sess, err := site.Open(ctx, cfg, a.hostKeys(), logf)
	if err != nil {
		return nil, nil, err
	}
	logf("info", "Хостинг %s: %s", cfg.Host, describeSite(sess.Info))
	for _, w := range sess.Warnings() {
		logf("warn", "%s", w)
	}
	sess.SetExcludes(src.Excludes)

	name := Slug(defaultName(src))
	mk := func(action, file, label string) stream {
		return stream{file: file, label: label, open: func(ctx context.Context) (*started, error) {
			var raw atomic.Int64
			rc := sess.Stream(ctx, action, func(n int64) { raw.Add(n) })
			return &started{
				r:             rc,
				precompressed: true,
				raw:           raw.Load,
				wait:          func() (bool, error) { return false, nil },
				cleanup:       func() { rc.Close() },
			}, nil
		}}
	}
	var out []stream
	if cfg.DB {
		label := "Сайт " + cfg.Host + ": база данных"
		if sess.Info.DB != nil {
			label = "Сайт " + cfg.Host + ": база " + sess.Info.DB.Name
		}
		out = append(out, mk("db", name+".sql", label))
	}
	if cfg.Files {
		out = append(out, mk("files", name+".tar", "Сайт "+cfg.Host+": файлы"))
	}
	return out, sess.Close, nil
}

// probe checks a source before saving; for a hosting it uploads the script, asks it and removes it.
func (a *Agent) probe(ctx context.Context, src model.Source) model.ProbeResult {
	if src.Type != model.SourceSite {
		return Probe(ctx, src)
	}
	var res model.ProbeResult
	if src.Site == nil {
		res.Error = "не заданы параметры подключения"
		return res
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var notes []string
	sess, err := site.Open(ctx, *src.Site, a.hostKeys(), func(level, format string, args ...any) {
		if level != "info" {
			notes = append(notes, fmt.Sprintf(format, args...))
		}
	})
	if err != nil {
		res.Error = err.Error()
		return res
	}
	sess.Close()
	info := sess.Info
	info.Warnings = append(sess.Warnings(), notes...)
	res.Site = &info
	res.OK = true
	return res
}
