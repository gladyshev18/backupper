package chunk

import (
	"testing"

	"backupper/internal/model"
)

func TestPlanAuto(t *testing.T) {
	cases := []struct {
		name       string
		in         Input
		wantSize   int64
		wantBuffer string
		warn       bool
	}{
		{"small VPS, unknown speed", Input{MemAvailable: 900 * uint64(MB)}, 64 * MB, model.BufferMemory, false},
		{"fast link, lots of RAM", Input{MemAvailable: 8 << 30, SpeedBps: 50 * float64(MB)}, 512 * MB, model.BufferMemory, false},
		{"slow link", Input{MemAvailable: 8 << 30, SpeedBps: 300 << 10}, 16 * MB, model.BufferMemory, false},
		{"tiny RAM goes to disk", Input{MemAvailable: 150 * uint64(MB), DiskFree: 5 << 30, SpeedBps: 10 * float64(MB)}, 512 * MB, model.BufferDisk, false},
		{"big archive grows part", Input{MemAvailable: 4 << 30, SpeedBps: 1 * float64(MB), ExpectedSize: 60 << 30}, 192 * MB, model.BufferMemory, false},
		{"mail.ru file limit", Input{MemAvailable: 64 << 30, SpeedBps: 500 * float64(MB), MaxFileSize: 100 * MB}, 96 * MB, model.BufferMemory, false},
	}
	for _, c := range cases {
		p := Plan(c.in)
		if p.Size != c.wantSize || p.Buffer != c.wantBuffer {
			t.Errorf("%s: got %s/%s, want %s/%s; reasons %v", c.name, HumanBytes(p.Size), p.Buffer, HumanBytes(c.wantSize), c.wantBuffer, p.Reasons)
		}
		if (len(p.Warnings) > 0) != c.warn {
			t.Errorf("%s: warnings %v", c.name, p.Warnings)
		}
	}
}

func TestPlanManualWarns(t *testing.T) {
	p := Plan(Input{Settings: model.ChunkSettings{Mode: model.ChunkManual, SizeMB: 512}, MemAvailable: 600 * uint64(MB), SpeedBps: 1 * float64(MB)})
	if p.Size != 512*MB {
		t.Fatalf("manual size ignored: %d", p.Size)
	}
	if len(p.Warnings) < 2 {
		t.Fatalf("expected memory and slow-upload warnings, got %v", p.Warnings)
	}
}

func TestCombinedSpeed(t *testing.T) {
	if s := CombinedSpeed([]float64{10, 10}); s != 5 {
		t.Fatalf("got %v", s)
	}
	if s := CombinedSpeed([]float64{10, 0}); s != 0 {
		t.Fatalf("unknown speed should give 0, got %v", s)
	}
}

func TestPlanEnglish(t *testing.T) {
	inputs := []Input{
		{MemAvailable: 900 << 20},
		{MemAvailable: 150 << 20, DiskFree: 5 << 30, SpeedBps: 10 << 20},
		{MemAvailable: 4 << 30, SpeedBps: 1 << 20, ExpectedSize: 60 << 30},
		{MemAvailable: 64 << 30, SpeedBps: 500 << 20, MaxFileSize: 100 << 20},
		{Settings: model.ChunkSettings{Mode: model.ChunkManual, SizeMB: 2048}, MemAvailable: 300 << 20, SpeedBps: 1 << 20, ExpectedSize: 900 << 30},
		{Settings: model.ChunkSettings{Mode: model.ChunkManual, SizeMB: 1024, Buffer: model.BufferDisk}, DiskFree: 1 << 30},
	}
	for _, in := range inputs {
		in.Lang = "en"
		p := Plan(in)
		for _, m := range append(p.Reasons, p.Warnings...) {
			for _, r := range m {
				if r >= 'А' && r <= 'я' {
					t.Fatalf("untranslated: %s", m)
				}
			}
		}
	}
}
