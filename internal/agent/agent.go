// Package agent is the server side: it keeps jobs, runs them on schedule and
// streams dumps to the clouds. The desktop panel manages it over an SSH tunnel.
package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"backupper/internal/model"
)

type Agent struct {
	state   *State
	version string

	mu      sync.Mutex
	cron    *cron.Cron
	entries map[string]cron.EntryID
	queue   []*model.Run
	wake    chan struct{}
	active  *model.Run
	cancel  context.CancelFunc
}

func New(state *State, version string) *Agent {
	a := &Agent{
		state:   state,
		version: version,
		cron:    cron.New(),
		entries: map[string]cron.EntryID{},
		wake:    make(chan struct{}, 1),
	}
	return a
}

func NewID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (a *Agent) Start(ctx context.Context) {
	a.Reschedule()
	a.cron.Start()
	go a.worker(ctx)
}

// Reschedule rebuilds cron entries from the current job list.
func (a *Agent) Reschedule() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, e := range a.entries {
		a.cron.Remove(e)
		delete(a.entries, id)
	}
	for _, j := range a.state.Config().Jobs {
		if !j.Schedule.Enabled {
			continue
		}
		expr, err := j.Schedule.CronExpr()
		if err != nil {
			continue
		}
		jobID := j.ID
		id, err := a.cron.AddFunc(expr, func() { _, _ = a.Enqueue(jobID, "по расписанию") })
		if err == nil {
			a.entries[jobID] = id
		}
	}
}

func (a *Agent) NextRun(jobID string) *time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	if e, ok := a.entries[jobID]; ok {
		t := a.cron.Entry(e).Next
		if !t.IsZero() {
			return &t
		}
	}
	return nil
}

// Enqueue adds a run; one job never has two runs waiting at once.
func (a *Agent) Enqueue(jobID, trigger string) (*model.Run, error) {
	job, ok := a.state.Job(jobID)
	if !ok {
		return nil, fmt.Errorf("задача не найдена")
	}
	a.mu.Lock()
	if a.active != nil && a.active.JobID == jobID {
		a.mu.Unlock()
		return nil, fmt.Errorf("задача уже выполняется")
	}
	for _, r := range a.queue {
		if r.JobID == jobID {
			a.mu.Unlock()
			return nil, fmt.Errorf("задача уже в очереди")
		}
	}
	run := &model.Run{ID: NewID(), JobID: job.ID, JobName: job.Name, Trigger: trigger, Status: model.RunQueued, QueuedAt: time.Now()}
	a.queue = append(a.queue, run)
	a.mu.Unlock()
	a.state.AddRun(run)
	select {
	case a.wake <- struct{}{}:
	default:
	}
	return cloneRun(run, false), nil
}

// Jobs run one after another so that memory use stays predictable.
func (a *Agent) worker(ctx context.Context) {
	for {
		a.mu.Lock()
		var run *model.Run
		if len(a.queue) > 0 {
			run = a.queue[0]
			a.queue = a.queue[1:]
		}
		a.mu.Unlock()
		if run == nil {
			select {
			case <-a.wake:
				continue
			case <-ctx.Done():
				return
			}
		}
		job, ok := a.state.Job(run.JobID)
		if !ok {
			a.state.WithRun(run, func(r *model.Run) {
				r.Status, r.Error, r.FinishedAt = model.RunFailed, "задача удалена", time.Now()
			})
			continue
		}
		rctx, cancel := context.WithCancel(ctx)
		a.mu.Lock()
		a.active, a.cancel = run, cancel
		a.mu.Unlock()
		a.execute(rctx, run, job)
		cancel()
		a.mu.Lock()
		a.active, a.cancel = nil, nil
		a.mu.Unlock()
	}
}

// Cancel stops a running run or removes a queued one.
func (a *Agent) Cancel(runID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active != nil && a.active.ID == runID {
		a.cancel()
		return true
	}
	for i, r := range a.queue {
		if r.ID == runID {
			a.queue = append(a.queue[:i], a.queue[i+1:]...)
			a.state.WithRun(r, func(r *model.Run) {
				r.Status, r.Error, r.FinishedAt = model.RunCanceled, "отменено", time.Now()
			})
			return true
		}
	}
	return false
}

// Active returns the running run and the queue.
func (a *Agent) Active() []*model.Run {
	a.mu.Lock()
	ids := []string{}
	if a.active != nil {
		ids = append(ids, a.active.ID)
	}
	for _, r := range a.queue {
		ids = append(ids, r.ID)
	}
	a.mu.Unlock()
	out := []*model.Run{}
	for _, id := range ids {
		if r := a.state.Run(id); r != nil {
			out = append(out, r)
		}
	}
	return out
}

func (a *Agent) JobViews() []model.JobView {
	cfg := a.state.Config()
	out := []model.JobView{}
	for _, j := range sortedJobs(cfg.Jobs) {
		v := model.JobView{Job: j, NextRun: a.NextRun(j.ID), LastRun: a.state.LastFinished(j.ID), LastSize: a.state.LastBytes(j.ID)}
		if j.Schedule.Enabled {
			expr, err := j.Schedule.CronExpr()
			if err == nil {
				_, err = cron.ParseStandard(expr)
			}
			if err != nil {
				v.CronError = err.Error()
			}
		}
		out = append(out, v)
	}
	return out
}
