package model

import (
	"testing"
	"time"
)

func TestRetentionSelect(t *testing.T) {
	// Weekly backups for two years, newest first does not matter.
	base := time.Date(2025, 1, 5, 4, 0, 0, 0, time.UTC) // Sunday
	var times []time.Time
	for i := 0; i < 104; i++ {
		times = append(times, base.AddDate(0, 0, 7*i))
	}
	r := Retention{Weekly: 4, Monthly: 6, Yearly: 2}
	keep := r.Select(times)
	var kept []time.Time
	for i, k := range keep {
		if k != 0 {
			kept = append(kept, times[i])
		}
	}
	// 4 weeks (Dec 6–27), months Dec…Jul are the newest of each month
	// (Dec 27 shared with weeks), years 2026 (shared) and 2025 (Dec 28, 2025).
	if len(kept) != 4+5+1 {
		t.Fatalf("kept %d copies: %v", len(kept), kept)
	}
	newest := times[len(times)-1]
	if keep[len(times)-1] != KeepWeekly|KeepMonthly|KeepYearly {
		t.Fatalf("newest copy %v reasons %b", newest, keep[len(times)-1])
	}
	oldest := kept[0]
	if oldest.Year() != 2025 || oldest.Month() != time.December {
		t.Fatalf("yearly copy of 2025 should be its last one, got %v", oldest)
	}
	if !(Retention{}).Empty() || (Retention{Last: 1}).Calendar() {
		t.Fatal("Empty/Calendar")
	}
}

func TestRetentionLastAndDaily(t *testing.T) {
	// Several runs a day: Last counts runs, Daily counts days.
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	var times []time.Time
	for i := 0; i < 5*4; i++ {
		times = append(times, base.Add(time.Duration(i)*6*time.Hour))
	}
	keep := Retention{Last: 2, Daily: 3}.Select(times)
	n := 0
	for _, k := range keep {
		if k != 0 {
			n++
		}
	}
	// Newest two runs of Mar 5 (one also daily) + last run of Mar 4 and Mar 3.
	if n != 4 {
		t.Fatalf("kept %d, want 4: %v", n, keep)
	}
}
