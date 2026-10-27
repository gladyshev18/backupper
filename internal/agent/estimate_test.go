package agent

import (
	"testing"

	"backupper/internal/model"
)

// The run total starts from the last run and follows what the sources count.
func TestRunEstimate(t *testing.T) {
	e := &runEstimate{files: map[string]int64{"site.tar.gz": 60, "site.sql.gz": 40}}
	r := &model.Run{EstRawBytes: 100, Progress: &model.Progress{}}

	setFileEst(r, e, "site.tar.gz", e.fileEst("site.tar.gz", 0))
	if r.Progress.EstRaw != 60 || r.EstRawBytes != 100 {
		t.Fatalf("from history: file %d, run %d", r.Progress.EstRaw, r.EstRawBytes)
	}
	// The hosting counted the files: 80 instead of 60 last time.
	setFileEst(r, e, "site.tar.gz", e.fileEst("site.tar.gz", 80))
	if r.Progress.EstRaw != 80 || r.EstRawBytes != 120 {
		t.Fatalf("counted: file %d, run %d", r.Progress.EstRaw, r.EstRawBytes)
	}
	// A file new to this job adds to the total.
	r.Progress = &model.Progress{}
	setFileEst(r, e, "new.sql.gz", e.fileEst("new.sql.gz", 30))
	if r.EstRawBytes != 150 {
		t.Fatalf("new file: run %d", r.EstRawBytes)
	}

	// Without history only the file has an estimate.
	r = &model.Run{Progress: &model.Progress{}}
	setFileEst(r, &runEstimate{}, "site.tar.gz", 500)
	if r.Progress.EstRaw != 500 || r.EstRawBytes != 0 {
		t.Fatalf("first run: file %d, run %d", r.Progress.EstRaw, r.EstRawBytes)
	}
}
