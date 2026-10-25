package main

import (
	"testing"

	"backupper/internal/model"
)

func TestRetentionPreview(t *testing.T) {
	a := &App{}
	weekly := model.Schedule{Enabled: true, Mode: model.ScheduleWeekly, Time: "04:00", Weekdays: []int{0}}
	p := a.RetentionPreview(weekly, 3*3600, model.Retention{Weekly: 4, Monthly: 12})
	// 4 weeks + 12 months, the newest copy counts for both.
	if n := len(p.Ages); n != 15 {
		t.Fatalf("kept %d copies: %v", n, p.Ages)
	}
	if old := p.Ages[len(p.Ages)-1]; old < 300 || old > 366 {
		t.Fatalf("oldest copy %.0f days, want about 11 months", old)
	}
	if p.Assumed {
		t.Fatal("schedule is set, nothing to assume")
	}
	p = a.RetentionPreview(model.Schedule{}, 0, model.Retention{Last: 5})
	if len(p.Ages) != 5 || !p.Assumed {
		t.Fatalf("manual job: %+v", p)
	}
}
