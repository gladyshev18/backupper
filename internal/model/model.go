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
	SourceSite     = "site" // site on shared hosting: PHP script over FTP/SFTP
)

const (
	ProtoFTP  = "ftp"
	ProtoFTPS = "ftps" // explicit TLS; port 990 means implicit TLS
	ProtoSFTP = "sftp"
)

// SiteSource describes a site on shared hosting backed up through a temporary PHP script.
type SiteSource struct {
	Protocol    string `json:"protocol"`
	Host        string `json:"host"`
	Port        int    `json:"port,omitempty"`
	User        string `json:"user"`
	Password    string `json:"password,omitempty"`
	Root        string `json:"root"` // site root as seen over FTP, e.g. /public_html
	URL         string `json:"url"`  // the same root over HTTP, e.g. https://site.ru
	DB          bool   `json:"db"`
	Files       bool   `json:"files"`
	CMS         string `json:"cms"` // auto | none | a CMS id of script.php (bitrix, wordpress, joomla, …)
	CMSExcludes bool   `json:"cmsExcludes"`
	DBHost      string `json:"dbHost,omitempty"` // manual database settings: used when the CMS files have none or they fail
	DBName      string `json:"dbName,omitempty"`
	DBUser      string `json:"dbUser,omitempty"`
	DBPassword  string `json:"dbPassword,omitempty"`
	Insecure    bool   `json:"insecure,omitempty"` // do not verify TLS certificates (FTPS and HTTPS)
}

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
	// CMSExcludes finds sites of known CMS inside Paths and skips their caches and own backups.
	CMSExcludes bool `json:"cmsExcludes,omitempty"`

	Site *SiteSource `json:"site,omitempty"`
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

// Retention decides which complete backups survive rotation (the GFS scheme):
// Last keeps the newest N copies, Daily/Weekly/Monthly/Yearly keep the newest
// copy of each of the last N days / ISO weeks / months / years that have one.
// A copy kept by several rules is stored once.
type Retention struct {
	Last    int `json:"last"`
	Daily   int `json:"daily"`
	Weekly  int `json:"weekly"`
	Monthly int `json:"monthly"`
	Yearly  int `json:"yearly"`
}

// KeepReason is a bit set of the rules that keep a copy; 0 means delete.
type KeepReason uint8

const (
	KeepLast KeepReason = 1 << iota
	KeepDaily
	KeepWeekly
	KeepMonthly
	KeepYearly
)

// Calendar reports whether any period rule is set (otherwise only Last matters).
func (r Retention) Calendar() bool {
	return r.Daily > 0 || r.Weekly > 0 || r.Monthly > 0 || r.Yearly > 0
}

func (r Retention) Empty() bool { return r.Last <= 0 && !r.Calendar() }

func (r Retention) Normalized() Retention {
	for _, v := range []*int{&r.Last, &r.Daily, &r.Weekly, &r.Monthly, &r.Yearly} {
		*v = max(0, min(*v, 1000))
	}
	return r
}

// Select marks which of the backup times to keep. Times are wall-clock times of
// the run folders; their order does not matter.
func (r Retention) Select(times []time.Time) []KeepReason {
	out := make([]KeepReason, len(times))
	order := make([]int, len(times))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return times[order[a]].After(times[order[b]]) })
	for n, i := range order {
		if n < r.Last {
			out[i] |= KeepLast
		}
	}
	bucket := func(limit int, flag KeepReason, key func(time.Time) int) {
		seen := map[int]bool{}
		for _, i := range order {
			if len(seen) >= limit {
				return
			}
			if k := key(times[i]); !seen[k] {
				seen[k] = true
				out[i] |= flag
			}
		}
	}
	bucket(r.Daily, KeepDaily, func(t time.Time) int { return t.Year()*1000 + t.YearDay() })
	bucket(r.Weekly, KeepWeekly, func(t time.Time) int { y, w := t.ISOWeek(); return y*100 + w })
	bucket(r.Monthly, KeepMonthly, func(t time.Time) int { return t.Year()*100 + int(t.Month()) })
	bucket(r.Yearly, KeepYearly, func(t time.Time) int { return t.Year() })
	return out
}

// JobSite marks a job made in the panel's "Sites on hosting" section: one site
// source, its copies live in SitesFolder/<site> rather than under the server.
const JobSite = "site"

// SitesFolder is the cloud folder (below the storage root) of hosting-site jobs,
// shared by all agents, so a site keeps its copies when moved to another server.
const SitesFolder = "sites"

type Job struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Kind        string        `json:"kind,omitempty"` // "" (server job) | JobSite
	Sources     []Source      `json:"sources"`
	Schedule    Schedule      `json:"schedule"`
	Storages    []string      `json:"storages"`
	Keep        int           `json:"keep"` // simple "keep last N"; used when Retention is empty (jobs from 1.1 and older)
	Retention   Retention     `json:"retention"`
	Encrypt     bool          `json:"encrypt"`
	Compression int           `json:"compression"` // gzip level 1..9
	Chunk       ChunkSettings `json:"chunk"`
	Notify      string        `json:"notify"` // errors | always | never
}

// Policy is the effective retention: the calendar scheme or the plain Keep count.
func (j Job) Policy() Retention {
	if j.Retention.Empty() {
		return Retention{Last: j.Keep}
	}
	return j.Retention
}

const (
	StorageYandex       = "yandex"        // REST API, OAuth token
	StorageYandexWebDAV = "yandex_webdav" // WebDAV, app password
	StorageMailru       = "mailru"        // WebDAV, app password
	StorageWebDAV       = "webdav"        // any WebDAV server
	StorageTelegram     = "telegram"      // Telegram chat through an own Bot API server (telegram-bot-api --local)
)

type Storage struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Token    string `json:"token,omitempty"` // OAuth token; bot token for Telegram
	Login    string `json:"login,omitempty"`
	Password string `json:"password,omitempty"`
	URL      string `json:"url,omitempty"`
	Root     string `json:"root"`

	// Telegram: the chat that keeps the files (-100… id or @username) and the Bot API server.
	ChatID   string          `json:"chatId,omitempty"`
	TgServer *TelegramServer `json:"tgServer,omitempty"`
}

// TelegramRelayUser is the restricted account on the Bot API server: agents log in
// with the panel-made key and may only forward to the Bot API port and use SFTP.
const TelegramRelayUser = "backupper-tg"

// TelegramServer is the own Bot API server a Telegram storage goes through. It
// listens on the server's loopback only; agents reach it over SSH.
type TelegramServer struct {
	Host    string `json:"host"`
	Port    int    `json:"port,omitempty"`    // SSH port, 22 by default
	User    string `json:"user,omitempty"`    // relay user, TelegramRelayUser by default
	Key     string `json:"key,omitempty"`     // relay user's private key (OpenSSH PEM), made by the panel
	HostKey string `json:"hostKey,omitempty"` // pinned SSH host key, base64
	APIPort int    `json:"apiPort,omitempty"` // Bot API port on the server's loopback, 8081 by default
	// DirectURL skips SSH and reads downloaded files from the local disk: for tests
	// and for a Bot API server running on the same machine.
	DirectURL string `json:"directUrl,omitempty"`

	// Panel only, cleared before the storage is sent to agents.
	AdminUser       string `json:"adminUser,omitempty"`
	AdminKeyPath    string `json:"adminKeyPath,omitempty"`
	AdminPassphrase string `json:"adminPassphrase,omitempty"`
	APIID           string `json:"apiId,omitempty"`
	APIHash         string `json:"apiHash,omitempty"`
	Installed       string `json:"installed,omitempty"` // when the panel installed the Bot API there
}

// ForAgent returns a copy without the panel-only fields.
func (s Storage) ForAgent() Storage {
	if s.TgServer != nil {
		ts := *s.TgServer
		ts.AdminUser, ts.AdminKeyPath, ts.AdminPassphrase, ts.APIID, ts.APIHash = "", "", "", "", ""
		s.TgServer = &ts
	}
	return s
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
	// EstRaw is the expected uncompressed size of the current file: counted by the
	// source (files on a hosting) or taken from the last successful run; 0 = unknown.
	EstRaw int64     `json:"estRaw,omitempty"`
	Since  time.Time `json:"since"` // when the current file started
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
	EstRawBytes int64                 `json:"estRawBytes,omitempty"` // expected uncompressed total while running; 0 = unknown
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
	// Telegram: where each part lies in the chat, so a copy can be restored from the manifest alone.
	Telegram *TelegramIndex `json:"telegram,omitempty"`
}

type TelegramIndex struct {
	Chat     string                 `json:"chat"`
	Messages map[string]TelegramRef `json:"messages"` // part name → message
}

type TelegramRef struct {
	MessageID int    `json:"messageId"`
	FileID    string `json:"fileId"`
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
	// StorageNotes are problems an agent found with a storage, e.g. Telegram
	// refusing to delete old messages (rotation is then off there).
	StorageNotes map[string]string `json:"storageNotes,omitempty"`
}

// JobView is a job plus its runtime state.
type JobView struct {
	Job
	NextRun   *time.Time `json:"nextRun,omitempty"`
	LastRun   *Run       `json:"lastRun,omitempty"`
	LastSize  int64      `json:"lastSize,omitempty"` // bytes of the last complete backup
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
	OK        bool     `json:"ok"`
	Error     string   `json:"error,omitempty"`
	SpeedBps  float64  `json:"speedBps"`
	Total     int64    `json:"total"`
	Used      int64    `json:"used"`
	MaxFile   int64    `json:"maxFile"`
	ElapsedMs int64    `json:"elapsedMs"`
	Notes     []string `json:"notes,omitempty"` // what works only partly, e.g. missing bot rights
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
	OK        bool      `json:"ok"`
	Error     string    `json:"error,omitempty"`
	Databases []string  `json:"databases,omitempty"`
	Paths     []string  `json:"paths,omitempty"` // existing paths with sizes
	CMS       []CMSSite `json:"cms,omitempty"`   // sites of known CMS found inside the paths
	Site      *SiteInfo `json:"site,omitempty"`
}

// CMSSite is a site root recognised by its CMS files.
type CMSSite struct {
	Path string `json:"path"`
	CMS  string `json:"cms"`  // preset id, e.g. "bitrix"
	Name string `json:"name"` // human name, e.g. "1С-Битрикс"
}

// SiteInfo is what the PHP script reports about the hosting.
type SiteInfo struct {
	PHP         string   `json:"php"`
	SAPI        string   `json:"sapi"`
	OS          string   `json:"os"`
	Root        string   `json:"root"`
	CMS         string   `json:"cms"`
	TimeLimit   int      `json:"timeLimit"`
	MemoryLimit int64    `json:"memoryLimit"`
	DiskFree    float64  `json:"diskFree"`
	Deflate     bool     `json:"deflate"`
	PDOMySQL    bool     `json:"pdoMysql"`
	ServerTime  int64    `json:"serverTime"`
	Top         []string `json:"top"`
	DB          *SiteDB  `json:"db,omitempty"`
	DBError     string   `json:"dbError,omitempty"` // no usable database settings at all
	Warnings    []string `json:"warnings,omitempty"`
}

type SiteDB struct {
	Host   string  `json:"host"`
	Name   string  `json:"name"`
	User   string  `json:"user"`
	From   string  `json:"from"`
	Tables int     `json:"tables"`
	Size   float64 `json:"size"`
	Server string  `json:"server"`
	Error  string  `json:"error,omitempty"`
	Note   string  `json:"note,omitempty"` // why the CMS settings were not used (the panel's were)
}
