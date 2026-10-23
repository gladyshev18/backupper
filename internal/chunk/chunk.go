// Package chunk picks the part size used to stream archives into the cloud.
//
// The archive never touches the server disk (unless disk buffering is chosen):
// it is cut into parts held in memory, and two buffers are in flight at once
// (one is being filled while the other uploads). The size is a balance between
// free RAM on the server, upload speed (a failed part is re-sent whole, so a
// part should upload in about a minute) and the number of files in the cloud.
package chunk

import (
	"fmt"
	"math"

	"backupper/internal/model"
)

const (
	MB = int64(1 << 20)

	MinSize       = 8 * MB
	MaxAutoSize   = 512 * MB
	MaxManualSize = 2048 * MB

	// Buffers in flight: one being filled, one being uploaded.
	Buffers = 2
	// Parts beyond this number make listings slow; grow the part if RAM allows.
	MaxParts = 400
	// RAM always left to the rest of the server.
	reserve = 128 * MB
	// Speed assumed until a real measurement exists.
	defaultSpeed = 2 * MB
)

// Sizes are the "round" part sizes the planner and the UI slider snap to.
var Sizes = []int64{8 * MB, 16 * MB, 24 * MB, 32 * MB, 48 * MB, 64 * MB, 96 * MB, 128 * MB, 192 * MB, 256 * MB, 384 * MB, 512 * MB, 768 * MB, 1024 * MB, 1536 * MB, 2048 * MB}

type Input struct {
	Settings     model.ChunkSettings
	MemAvailable uint64
	DiskFree     uint64
	// SpeedBps is the combined upload speed to all destinations (0 = unknown).
	SpeedBps float64
	// ExpectedSize is the biggest archive of the previous run (0 = unknown).
	ExpectedSize int64
	// MaxFileSize is the smallest per-file limit among destinations (0 = none).
	MaxFileSize int64
	// Lang of the explanations: "ru" (default) or "en".
	Lang string
}

func floorNice(v int64) int64 {
	best := Sizes[0]
	for _, s := range Sizes {
		if s <= v {
			best = s
		}
	}
	return best
}

func ceilNice(v int64) int64 {
	for _, s := range Sizes {
		if s >= v {
			return s
		}
	}
	return Sizes[len(Sizes)-1]
}

func HumanBytes(n int64) string { return humanBytesLang(n, "ru") }

func humanBytesLang(n int64, lang string) string {
	u := [4]string{"ГБ", "МБ", "КБ", "Б"}
	if lang == "en" {
		u = [4]string{"GB", "MB", "KB", "B"}
	}
	f := float64(n)
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f %s", f/(1<<30), u[0])
	case n >= 1<<20:
		return fmt.Sprintf("%.0f %s", f/(1<<20), u[1])
	case n >= 1<<10:
		return fmt.Sprintf("%.0f %s", f/(1<<10), u[2])
	}
	return fmt.Sprintf("%d %s", n, u[3])
}

func humanDur(sec float64) string {
	if sec < 90 {
		return fmt.Sprintf("%.0f с", sec)
	}
	return fmt.Sprintf("%.1f мин", sec/60)
}

// Plan computes the part size and explains the decision in plain words.
func Plan(in Input) model.ChunkPlan {
	s := in.Settings.Normalized()
	tr := func(format string, args ...any) string {
		if in.Lang == "en" {
			if f, ok := enMessages[format]; ok {
				format = f
			}
		}
		return fmt.Sprintf(format, args...)
	}
	hb := func(n int64) string { return humanBytesLang(n, in.Lang) }
	dur := func(sec float64) string {
		if in.Lang == "en" {
			if sec < 90 {
				return fmt.Sprintf("%.0f s", sec)
			}
			return fmt.Sprintf("%.1f min", sec/60)
		}
		return humanDur(sec)
	}
	p := model.ChunkPlan{
		MemAvailable:  in.MemAvailable,
		DiskFree:      in.DiskFree,
		SpeedBps:      in.SpeedBps,
		ExpectedSize:  in.ExpectedSize,
		MaxFileSize:   in.MaxFileSize,
		SpeedMeasured: in.SpeedBps > 0,
		Reasons:       []string{},
		Warnings:      []string{},
	}
	speed := in.SpeedBps
	if speed <= 0 {
		speed = float64(defaultSpeed)
	}

	usableMem := int64(in.MemAvailable) - reserve
	if usableMem < 0 {
		usableMem = 0
	}
	memBudget := usableMem * int64(s.MaxMemoryPercent) / 100
	byMem := memBudget / Buffers
	diskBudget := int64(in.DiskFree) / 2 // never take more than half of the free disk
	byDisk := diskBudget / Buffers

	buffer := s.Buffer
	if buffer == model.BufferAuto {
		buffer = model.BufferMemory
		if byMem < MinSize && byDisk >= 4*MinSize {
			buffer = model.BufferDisk
			p.Reasons = append(p.Reasons, tr("Свободной памяти мало (%s), поэтому куски буферизуются во временных файлах на диске.", hb(int64(in.MemAvailable))))
		}
	}
	limit := byMem
	if buffer == model.BufferDisk {
		limit = byDisk
	}

	var size int64
	if s.Mode == model.ChunkManual {
		size = int64(s.SizeMB) * MB
		if size < MinSize {
			size = MinSize
		}
		if size > MaxManualSize {
			size = MaxManualSize
		}
		p.Reasons = append(p.Reasons, tr("Размер задан вручную: %s.", hb(size)))
		if size > limit {
			if buffer == model.BufferDisk {
				p.Warnings = append(p.Warnings, tr("Два куска по %s займут больше половины свободного места на диске (%s).", hb(size), hb(int64(in.DiskFree))))
			} else {
				p.Warnings = append(p.Warnings, tr("Два куска по %s займут больше %d%% свободной памяти (%s). Сервер может начать свопиться или агент будет убит OOM-killer.", hb(size), s.MaxMemoryPercent, hb(int64(in.MemAvailable))))
			}
		}
	} else {
		if buffer == model.BufferDisk {
			p.Reasons = append(p.Reasons, tr("По диску: до %s на кусок (половина свободного места %s, два куска одновременно).", hb(byDisk), hb(int64(in.DiskFree))))
		} else {
			p.Reasons = append(p.Reasons, tr("По памяти: до %s на кусок (%d%% от %s свободной RAM за вычетом резерва %s, два куска одновременно).", hb(byMem), s.MaxMemoryPercent, hb(int64(in.MemAvailable)), hb(reserve)))
		}
		byNet := int64(speed * float64(s.TargetSeconds))
		if in.SpeedBps > 0 {
			p.Reasons = append(p.Reasons, tr("По сети: ~%s за %d с при измеренной скорости %s/с — при обрыве повторно отправится немного.", hb(byNet), s.TargetSeconds, hb(int64(speed))))
		} else {
			p.Reasons = append(p.Reasons, tr("По сети: скорость ещё не измерена, принято %s/с → ~%s за %d с. Нажмите «Измерить скорость», чтобы уточнить.", hb(int64(speed)), hb(byNet), s.TargetSeconds))
		}
		size = byNet
		if limit < size {
			size = limit
		}
		if in.ExpectedSize > 0 && size > 0 && in.ExpectedSize/size > MaxParts {
			want := ceilNice(in.ExpectedSize / MaxParts)
			if want <= limit {
				size = want
				p.Reasons = append(p.Reasons, tr("Прошлый архив был %s — кусок увеличен до %s, чтобы частей было не больше %d.", hb(in.ExpectedSize), hb(want), MaxParts))
			}
		}
		if size > MaxAutoSize {
			size = MaxAutoSize
		}
		size = floorNice(size)
	}

	if in.MaxFileSize > 0 && size > in.MaxFileSize {
		size = floorNice(in.MaxFileSize)
		p.Reasons = append(p.Reasons, tr("Облако принимает файлы не больше %s — кусок уменьшен до %s.", hb(in.MaxFileSize), hb(size)))
	}
	if size < MinSize {
		size = MinSize
	}
	if buffer == model.BufferMemory && int64(in.MemAvailable) > 0 && Buffers*size+reserve > int64(in.MemAvailable) {
		p.Warnings = append(p.Warnings, tr("Свободной памяти (%s) почти не хватает даже на минимальные куски. Включите буфер на диске или освободите RAM.", hb(int64(in.MemAvailable))))
	}

	p.Size = size
	p.Buffer = buffer
	if buffer == model.BufferDisk {
		p.PeakMemory = 4 * MB
		p.PeakDisk = Buffers * size
	} else {
		p.PeakMemory = Buffers * size
	}
	p.PartSeconds = float64(size) / speed
	if in.ExpectedSize > 0 {
		p.Parts = int(math.Ceil(float64(in.ExpectedSize) / float64(size)))
	}
	if p.PartSeconds > 300 {
		p.Warnings = append(p.Warnings, tr("Один кусок будет загружаться ~%s. При обрыве он отправляется заново целиком — лучше уменьшить размер.", dur(p.PartSeconds)))
	}
	if p.Parts > 1000 {
		p.Warnings = append(p.Warnings, tr("Получится ~%d частей — облако будет медленно показывать папку. Увеличьте кусок, если позволяет память.", p.Parts))
	}
	return p
}

// Shrink returns the next smaller nice size (used when RAM runs low or uploads keep failing).
func Shrink(size int64) int64 {
	next := floorNice(size / 2)
	if next < MinSize {
		return MinSize
	}
	return next
}

// CombinedSpeed: parts go to every destination one after another, so the
// effective speed is the harmonic combination of the individual speeds.
func CombinedSpeed(speeds []float64) float64 {
	var inv float64
	for _, s := range speeds {
		if s <= 0 {
			return 0
		}
		inv += 1 / s
	}
	if inv == 0 {
		return 0
	}
	return 1 / inv
}
