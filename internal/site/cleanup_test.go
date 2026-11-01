package site

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"backupper/internal/model"
)

type cleanupRemote struct {
	files   map[string]bool
	offline bool
	removed []string
}

func (r *cleanupRemote) Upload(string, []byte) error { return nil }
func (r *cleanupRemote) Close() error                { return nil }
func (r *cleanupRemote) List(dir string) ([]string, error) {
	if r.offline {
		return nil, errors.New("offline")
	}
	var out []string
	for p := range r.files {
		if path.Dir(p) == dir {
			out = append(out, path.Base(p))
		}
	}
	return out, nil
}
func (r *cleanupRemote) Remove(p string) error {
	if r.offline {
		return errors.New("offline")
	}
	r.removed = append(r.removed, p)
	delete(r.files, p)
	return nil
}

func TestCleanupRetryPersistsAndSkipsActive(t *testing.T) {
	dir := t.TempDir()
	q, err := OpenCleanupQueue(dir)
	if err != nil {
		t.Fatal(err)
	}
	src := model.SiteSource{Host: "ftp.example", User: "user", Password: "secret", Root: "/site", DBPassword: "must-not-be-stored"}
	name := "backupper_0123456789abcdef"
	other := "backupper_1111111111111111"
	r := &cleanupRemote{files: map[string]bool{"/site/" + name + ".php": true, "/site/" + name + ".list.php": true, "/site/" + other + ".php": true}}
	q.dial = func(context.Context, model.SiteSource, HostKeys) (Remote, error) { return r, nil }
	if err := q.register(src, name); err != nil {
		t.Fatal(err)
	}
	if !q.HasPending(src) {
		t.Fatal("pending site was not detected")
	}
	q.retry(context.Background(), nil)
	if len(r.removed) != 0 {
		t.Fatal("removed an active session")
	}
	b, err := os.ReadFile(filepath.Join(dir, "site-cleanup.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 || strings.Contains(string(b), "must-not-be-stored") {
		t.Fatal("stored unrelated credentials")
	}
	if err := q.release(name, false); err != nil {
		t.Fatal(err)
	}
	r.offline = true
	q.retry(context.Background(), nil)
	if len(q.entries) != 1 {
		t.Fatal("lost pending entry after failure")
	}
	// A restarted agent must retry the saved entry without remembering it as active.
	q, err = OpenCleanupQueue(dir)
	if err != nil {
		t.Fatal(err)
	}
	r.offline = false
	q.dial = func(context.Context, model.SiteSource, HostKeys) (Remote, error) { return r, nil }
	q.retry(context.Background(), nil)
	if len(q.entries) != 0 || r.files["/site/"+name+".php"] || r.files["/site/"+name+".list.php"] {
		t.Fatal("pending files were not removed")
	}
	if q.HasPending(src) {
		t.Fatal("completed site still pending")
	}
	if !r.files["/site/"+other+".php"] {
		t.Fatal("removed another session")
	}
	q, err = OpenCleanupQueue(dir)
	if err != nil || len(q.entries) != 0 {
		t.Fatalf("cleanup was not saved: %v", err)
	}
}
