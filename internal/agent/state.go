package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"backupper/internal/model"
)

// Config is the agent's persistent configuration (/etc/backupper/config.json).
type Config struct {
	Token    string          `json:"token"`
	Listen   string          `json:"listen"`
	Settings model.Settings  `json:"settings"`
	Storages []model.Storage `json:"storages"`
	Jobs     []model.Job     `json:"jobs"`
}

// StorageStats remembers measured speed and limits of each destination.
type StorageStats struct {
	SpeedBps float64   `json:"speedBps"`
	MaxFile  int64     `json:"maxFile"`
	Updated  time.Time `json:"updated"`
}

type Paths struct {
	Config  string
	DataDir string
}

func (p Paths) history() string { return filepath.Join(p.DataDir, "history.json") }
func (p Paths) stats() string   { return filepath.Join(p.DataDir, "stats.json") }

type State struct {
	paths Paths
	mu    sync.Mutex
	cfg   Config
	runs  []*model.Run // newest last
	stats map[string]StorageStats
}

const maxRuns = 300

func writeJSON(path string, v any, perm os.FileMode) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func LoadConfig(path string) (Config, error) {
	var c Config
	if err := readJSON(path, &c); err != nil {
		return c, err
	}
	return c, nil
}

func SaveConfig(path string, c Config) error { return writeJSON(path, c, 0o600) }

func OpenState(p Paths) (*State, error) {
	cfg, err := LoadConfig(p.Config)
	if err != nil {
		return nil, fmt.Errorf("чтение конфигурации %s: %w", p.Config, err)
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("в %s не задан token — выполните install", p.Config)
	}
	s := &State{paths: p, cfg: cfg, stats: map[string]StorageStats{}}
	_ = readJSON(p.history(), &s.runs)
	_ = readJSON(p.stats(), &s.stats)
	// Runs interrupted by a restart can never finish.
	for _, r := range s.runs {
		if r.Status == model.RunRunning || r.Status == model.RunQueued {
			r.Status = model.RunFailed
			r.Error = "агент был перезапущен во время выполнения"
			r.Progress = nil
			if r.FinishedAt.IsZero() {
				r.FinishedAt = time.Now()
			}
		}
	}
	return s, nil
}

func (s *State) Config() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(s.cfg)
	var c Config
	_ = json.Unmarshal(b, &c)
	return c
}

func (s *State) update(fn func(c *Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.cfg
	next.Jobs = append([]model.Job(nil), s.cfg.Jobs...)
	next.Storages = append([]model.Storage(nil), s.cfg.Storages...)
	if err := fn(&next); err != nil {
		return err
	}
	if err := SaveConfig(s.paths.Config, next); err != nil {
		return err
	}
	s.cfg = next
	return nil
}

func (s *State) Job(id string) (model.Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.cfg.Jobs {
		if j.ID == id {
			return j, true
		}
	}
	return model.Job{}, false
}

func (s *State) Storage(id string) (model.Storage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, st := range s.cfg.Storages {
		if st.ID == id {
			return st, true
		}
	}
	return model.Storage{}, false
}

func (s *State) AddRun(r *model.Run) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs = append(s.runs, r)
	if len(s.runs) > maxRuns {
		s.runs = s.runs[len(s.runs)-maxRuns:]
	}
}

// WithRun lets the runner mutate a run under the state lock.
func (s *State) WithRun(r *model.Run, fn func(r *model.Run)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(r)
}

func (s *State) SaveHistory() {
	s.mu.Lock()
	b, err := json.Marshal(s.runs)
	s.mu.Unlock()
	if err == nil {
		_ = os.MkdirAll(s.paths.DataDir, 0o700)
		_ = os.WriteFile(s.paths.history(), b, 0o600)
	}
}

func cloneRun(r *model.Run, withLog bool) *model.Run {
	c := *r
	c.Files = append([]model.RunFile(nil), r.Files...)
	if withLog {
		c.Log = append([]model.LogLine(nil), r.Log...)
	} else {
		c.Log = nil
	}
	if r.Progress != nil {
		p := *r.Progress
		c.Progress = &p
	}
	if r.Uploads != nil {
		c.Uploads = map[string]model.UploadStat{}
		for k, v := range r.Uploads {
			c.Uploads[k] = v
		}
	}
	return &c
}

// Runs returns copies, newest first.
func (s *State) Runs(jobID string, limit int, withLog bool) []*model.Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*model.Run
	for i := len(s.runs) - 1; i >= 0; i-- {
		r := s.runs[i]
		if jobID != "" && r.JobID != jobID {
			continue
		}
		out = append(out, cloneRun(r, withLog))
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func (s *State) Run(id string) *model.Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.runs {
		if r.ID == id {
			return cloneRun(r, true)
		}
	}
	return nil
}

func (s *State) rawRun(id string) *model.Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.runs {
		if r.ID == id {
			return r
		}
	}
	return nil
}

// LastFinished returns the most recent finished run of a job.
func (s *State) LastFinished(jobID string) *model.Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.runs) - 1; i >= 0; i-- {
		r := s.runs[i]
		if r.JobID == jobID && !r.FinishedAt.IsZero() {
			return cloneRun(r, false)
		}
	}
	return nil
}

// ExpectedSize is the largest file produced by the last successful run of a job.
func (s *State) ExpectedSize(jobID string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.runs) - 1; i >= 0; i-- {
		r := s.runs[i]
		if r.JobID != jobID || (r.Status != model.RunSuccess && r.Status != model.RunWarning) {
			continue
		}
		var max int64
		for _, f := range r.Files {
			if f.Size > max {
				max = f.Size
			}
		}
		return max
	}
	return 0
}

// LastBytes is the cloud size of the last successful run of a job.
func (s *State) LastBytes(jobID string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.runs) - 1; i >= 0; i-- {
		r := s.runs[i]
		if r.JobID == jobID && (r.Status == model.RunSuccess || r.Status == model.RunWarning) {
			return r.Bytes
		}
	}
	return 0
}

func (s *State) Stats(id string) StorageStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats[id]
}

// RecordSpeed blends a new measurement into the remembered speed.
func (s *State) RecordSpeed(id string, bps float64, maxFile int64) {
	s.mu.Lock()
	st := s.stats[id]
	if bps > 0 {
		if st.SpeedBps <= 0 {
			st.SpeedBps = bps
		} else {
			st.SpeedBps = 0.6*bps + 0.4*st.SpeedBps
		}
	}
	if maxFile > 0 {
		st.MaxFile = maxFile
	}
	st.Updated = time.Now()
	s.stats[id] = st
	b, _ := json.Marshal(s.stats)
	s.mu.Unlock()
	_ = os.MkdirAll(s.paths.DataDir, 0o700)
	_ = os.WriteFile(s.paths.stats(), b, 0o600)
}

func sortedJobs(jobs []model.Job) []model.Job {
	out := append([]model.Job(nil), jobs...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
