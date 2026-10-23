package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/klauspost/compress/gzip"
	"golang.org/x/net/webdav"

	"backupper/internal/chunk"
	"backupper/internal/model"
)

// TestHelperDump plays the role of mysqldump: writes deterministic data to stdout.
func TestHelperDump(t *testing.T) {
	if os.Getenv("BACKUPPER_HELPER") != "1" {
		t.Skip()
	}
	size, _ := strconv.Atoi(os.Getenv("BACKUPPER_SIZE"))
	os.Stdout.Write(testData(size))
	os.Exit(0)
}

func testData(size int) []byte {
	r := rand.New(rand.NewSource(42))
	b := make([]byte, size)
	// Half random, half repetitive: realistic compression ratio.
	for i := 0; i < size; i += 4096 {
		end := min(i+4096, size)
		if (i/4096)%2 == 0 {
			r.Read(b[i:end])
		} else {
			copy(b[i:end], bytes.Repeat([]byte("INSERT INTO t VALUES (1,'x');\n"), 200))
		}
	}
	return b
}

func helperStream(size int) stream {
	return stream{file: "db.sql", label: "test dump", build: func(ctx context.Context) (*exec.Cmd, func(), error) {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperDump")
		cmd.Env = append(os.Environ(), "BACKUPPER_HELPER=1", "BACKUPPER_SIZE="+strconv.Itoa(size))
		return cmd, func() {}, nil
	}}
}

type davServer struct {
	*httptest.Server
	fs       webdav.FileSystem
	maxPut   int64
	putCount int
}

func newDAV(t *testing.T) *davServer {
	d := &davServer{fs: webdav.NewMemFS()}
	h := &webdav.Handler{FileSystem: d.fs, LockSystem: webdav.NewMemLS()}
	d.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			d.putCount++
			if d.maxPut > 0 && r.ContentLength > d.maxPut {
				io.Copy(io.Discard, r.Body)
				http.Error(w, "too large", http.StatusRequestEntityTooLarge)
				return
			}
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(d.Close)
	return d
}

func newTestAgent(t *testing.T, davURL string) *Agent {
	dir := t.TempDir()
	cfg := Config{
		Token:    "secret",
		Settings: model.Settings{ServerName: "Тест сервер", EncryptionPassword: "pa$$word"},
		Storages: []model.Storage{{ID: "dav", Name: "DAV", Kind: model.StorageWebDAV, URL: davURL, Login: "u", Password: "p", Root: "/Backupper"}},
		Jobs:     []model.Job{{ID: "j1", Name: "Сайт", Storages: []string{"dav"}, Keep: 2, Encrypt: true}},
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

func streamOnce(t *testing.T, a *Agent, size int, plan model.ChunkPlan) (*model.Run, model.RunFile) {
	t.Helper()
	job, _ := a.state.Job("j1")
	run := &model.Run{ID: NewID(), JobID: "j1", Uploads: map[string]model.UploadStat{}, Progress: &model.Progress{}}
	a.state.AddRun(run)
	runDir := path.Join(a.jobDir(job), time.Now().Format(runDirLayout))
	dests, err := a.openDests(job, runDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range dests {
		if err := d.st.MkdirAll(context.Background(), d.dir); err != nil {
			t.Fatal(err)
		}
	}
	rec, _ := age.NewScryptRecipient("pa$$word")
	rec.SetWorkFactor(10)
	rf, _ := a.runStream(context.Background(), run, helperStream(size), "db.sql.gz.age", dests, plan, 6, rec)
	rf.Encrypted = true
	run.RemoteDir = runDir
	return run, rf
}

func TestStreamEncryptUploadDownload(t *testing.T) {
	dav := newDAV(t)
	a := newTestAgent(t, dav.URL)
	const size = 40 << 20
	plan := model.ChunkPlan{Size: chunk.MinSize, Buffer: model.BufferMemory}
	run, rf := streamOnce(t, a, size, plan)
	if rf.Error != "" {
		t.Fatalf("stream error: %s\nlog: %+v", rf.Error, a.state.Run(run.ID).Log)
	}
	if rf.RawSize != size {
		t.Fatalf("raw size %d, want %d", rf.RawSize, size)
	}
	if len(rf.Parts) < 2 {
		t.Fatalf("expected several parts, got %d", len(rf.Parts))
	}
	t.Logf("raw %s → %s in %d parts", chunk.HumanBytes(rf.RawSize), chunk.HumanBytes(rf.Size), len(rf.Parts))

	// Write the manifest like doRun does, then download through the API.
	job, _ := a.state.Job("j1")
	dests, _ := a.openDests(job, run.RemoteDir)
	man := model.Manifest{Status: model.RunSuccess, Files: []model.RunFile{rf}}
	mb, _ := json.Marshal(man)
	if err := dests[0].st.Put(context.Background(), path.Join(dests[0].dir, "manifest.json"), bytes.NewReader(mb), int64(len(mb))); err != nil {
		t.Fatal(err)
	}

	api := httptest.NewServer(a.Handler())
	defer api.Close()
	req, _ := http.NewRequest("GET", api.URL+"/api/download?storage=dav&decrypt=1&file=db.sql.gz.age&dir="+run.RemoteDir, nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("download: %d %s", resp.StatusCode, b)
	}
	if !strings.Contains(resp.Header.Get("Content-Disposition"), "db.sql.gz") {
		t.Fatalf("bad filename header %q", resp.Header.Get("Content-Disposition"))
	}
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

	// Unauthorized access must be refused.
	resp2, _ := http.Get(api.URL + "/api/info")
	if resp2.StatusCode != 401 {
		t.Fatalf("expected 401 without token, got %d", resp2.StatusCode)
	}
}

func TestPartSplitWhenCloudRejectsSize(t *testing.T) {
	dav := newDAV(t)
	dav.maxPut = 10 << 20 // cloud refuses parts over 10 MB
	a := newTestAgent(t, dav.URL)
	plan := model.ChunkPlan{Size: 16 * chunk.MB, Buffer: model.BufferMemory}
	run, rf := streamOnce(t, a, 60<<20, plan)
	if rf.Error != "" {
		t.Fatalf("stream error: %s\nlog: %+v", rf.Error, a.state.Run(run.ID).Log)
	}
	for i, p := range rf.Parts {
		if p.Size > dav.maxPut {
			t.Fatalf("part %s is %d bytes, over the limit", p.Name, p.Size)
		}
		if want := fmt.Sprintf("db.sql.gz.age.part%04d", i+1); p.Name != want {
			t.Fatalf("part %d named %s, want %s", i, p.Name, want)
		}
	}
	t.Logf("%d parts after splitting", len(rf.Parts))
}

func TestDiskBuffer(t *testing.T) {
	dav := newDAV(t)
	a := newTestAgent(t, dav.URL)
	t.Setenv("BACKUPPER_TMP", t.TempDir())
	plan := model.ChunkPlan{Size: chunk.MinSize, Buffer: model.BufferDisk}
	_, rf := streamOnce(t, a, 20<<20, plan)
	if rf.Error != "" {
		t.Fatal(rf.Error)
	}
	left, _ := os.ReadDir(os.Getenv("BACKUPPER_TMP"))
	if len(left) != 0 {
		t.Fatalf("temporary part files left behind: %d", len(left))
	}
}

func TestRotation(t *testing.T) {
	dav := newDAV(t)
	a := newTestAgent(t, dav.URL)
	job, _ := a.state.Job("j1")
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 3, 0, 0, 0, time.Local)
	var last dest
	for i := 0; i < 5; i++ {
		dir := path.Join(a.jobDir(job), base.Add(time.Duration(i)*24*time.Hour).Format(runDirLayout))
		ds, _ := a.openDests(job, dir)
		d := ds[0]
		d.st.MkdirAll(ctx, d.dir)
		status := model.RunSuccess
		if i == 3 {
			status = model.RunFailed
		}
		if i != 1 { // run #1 has no manifest (interrupted upload)
			mb, _ := json.Marshal(model.Manifest{Status: status})
			d.st.Put(ctx, path.Join(d.dir, "manifest.json"), bytes.NewReader(mb), int64(len(mb)))
		}
		last = d
	}
	run := &model.Run{ID: "r"}
	a.state.AddRun(run)
	a.rotate(ctx, run, last, 2)
	entries, err := last.st.List(ctx, path.Dir(last.dir))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	sort.Strings(names)
	// Kept: day 5 (current) and day 3 (previous successful); days 1, 2 (no manifest) and 4 (failed) removed.
	want := []string{"2026-01-03_03-00-00", "2026-01-05_03-00-00"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("after rotation: %v, want %v", names, want)
	}
}

func waitNextSecond() {
	now := time.Now()
	time.Sleep(time.Until(now.Truncate(time.Second).Add(time.Second + 50*time.Millisecond)))
}
