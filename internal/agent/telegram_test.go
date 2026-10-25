package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/gzip"

	"backupper/internal/chunk"
	"backupper/internal/model"
	"backupper/internal/storage/telegramtest"
)

func newTelegramAgent(t *testing.T, f *telegramtest.Fake) *Agent {
	dir := t.TempDir()
	cfg := Config{
		Token:    "secret",
		Settings: model.Settings{ServerName: "re308", EncryptionPassword: "pa$$word"},
		Storages: []model.Storage{{ID: "tg", Name: "Telegram", Kind: model.StorageTelegram, Token: telegramtest.Token, ChatID: telegramtest.Chat, Root: "/Backupper",
			TgServer: &model.TelegramServer{DirectURL: f.URL}}},
		Jobs: []model.Job{{ID: "j1", Name: "Сайт", Storages: []string{"tg"}, Keep: 2, Encrypt: true}},
	}
	conf := filepath.Join(dir, "config.json")
	if err := SaveConfig(conf, cfg); err != nil {
		t.Fatal(err)
	}
	st, err := OpenState(Paths{Config: conf, DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	return New(st, "test")
}

// The whole way through Telegram: parts → albums → manifest → /api/backups →
// /api/download with decryption.
func TestTelegramUploadDownload(t *testing.T) {
	f := telegramtest.New(t)
	a := newTelegramAgent(t, f)
	const size = 24 << 20
	run, rf := streamOnce(t, a, size, model.ChunkPlan{Size: chunk.MinSize, Buffer: model.BufferMemory})
	if rf.Error != "" {
		t.Fatalf("stream error: %s\nlog: %+v", rf.Error, a.state.Run(run.ID).Log)
	}
	if len(rf.Parts) < 2 {
		t.Fatalf("expected several parts, got %d", len(rf.Parts))
	}
	job, _ := a.state.Job("j1")
	dests, _ := a.openDests(job, run.RemoteDir)
	mb, _ := json.Marshal(model.Manifest{Server: "re308", JobName: "Сайт", Status: model.RunSuccess, Files: []model.RunFile{rf}})
	if err := dests[0].st.Put(context.Background(), path.Join(dests[0].dir, "manifest.json"), bytes.NewReader(mb), int64(len(mb))); err != nil {
		t.Fatal(err)
	}

	api := httptest.NewServer(a.Handler())
	defer api.Close()
	get := func(u string) *http.Response {
		req, _ := http.NewRequest("GET", api.URL+u, nil)
		req.Header.Set("Authorization", "Bearer secret")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("%s: %d %s", u, resp.StatusCode, b)
		}
		return resp
	}

	resp := get("/api/backups?job=j1&storage=tg")
	var list []model.BackupEntry
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 1 || !list[0].Complete || list[0].Status != model.RunSuccess {
		t.Fatalf("backups: %+v", list)
	}

	resp = get("/api/download?storage=tg&decrypt=1&file=db.sql.gz.age&dir=" + run.RemoteDir)
	defer resp.Body.Close()
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(gz)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(got) != sha256.Sum256(testData(size)) {
		t.Fatal("restored data differs from the original")
	}
	if left, _ := filepath.Glob(filepath.Join(f.Dir, "*", "documents", "*")); len(left) != 0 {
		t.Fatalf("downloads left on the Bot API server: %v", left)
	}
}

func TestTelegramNeedsEncryption(t *testing.T) {
	a := newTelegramAgent(t, telegramtest.New(t))
	api := httptest.NewServer(a.Handler())
	defer api.Close()
	body, _ := json.Marshal(model.Job{Name: "Открытая", Storages: []string{"tg"}, Sources: []model.Source{{Type: model.SourceFiles, Paths: []string{"/etc"}}}})
	req, _ := http.NewRequest("PUT", api.URL+"/api/jobs/new", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 400 || !strings.Contains(string(b), "шифрование") {
		t.Fatalf("a job without encryption must be refused: %d %s", resp.StatusCode, b)
	}
}
