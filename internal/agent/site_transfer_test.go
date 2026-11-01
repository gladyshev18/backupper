package agent

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"backupper/internal/model"
)

func TestSiteTransferPauseExportImport(t *testing.T) {
	a := newTestAgent(t, "http://127.0.0.1:1")
	if err := a.state.update(func(c *Config) error {
		c.Jobs[0].Kind = model.JobSite
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	request := func(method, path string, body []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer secret")
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		return w
	}
	if w := request(http.MethodGet, "/api/site-transfer?job=j1&name=%D0%A1%D0%B0%D0%B9%D1%82", nil); w.Code != http.StatusConflict {
		t.Fatalf("export without pause: %d", w.Code)
	}
	if _, err := a.Enqueue("j1", "test"); err != nil {
		t.Fatal(err)
	}
	if w := request(http.MethodPost, "/api/jobs/j1/site-pause", nil); w.Code != http.StatusConflict {
		t.Fatalf("paused queued site: %d", w.Code)
	}
	a.mu.Lock()
	a.queue = nil
	a.mu.Unlock()
	if w := request(http.MethodPost, "/api/jobs/j1/site-pause", nil); w.Code != http.StatusOK {
		t.Fatalf("pause: %d %s", w.Code, w.Body.String())
	}
	if _, err := a.Enqueue("j1", "test"); err == nil {
		t.Fatal("paused site accepted run")
	}
	// Remove the queued test record before exporting only completed runs.
	a.state.mu.Lock()
	a.state.runs = nil
	a.state.mu.Unlock()
	a.state.AddRun(&model.Run{ID: "run", JobID: "j1", JobName: "Сайт", Status: model.RunSuccess, Log: []model.LogLine{{Msg: "log"}}})
	w := request(http.MethodGet, "/api/site-transfer?job=j1&name=%D0%A1%D0%B0%D0%B9%D1%82", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("export: %d %s", w.Code, w.Body.String())
	}
	var data siteTransfer
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil || len(data.Runs) != 1 || len(data.Runs[0].Log) != 1 {
		t.Fatalf("bad export: %#v %v", data, err)
	}
	data.Runs[0].ID = "imported"
	b, _ := json.Marshal(data)
	for i := 0; i < 2; i++ {
		if w := request(http.MethodPost, "/api/jobs/j1/site-transfer", b); w.Code != http.StatusOK {
			t.Fatalf("import: %d %s", w.Code, w.Body.String())
		}
	}
	if got := a.state.Runs("j1", 0, true); len(got) != 2 {
		t.Fatalf("duplicate import: %d", len(got))
	}
}

func TestImportSiteRunsIdempotentAndDurable(t *testing.T) {
	dir := t.TempDir()
	s := &State{paths: Paths{DataDir: dir}}
	now := time.Now().UTC()
	s.runs = []*model.Run{{ID: "existing", JobID: "other", QueuedAt: now}}
	in := []*model.Run{{ID: "old", JobID: "old-job", QueuedAt: now.Add(-time.Hour), Status: model.RunSuccess, Log: []model.LogLine{{Msg: "log"}}}}
	if err := s.ImportSiteRuns("new-job", in); err != nil {
		t.Fatal(err)
	}
	if err := s.ImportSiteRuns("new-job", in); err != nil {
		t.Fatal(err)
	}
	if len(s.runs) != 2 || s.runs[0].JobID != "new-job" || len(s.runs[0].Log) != 1 {
		t.Fatalf("unexpected history: %#v", s.runs)
	}
	var disk []*model.Run
	if err := readJSON(filepath.Join(dir, "history.json"), &disk); err != nil || len(disk) != 2 {
		t.Fatalf("history not saved: %v %#v", err, disk)
	}
	if err := s.ImportSiteRuns("different", in); err == nil {
		t.Fatal("accepted duplicate run from another job")
	}
}

func TestImportSiteRunsRejectsOverflowWithoutRemovingExisting(t *testing.T) {
	s := &State{paths: Paths{DataDir: t.TempDir()}}
	for i := 0; i < maxRuns; i++ {
		s.runs = append(s.runs, &model.Run{ID: NewID(), JobID: "other"})
	}
	if err := s.ImportSiteRuns("site", []*model.Run{{ID: "new", Status: model.RunSuccess}}); err == nil {
		t.Fatal("accepted overflow")
	}
	if len(s.runs) != maxRuns {
		t.Fatal("existing history changed")
	}
}
