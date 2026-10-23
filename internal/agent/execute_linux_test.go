package agent

import (
	"archive/tar"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/klauspost/compress/gzip"

	"backupper/internal/model"
)

// TestExecuteFilesJob runs a whole job with the real GNU tar: files + configs
// sources, manifest, rotation and download through the API.
func TestExecuteFilesJob(t *testing.T) {
	site := t.TempDir()
	os.MkdirAll(filepath.Join(site, "cache"), 0o755)
	os.WriteFile(filepath.Join(site, "index.php"), []byte("<?php echo 1;"), 0o644)
	os.WriteFile(filepath.Join(site, "error.log"), []byte("noise"), 0o644)
	os.WriteFile(filepath.Join(site, "cache", "x.tmp"), []byte("cache"), 0o644)
	conf := t.TempDir()
	os.WriteFile(filepath.Join(conf, "nginx.conf"), []byte("server {}"), 0o644)

	dav := newDAV(t)
	a := newTestAgent(t, dav.URL)
	job, _ := a.state.Job("j1")
	job.Encrypt = false
	job.Keep = 1
	job.Sources = []model.Source{
		{ID: "s1", Type: model.SourceFiles, Name: "site", Paths: []string{site}, Excludes: []string{"*.log", "*/cache/*"}},
		{ID: "s2", Type: model.SourceConfigs, Name: "configs", Paths: []string{conf, "/etc/definitely-missing-dir"}},
	}
	a.state.update(func(c *Config) error { c.Jobs[0] = job; return nil })

	var last *model.Run
	for i := 0; i < 2; i++ {
		run := &model.Run{ID: NewID(), JobID: "j1", JobName: job.Name}
		a.state.AddRun(run)
		a.execute(context.Background(), run, job)
		last = a.state.Run(run.ID)
		if last.Status != model.RunSuccess {
			t.Fatalf("status %s: %s\n%+v", last.Status, last.Error, last.Log)
		}
		if i == 0 {
			// Make the next run land in a different second-precision folder.
			waitNextSecond()
		}
	}

	// keep=1 → only the latest folder survives.
	ds, _ := a.openDests(job, last.RemoteDir)
	entries, _ := ds[0].st.List(context.Background(), filepath.ToSlash(filepath.Dir(ds[0].dir)))
	if len(entries) != 1 {
		t.Fatalf("rotation left %d folders", len(entries))
	}

	api := httptest.NewServer(a.Handler())
	defer api.Close()
	req, _ := http.NewRequest("GET", api.URL+"/api/download?storage=dav&file=site.tar.gz&dir="+last.RemoteDir, nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("download: %v %v", err, resp.Status)
	}
	gz, _ := gzip.NewReader(resp.Body)
	tr := tar.NewReader(gz)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, filepath.Base(h.Name))
	}
	sort.Strings(names)
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "index.php") || strings.Contains(joined, "error.log") || strings.Contains(joined, "x.tmp") {
		t.Fatalf("unexpected archive content: %s", joined)
	}
	t.Logf("archive: %s", joined)
	for _, l := range last.Log {
		t.Logf("[%s] %s", l.Level, l.Msg)
	}
}
