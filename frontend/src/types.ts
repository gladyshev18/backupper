export interface Source {
  id: string
  type: 'mysql' | 'postgres' | 'files' | 'configs'
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

export interface Job {
  id: string
  name: string
  sources: Source[]
  schedule: Schedule
  storages: string[]
  keep: number
  encrypt: boolean
  compression: number
  chunk: ChunkSettings
  notify: 'errors' | 'always' | 'never'
}

export interface JobView extends Job {
  nextRun?: string
  lastRun?: Run
  cronError?: string
}

export type StorageKind = 'yandex' | 'yandex_webdav' | 'mailru' | 'webdav'

export interface Storage {
  id: string
  name: string
  kind: StorageKind
  token?: string
  login?: string
  password?: string
  url?: string
  root: string
}

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
}

export interface BackupEntry {
  dir: string; name: string; status: string; started: string; size: number; files: RunFile[]; complete: boolean
}

export interface ProbeResult { ok: boolean; error?: string; databases?: string[]; paths?: string[] }
