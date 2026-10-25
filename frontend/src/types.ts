export interface Source {
  id: string
  type: 'mysql' | 'postgres' | 'files' | 'configs' | 'site'
  name: string
  host?: string
  port?: number
  user?: string
  password?: string
  databases?: string[]
  perDatabase?: boolean
  extraArgs?: string
  paths?: string[]
  excludes?: string[]
  cmsExcludes?: boolean
  site?: SiteSource
}

export interface CMSPreset { id: string; name: string; markers: string[]; excludes: string[] }
export interface CMSSite { path: string; cms: string; name: string }

export interface SiteSource {
  protocol: 'ftp' | 'ftps' | 'sftp'
  host: string
  port?: number
  user: string
  password?: string
  root: string
  url: string
  db: boolean
  files: boolean
  cms: 'auto' | 'bitrix' | 'wordpress' | 'none'
  cmsExcludes: boolean
  dbHost?: string
  dbName?: string
  dbUser?: string
  dbPassword?: string
  insecure?: boolean
}

export interface SiteInfo {
  php: string; sapi: string; os: string; root: string; cms: string; timeLimit: number; memoryLimit: number
  diskFree: number; deflate: boolean; pdoMysql: boolean; top: string[]; warnings?: string[]
  db?: { host: string; name: string; user: string; from: string; tables: number; size: number; server: string; error?: string }
}

export interface Schedule {
  enabled: boolean
  mode: 'daily' | 'weekly' | 'monthly' | 'interval' | 'cron'
  time: string
  weekdays: number[]
  monthDay: number
  everyHours: number
  cron: string
}

export interface ChunkSettings {
  mode: 'auto' | 'manual'
  sizeMB: number
  maxMemoryPercent: number
  targetSeconds: number
  buffer: 'auto' | 'memory' | 'disk'
}

/** GFS retention: newest N copies plus the newest copy of each of the last N days/weeks/months/years. */
export interface Retention { last: number; daily: number; weekly: number; monthly: number; yearly: number }

/** Bits of RetentionPreview.reasons, as model.KeepReason. */
export const KEEP_LAST = 1, KEEP_DAILY = 2, KEEP_WEEKLY = 4, KEEP_MONTHLY = 8, KEEP_YEARLY = 16

export interface RetentionPreview { ages: number[] | null; reasons: number[] | null; assumed: boolean }

export interface Job {
  id: string
  name: string
  /** 'site' — a site from the "Sites on hosting" section: one site source, copies in sites/<name>. */
  kind?: 'site'
  sources: Source[]
  schedule: Schedule
  storages: string[]
  keep: number
  retention: Retention
  encrypt: boolean
  compression: number
  chunk: ChunkSettings
  notify: 'errors' | 'always' | 'never'
}

export interface JobView extends Job {
  nextRun?: string
  lastRun?: Run
  lastSize?: number
  cronError?: string
}

export type StorageKind = 'yandex' | 'yandex_webdav' | 'mailru' | 'webdav' | 'telegram'

export interface Storage {
  id: string
  name: string
  kind: StorageKind
  token?: string
  login?: string
  password?: string
  url?: string
  root: string
  /** Telegram: channel id (-100…) or @username, and the own Bot API server. */
  chatId?: string
  tgServer?: TelegramServer
}

/** Own Bot API server for a Telegram storage; admin fields stay in the panel. */
export interface TelegramServer {
  host: string
  port?: number
  user?: string
  hostKey?: string
  apiPort?: number
  adminUser?: string
  adminKeyPath?: string
  adminPassphrase?: string
  apiId?: string
  apiHash?: string
  installed?: string
}

export interface TgServerCheck {
  ok: boolean; error?: string; os: string; arch: string; root: boolean; sudo: boolean; systemd: boolean
  runtime: string; memTotal: number; memAvailable: number; diskFree: number
  telegramOk: boolean; telegramMs: number; speedBps: number; agentVersion: string
  installed: boolean; running: boolean; fingerprint: string; hostKey: string; warnings?: string[]
}

export interface TgInstallResult { ok: boolean; error?: string; log: string; storage: Storage }
export interface TelegramChat { id: number; title: string; type: string }

export interface Telegram {
  enabled: boolean
  botToken: string
  chatId: string
}

export interface Settings {
  encryptionPassword: string
  telegram: Telegram
  theme: 'system' | 'light' | 'dark'
  yandexClientId: string
  language: 'ru' | 'en' | ''
}

export interface Server {
  id: string
  name: string
  host: string
  port: number
  user: string
  keyPath: string
  passphrase?: string
  agentPort: number
  hasToken?: boolean
  fingerprint?: string
}

export interface AppState {
  version: string
  servers: Server[]
  storages: Storage[]
  settings: Settings
  keys: string[]
}

export interface LogLine { time: string; level: 'info' | 'warn' | 'error'; msg: string }
export interface Part { name: string; size: number }
export interface RunFile {
  name: string; source: string; size: number; rawSize: number; sha256: string
  encrypted: boolean; parts: Part[]; error?: string
}
export interface Progress { stage: string; source: string; rawBytes: number; bytes: number; part: number; speedBps: number }

export interface Run {
  id: string; jobId: string; jobName: string; trigger: string
  status: 'queued' | 'running' | 'success' | 'warning' | 'failed' | 'canceled'
  queuedAt: string; startedAt: string; finishedAt: string
  remoteDir: string; storages: string[]; files: RunFile[]
  bytes: number; rawBytes: number; chunkSize: number; chunkBuffer: string
  error?: string; log?: LogLine[]; progress?: Progress
}

export interface SysInfo {
  hostname: string; os: string; kernel: string; arch: string; cpus: number; load1: number
  memTotal: number; memAvailable: number; swapTotal: number; swapFree: number
  tmpDir: string; diskTotal: number; diskFree: number; uptime: number
  tz: string; tzOffset: number; tools: Record<string, boolean>
}

export interface AgentInfo {
  version: string; sys: SysInfo; storages: Storage[]; jobs: JobView[]
  /** Problems the agent found with a storage, e.g. Telegram rotation turned off. */
  storageNotes?: Record<string, string>
}

export interface ServerStatus {
  id: string; online: boolean; error?: string; noAgent: boolean
  info?: AgentInfo; active: Run[]; recent: Run[]; outdated: boolean
}

export interface ServerCheck {
  ok: boolean; error?: string; os: string; arch: string; root: boolean; sudo: boolean
  systemd: boolean; agentInstalled: boolean; agentVersion: string; fingerprint: string
}

export interface ChunkPlan {
  size: number; buffer: 'memory' | 'disk'; peakMemory: number; peakDisk: number
  memAvailable: number; diskFree: number; speedBps: number; expectedSize: number
  parts: number; partSeconds: number; maxFileSize: number
  reasons: string[]; warnings: string[]; speedMeasured: boolean
}

export interface StorageTest {
  ok: boolean; error?: string; speedBps: number; total: number; used: number; maxFile: number; elapsedMs: number
  notes?: string[]
}

export interface BackupEntry {
  dir: string; name: string; status: string; started: string; size: number; files: RunFile[]; complete: boolean
}

export interface ProbeResult { ok: boolean; error?: string; databases?: string[]; paths?: string[]; cms?: CMSSite[]; site?: SiteInfo }
