// Package model holds the types shared by the desktop panel and the server agent.
package model

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	SourceMySQL    = "mysql"
	SourcePostgres = "postgres"
	SourceFiles    = "files"
	SourceConfigs  = "configs"
)

// Source is one thing to back up inside a job.
type Source struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`

	// Databases (mysql / postgres).
	Host        string   `json:"host,omitempty"`
	Port        int      `json:"port,omitempty"`
	User        string   `json:"user,omitempty"`
	Password    string   `json:"password,omitempty"`
	Databases   []string `json:"databases,omitempty"` // empty = all
	PerDatabase bool     `json:"perDatabase,omitempty"`
	ExtraArgs   string   `json:"extraArgs,omitempty"`

	// Files / configs.
	Paths    []string `json:"paths,omitempty"`
	Excludes []string `json:"excludes,omitempty"`
}

const (
	ScheduleDaily    = "daily"
	ScheduleWeekly   = "weekly"
	ScheduleMonthly  = "monthly"
	ScheduleInterval = "interval"
	ScheduleCron     = "cron"
)

type Schedule struct {
	Enabled    bool   `json:"enabled"`
	Mode       string `json:"mode"`
	Time       string `json:"time"`     // "03:00"
	Weekdays   []int  `json:"weekdays"` // 0 = Sunday
	MonthDay   int    `json:"monthDay"`
	EveryHours int    `json:"everyHours"`
	Cron       string `json:"cron"`
}

func parseHM(s string) (int, int, error) {
	if s == "" {
		return 3, 0, nil
	}
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("неверное время %q", s)
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("неверное время %q", s)
	}
	return h, m, nil
}

// CronExpr converts the schedule into a standard 5-field cron expression.
func (s Schedule) CronExpr() (string, error) {
	h, m, err := parseHM(s.Time)
	if err != nil {
		return "", err
	}
	switch s.Mode {
	case ScheduleDaily, "":
		return fmt.Sprintf("%d %d * * *", m, h), nil
	case ScheduleWeekly:
		if len(s.Weekdays) == 0 {
			return "", fmt.Errorf("не выбраны дни недели")
		}
		days := append([]int(nil), s.Weekdays...)
		sort.Ints(days)
		ds := make([]string, len(days))
		for i, d := range days {
			ds[i] = strconv.Itoa(d)
		}
		return fmt.Sprintf("%d %d * * %s", m, h, strings.Join(ds, ",")), nil
	case ScheduleMonthly:
		d := s.MonthDay
		if d < 1 || d > 28 {
			d = 1
		}
		return fmt.Sprintf("%d %d %d * *", m, h, d), nil
	case ScheduleInterval:
		n := s.EveryHours
		if n < 1 || n > 23 {
			return "", fmt.Errorf("интервал должен быть от 1 до 23 часов")
		}
		return fmt.Sprintf("%d %d-23/%d * * *", m, h%n, n), nil
	case ScheduleCron:
		if strings.TrimSpace(s.Cron) == "" {
			return "", fmt.Errorf("пустое cron-выражение")
		}
		return strings.TrimSpace(s.Cron), nil
	}
	return "", fmt.Errorf("неизвестный режим расписания %q", s.Mode)
}

const (
	ChunkAuto   = "auto"
	ChunkManual = "manual"

	BufferAuto   = "auto"
	BufferMemory = "memory"
	BufferDisk   = "disk"
)

// ChunkSettings controls how the archive stream is split into parts.
type ChunkSettings struct {
	Mode             string `json:"mode"`             // auto | manual
	SizeMB           int    `json:"sizeMB"`           // manual size
	MaxMemoryPercent int    `json:"maxMemoryPercent"` // auto: share of free RAM allowed for buffers
	TargetSeconds    int    `json:"targetSeconds"`    // auto: desired upload time of one part
	Buffer           string `json:"buffer"`           // auto | memory | disk
}

func (c ChunkSettings) Normalized() ChunkSettings {
	if c.Mode != ChunkManual {
		c.Mode = ChunkAuto
	}
	if c.SizeMB <= 0 {
		c.SizeMB = 64
	}
	if c.MaxMemoryPercent <= 0 || c.MaxMemoryPercent > 80 {
		c.MaxMemoryPercent = 20
	}
	if c.TargetSeconds <= 0 {
		c.TargetSeconds = 60
	}
	if c.Buffer != BufferMemory && c.Buffer != BufferDisk {
		c.Buffer = BufferAuto
	}
	return c
}

type Job struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Sources     []Source      `json:"sources"`
	Schedule    Schedule      `json:"schedule"`
	Storages    []string      `json:"storages"`
	Keep        int           `json:"keep"`
	Encrypt     bool          `json:"encrypt"`
	Compression int           `json:"compression"` // gzip level 1..9
	Chunk       ChunkSettings `json:"chunk"`
	Notify      string        `json:"notify"` // errors | always | never
}

const (
	StorageYandex       = "yandex"        // REST API, OAuth token
	StorageYandexWebDAV = "yandex_webdav" // WebDAV, app password
	StorageMailru       = "mailru"        // WebDAV, app password
	StorageWebDAV       = "webdav"        // any WebDAV server
)

type Storage struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Token    string `json:"token,omitempty"`
	Login    string `json:"login,omitempty"`
	Password string `json:"password,omitempty"`
	URL      string `json:"url,omitempty"`
	Root     string `json:"root"`
}

type Telegram struct {
	Enabled  bool   `json:"enabled"`
	BotToken string `json:"botToken"`
	ChatID   string `json:"chatId"`
}

// Settings are global options pushed from the desktop to every agent.
type Settings struct {
	ServerName         string   `json:"serverName"`
	EncryptionPassword string   `json:"encryptionPassword"`
	Telegram           Telegram `json:"telegram"`
}

// SyncPayload is what the desktop sends to keep an agent in line with the panel.
type SyncPayload struct {
	Settings Settings  `json:"settings"`
	Storages []Storage `json:"storages"`
}

const (
	RunQueued   = "queued"
	RunRunning  = "running"
	RunSuccess  = "success"
	RunWarning  = "warning"
	RunFailed   = "failed"
	RunCanceled = "canceled"
)

type LogLine struct {
	Time  time.Time `json:"time"`
	Level string    `json:"level"`
	Msg   string    `json:"msg"`
}

type Part struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type RunFile struct {
	Name      string `json:"name"`
	Source    string `json:"source"`
	Size      int64  `json:"size"`
	RawSize   int64  `json:"rawSize"`
	SHA256    string `json:"sha256"`
	Encrypted bool   `json:"encrypted"`
	Parts     []Part `json:"parts"`
	Error     string `json:"error,omitempty"`
}

type Progress struct {
	Stage    string  `json:"stage"`
	Source   string  `json:"source"`
	RawBytes int64   `json:"rawBytes"`
	Bytes    int64   `json:"bytes"`
	Part     int     `json:"part"`
	SpeedBps float64 `json:"speedBps"`
}

type UploadStat struct {
	Bytes   int64   `json:"bytes"`
	Seconds float64 `json:"seconds"`
}

type Run struct {
	ID          string                `json:"id"`
	JobID       string                `json:"jobId"`
	JobName     string                `json:"jobName"`
	Trigger     string                `json:"trigger"`
	Status      string                `json:"status"`
	QueuedAt    time.Time             `json:"queuedAt"`
	StartedAt   time.Time             `json:"startedAt"`
	FinishedAt  time.Time             `json:"finishedAt"`
	RemoteDir   string                `json:"remoteDir"`
	Storages    []string              `json:"storages"`
	Files       []RunFile             `json:"files"`
	Bytes       int64                 `json:"bytes"`
	RawBytes    int64                 `json:"rawBytes"`
	ChunkSize   int64                 `json:"chunkSize"`
	ChunkBuffer string                `json:"chunkBuffer"`
	Uploads     map[string]UploadStat `json:"uploads,omitempty"`
	Error       string                `json:"error,omitempty"`
	Log         []LogLine             `json:"log,omitempty"`
	Progress    *Progress             `json:"progress,omitempty"`
}

// Manifest is stored next to the parts in every run folder of the cloud.
type Manifest struct {
	Version   int       `json:"version"`
	Server    string    `json:"server"`
	JobID     string    `json:"jobId"`
	JobName   string    `json:"jobName"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"startedAt"`
	Finished  time.Time `json:"finishedAt"`
	Files     []RunFile `json:"files"`
}

type SysInfo struct {
	Hostname     string          `json:"hostname"`
	OS           string          `json:"os"`
	Kernel       string          `json:"kernel"`
	Arch         string          `json:"arch"`
	CPUs         int             `json:"cpus"`
	Load1        float64         `json:"load1"`
	MemTotal     uint64          `json:"memTotal"`
	MemAvailable uint64          `json:"memAvailable"`
	SwapTotal    uint64          `json:"swapTotal"`
	SwapFree     uint64          `json:"swapFree"`
	TmpDir       string          `json:"tmpDir"`
	DiskTotal    uint64          `json:"diskTotal"`
	DiskFree     uint64          `json:"diskFree"`
	Uptime       int64           `json:"uptime"`
	TZ           string          `json:"tz"`
	TZOffset     int             `json:"tzOffset"` // seconds east of UTC
	Tools        map[string]bool `json:"tools"`
}

type AgentInfo struct {
	Version  string    `json:"version"`
	Sys      SysInfo   `json:"sys"`
	Settings Settings  `json:"settings"`
	Storages []Storage `json:"storages"`
	Jobs     []JobView `json:"jobs"`
}

// JobView is a job plus its runtime state.
type JobView struct {
	Job
	NextRun   *time.Time `json:"nextRun,omitempty"`
	LastRun   *Run       `json:"lastRun,omitempty"`
	CronError string     `json:"cronError,omitempty"`
}

// ChunkPlan explains the chunk size picked for a job.
type ChunkPlan struct {
	Size          int64    `json:"size"`
	Buffer        string   `json:"buffer"`
	PeakMemory    int64    `json:"peakMemory"`
	PeakDisk      int64    `json:"peakDisk"`
	MemAvailable  uint64   `json:"memAvailable"`
	DiskFree      uint64   `json:"diskFree"`
	SpeedBps      float64  `json:"speedBps"`
	ExpectedSize  int64    `json:"expectedSize"`
	Parts         int      `json:"parts"`
	PartSeconds   float64  `json:"partSeconds"`
	MaxFileSize   int64    `json:"maxFileSize"`
	Reasons       []string `json:"reasons"`
	Warnings      []string `json:"warnings"`
	SpeedMeasured bool     `json:"speedMeasured"`
}

type StorageTest struct {
	OK        bool    `json:"ok"`
	Error     string  `json:"error,omitempty"`
	SpeedBps  float64 `json:"speedBps"`
	Total     int64   `json:"total"`
	Used      int64   `json:"used"`
	MaxFile   int64   `json:"maxFile"`
	ElapsedMs int64   `json:"elapsedMs"`
}

type BackupEntry struct {
	Dir      string    `json:"dir"`
	Name     string    `json:"name"`
	Status   string    `json:"status"`
	Started  time.Time `json:"started"`
	Size     int64     `json:"size"`
	Files    []RunFile `json:"files"`
	Complete bool      `json:"complete"`
}

type ProbeResult struct {
	OK        bool     `json:"ok"`
	Error     string   `json:"error,omitempty"`
	Databases []string `json:"databases,omitempty"`
	Paths     []string `json:"paths,omitempty"` // existing paths with sizes
}
