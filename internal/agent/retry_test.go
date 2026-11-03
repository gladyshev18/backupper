package agent

import (
	"context"
	"testing"
	"time"

	"backupper/internal/model"
)

func TestNextScheduledRetry(t *testing.T) {
	tests := []struct {
		name    string
		run     *model.Run
		attempt int
		delay   time.Duration
		ok      bool
	}{
		{name: "scheduled failure", run: &model.Run{Trigger: scheduledTrigger, Status: model.RunFailed}, attempt: 1, delay: time.Minute, ok: true},
		{name: "first retry failed", run: &model.Run{Trigger: retryTrigger, Retry: 1, Status: model.RunFailed}, attempt: 2, delay: 5 * time.Minute, ok: true},
		{name: "retries exhausted", run: &model.Run{Trigger: retryTrigger, Retry: 2, Status: model.RunFailed}},
		{name: "manual failure", run: &model.Run{Trigger: "вручную", Status: model.RunFailed}},
		{name: "scheduled success", run: &model.Run{Trigger: scheduledTrigger, Status: model.RunSuccess}},
		{name: "retry canceled", run: &model.Run{Trigger: retryTrigger, Retry: 1, Status: model.RunCanceled}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attempt, delay, ok := nextScheduledRetry(tt.run)
			if attempt != tt.attempt || delay != tt.delay || ok != tt.ok {
				t.Fatalf("got (%d, %v, %v), want (%d, %v, %v)", attempt, delay, ok, tt.attempt, tt.delay, tt.ok)
			}
		})
	}
}

func TestScheduleRetryEnqueues(t *testing.T) {
	old := scheduledRetryDelays
	scheduledRetryDelays[0] = 5 * time.Millisecond
	defer func() { scheduledRetryDelays = old }()

	s := &State{
		paths: Paths{DataDir: t.TempDir()},
		cfg:   Config{Jobs: []model.Job{{ID: "job", Name: "Test", Schedule: model.Schedule{Enabled: true}}}},
	}
	a := New(s, "test")
	original := &model.Run{ID: "original", JobID: "job", JobName: "Test", Trigger: scheduledTrigger, Status: model.RunFailed}
	s.AddRun(original)
	a.scheduleRetry(context.Background(), original)

	deadline := time.Now().Add(2 * time.Second)
	for {
		runs := s.Runs("", 0, false)
		if len(runs) == 2 {
			got := runs[0]
			if got.Trigger != retryTrigger || got.Retry != 1 || got.Status != model.RunQueued {
				t.Fatalf("unexpected retry: %+v", got)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("retry was not enqueued")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
