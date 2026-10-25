// Thin typed wrapper over the Go methods bound by Wails.
import * as G from '../wailsjs/go/main/App'
import type {
  AppState, BackupEntry, ChunkPlan, CMSPreset, Job, ProbeResult, Retention, RetentionPreview, Run, Schedule, Server,
  ServerCheck, ServerStatus, Settings, Source, Storage, StorageTest, TelegramChat, TgInstallResult, TgServerCheck,
} from './types'

const g = G as any

export const api = {
  state: (): Promise<AppState> => g.GetState(),
  saveServer: (s: Partial<Server>): Promise<string> => g.SaveServer(s),
  deleteServer: (id: string): Promise<void> => g.DeleteServer(id),
  resetHostKey: (id: string): Promise<void> => g.ResetHostKey(id),
  checkServer: (id: string): Promise<ServerCheck> => g.CheckServer(id),
  installAgent: (id: string): Promise<string> => g.InstallAgent(id),
  uninstallAgent: (id: string, purge: boolean): Promise<void> => g.UninstallAgent(id, purge),
  pickKeyFile: (): Promise<string> => g.PickKeyFile(),

  overview: (): Promise<ServerStatus[]> => g.Overview(),
  serverStatus: (id: string): Promise<ServerStatus> => g.ServerStatus(id),
  active: (id: string): Promise<Run[]> => g.Active(id),
  saveJob: (sid: string, job: Job): Promise<Job> => g.SaveJob(sid, job),
  deleteJob: (sid: string, jid: string): Promise<void> => g.DeleteJob(sid, jid),
  runJob: (sid: string, jid: string): Promise<Run> => g.RunJob(sid, jid),
  cancelRun: (sid: string, rid: string): Promise<void> => g.CancelRun(sid, rid),
  runs: (sid: string, jid: string, limit: number): Promise<Run[]> => g.Runs(sid, jid, limit),
  run: (sid: string, rid: string): Promise<Run> => g.Run(sid, rid),
  plan: (sid: string, job: Job, lang: string): Promise<ChunkPlan> => g.Plan(sid, job, lang),
  probe: (sid: string, src: Source): Promise<ProbeResult> => g.Probe(sid, src),
  speedTest: (sid: string, stid: string): Promise<StorageTest> => g.SpeedTest(sid, stid),
  backups: (sid: string, jid: string, stid: string): Promise<BackupEntry[]> => g.Backups(sid, jid, stid),
  deleteBackup: (sid: string, stid: string, dir: string): Promise<void> => g.DeleteBackup(sid, stid, dir),
  download: (sid: string, stid: string, dir: string, file: string, decrypt: boolean, size: number): Promise<string> =>
    g.DownloadBackup(sid, stid, dir, file, decrypt, size),
  reveal: (path: string): Promise<void> => g.RevealFile(path),

  saveStorage: (s: Storage): Promise<string> => g.SaveStorage(s),
  deleteStorage: (id: string): Promise<void> => g.DeleteStorage(id),
  testStorage: (s: Storage): Promise<StorageTest> => g.TestStorage(s),
  checkTelegramServer: (s: Storage): Promise<TgServerCheck> => g.CheckTelegramServer(s),
  installTelegramServer: (s: Storage): Promise<TgInstallResult> => g.InstallTelegramServer(s),
  telegramChats: (s: Storage): Promise<TelegramChat[]> => g.TelegramChats(s),

  saveSettings: (s: Settings): Promise<Record<string, string>> => g.SaveSettings(s),
  testTelegram: (t: Settings['telegram']): Promise<void> => g.TestTelegram(t),
  openURL: (u: string): Promise<void> => g.OpenURL(u),
  nextRuns: (s: Schedule, tzOffset: number, count: number): Promise<string[]> => g.NextRuns(s, tzOffset, count),
  retentionPreview: (s: Schedule, tzOffset: number, r: Retention): Promise<RetentionPreview> => g.RetentionPreview(s, tzOffset, r),
  cmsPresets: (): Promise<CMSPreset[]> => g.CMSPresets(),
}

let presetsCache: Promise<CMSPreset[]> | null = null

/** CMS presets never change while the panel runs: fetch them once. */
export const cmsPresets = () => (presetsCache ??= api.cmsPresets().catch(() => { presetsCache = null; return [] }))

export function errText(e: unknown): string {
  if (typeof e === 'string') return e
  if (e instanceof Error) return e.message
  return String(e)
}

export function onEvent(name: string, cb: (data: any) => void): () => void {
  const rt = (window as any).runtime
  if (!rt?.EventsOn) return () => {}
  return rt.EventsOn(name, cb)
}
