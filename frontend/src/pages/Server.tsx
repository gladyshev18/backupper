import { Fragment, useEffect, useState } from 'react'
import {
  Archive, ArrowUpCircle, Clock, Cpu, Database, Download, FileArchive, FolderTree, History, MoreHorizontal,
  Pencil, Play, Plus, RefreshCw, Server as ServerIcon, Settings2, Trash2, Wrench, XCircle, CheckCircle2, Lock, FolderOpen,
} from 'lucide-react'
import { useApp } from '../App'
import { api, onEvent } from '../api'
import type { BackupEntry, JobView, Run, ServerCheck, ServerStatus } from '../types'
import { Alert, Bar, Button, Empty, Menu, Modal, StatusBadge, usePoll, useUI } from '../components/ui'
import RunProgress from '../components/RunProgress'
import { bytes, cls, dateTime, duration, kindLabels, relative, scheduleText, sourceLabels } from '../util'
import ServerForm from './ServerForm'
import JobEditor from './JobEditor'

type Tab = 'jobs' | 'history' | 'backups' | 'system'

export default function ServerPage({ id }: { id: string }) {
  const { state, statuses, setStatus, reload, go } = useApp()
  const { run, busy, confirm } = useUI()
  const server = state.servers.find(s => s.id === id)!
  const st = statuses[id]
  const [tab, setTab] = useState<Tab>('jobs')
  const [editing, setEditing] = useState(false)
  const [job, setJob] = useState<JobView | 'new' | null>(null)
  const [loading, setLoading] = useState(!st)

  const refresh = async () => {
    const s = await api.serverStatus(id)
    setStatus(s)
    setLoading(false)
  }
  const activeCount = st?.active?.length || 0
  usePoll(() => { refresh() }, activeCount ? 2500 : 15000, [id, activeCount])

  const removeServer = async () => {
    if (!await confirm(<>Удалить сервер <b>{server.name}</b> из панели? Агент на сервере продолжит делать бэкапы по расписанию — чтобы остановить его, сначала удалите агент.</>, 'Удалить', true)) return
    await run('del', async () => { await api.deleteServer(id); await reload(); go({ page: 'overview' }) })
  }
  const uninstall = async () => {
    if (!await confirm(<>Удалить агент с сервера <b>{server.name}</b>? Расписание перестанет работать. Настройки задач и история останутся на сервере в /etc/backupper (на случай переустановки). Копии в облаке не удаляются.</>, 'Удалить агент', true)) return
    await run('un', async () => { await api.uninstallAgent(id, false); await reload(); await refresh() }, 'Агент удалён')
  }
  const install = () => run('inst', async () => {
    const v = await api.installAgent(id)
    await reload()
    await refresh()
    return v
  }, 'Агент установлен и запущен')

  const info = st?.info

  return (
    <div className="page">
      <div className="page-head">
        <div className="job-icon" style={{ width: 48, height: 48, borderRadius: 13 }}><ServerIcon size={22} /></div>
        <div className="grow">
          <h1 className="row">{server.name} {st && <span className={cls('badge', st.online ? 'success' : 'danger')}>{st.online ? 'на связи' : 'нет связи'}</span>}</h1>
          <div className="desc">{server.user}@{server.host}:{server.port}{info && <> · {info.sys.os} · агент {info.version}</>}</div>
        </div>
        <div className="actions">
          {st?.online && <Button variant="primary" icon={<Plus size={16} />} onClick={() => setJob('new')}>Новая задача</Button>}
          <Button icon={<RefreshCw size={15} className={loading ? 'spin' : ''} />} onClick={() => { setLoading(true); refresh() }} />
          <Menu trigger={<Button icon={<MoreHorizontal size={16} />} />}>
            <button onClick={() => setEditing(true)}><Settings2 size={15} /> Настройки подключения</button>
            {st?.online && <button onClick={install}><ArrowUpCircle size={15} /> Переустановить агент</button>}
            {st?.online && <button className="danger" onClick={uninstall}><XCircle size={15} /> Удалить агент с сервера</button>}
            <hr />
            <button className="danger" onClick={removeServer}><Trash2 size={15} /> Удалить сервер из панели</button>
          </Menu>
        </div>
      </div>

      {!st && <div className="card card-pad"><Bar value={0} indeterminate /><div className="small muted mt8">Подключаемся по SSH…</div></div>}

      {st && !st.online && <InstallPanel id={id} status={st} onInstall={install} installing={busy === 'inst'} onEdit={() => setEditing(true)} onRetry={refresh} />}

      {st?.online && info && (
        <>
          {st.outdated && (
            <div className="mt8" style={{ marginBottom: 16 }}>
              <Alert tone="warning" icon={<ArrowUpCircle size={17} />}>
                <div className="row">
                  <span className="grow">На сервере агент версии {info.version}, в панели — {state.version}. Обновите, чтобы получить новые возможности.</span>
                  <Button size="sm" loading={busy === 'inst'} onClick={install}>Обновить агент</Button>
                </div>
              </Alert>
            </div>
          )}
          {st.active.length > 0 && (
            <div className="col" style={{ marginBottom: 18 }}>
              {st.active.map(r => <RunProgress key={r.id} run={r} cancelling={busy === 'c' + r.id} onCancel={() => run('c' + r.id, () => api.cancelRun(id, r.id).then(refresh))} />)}
            </div>
          )}
          <div className="tabs">
            <button className={cls(tab === 'jobs' && 'on')} onClick={() => setTab('jobs')}><Archive size={15} /> Задачи <span className="faint">{info.jobs.length}</span></button>
            <button className={cls(tab === 'history' && 'on')} onClick={() => setTab('history')}><History size={15} /> История</button>
            <button className={cls(tab === 'backups' && 'on')} onClick={() => setTab('backups')}><FileArchive size={15} /> Копии в облаке</button>
            <button className={cls(tab === 'system' && 'on')} onClick={() => setTab('system')}><Cpu size={15} /> Сервер</button>
          </div>
          {tab === 'jobs' && <JobsTab id={id} st={st} onEdit={setJob} refresh={refresh} />}
          {tab === 'history' && <HistoryTab id={id} jobs={info.jobs} />}
          {tab === 'backups' && <BackupsTab id={id} jobs={info.jobs} />}
          {tab === 'system' && <SystemTab st={st} />}
        </>
      )}

      {editing && <ServerForm server={server} onClose={() => setEditing(false)} onSaved={() => { setEditing(false); refresh() }} />}
      {job && st?.info && <JobEditor serverId={id} sys={st.info.sys} job={job === 'new' ? null : job} onClose={() => setJob(null)} onSaved={() => { setJob(null); refresh() }} />}
    </div>
  )
}

function InstallPanel({ id, status, onInstall, installing, onEdit, onRetry }: {
  id: string; status: ServerStatus; onInstall: () => void; installing: boolean; onEdit: () => void; onRetry: () => Promise<void>
}) {
  const [chk, setChk] = useState<ServerCheck | null>(null)
  const [checking, setChecking] = useState(false)
  const check = async () => {
    setChecking(true)
    try {
      const c = await api.checkServer(id)
      setChk(c)
      if (c.agentInstalled && !c.error) await onRetry()
    } finally { setChecking(false) }
  }
  useEffect(() => { check() }, [id])

  const sshOk = chk?.ok
  const privOk = chk && (chk.root || chk.sudo)
  const canInstall = sshOk && privOk && chk.systemd
  const Item = ({ ok, children }: { ok?: boolean; children: React.ReactNode }) => (
    <div className="row small">
      {ok === undefined ? <RefreshCw size={15} className="spin faint" /> : ok ? <CheckCircle2 size={15} color="var(--success)" /> : <XCircle size={15} color="var(--danger)" />}
      <span>{children}</span>
    </div>
  )
  return (
    <div className="card card-pad">
      <div className="row" style={{ alignItems: 'flex-start', gap: 18 }}>
        <div className="job-icon" style={{ width: 52, height: 52 }}><Wrench size={24} /></div>
        <div className="grow">
          <div className="card-title" style={{ fontSize: 17 }}>{status.noAgent ? 'Установите агент на сервер' : 'Нет связи с агентом'}</div>
          <div className="muted mt8" style={{ maxWidth: 640 }}>
            Агент — это небольшая служба (~10 МБ), которая сама делает бэкапы по расписанию, даже когда этот компьютер выключен.
            Панель подключается к нему через SSH-туннель — никаких открытых портов.
          </div>
          {!status.noAgent && status.error && <div className="mt12"><Alert tone="danger">{status.error}</Alert></div>}
          <div className="col mt16" style={{ gap: 8 }}>
            <Item ok={checking ? undefined : !!sshOk}>Подключение по SSH {chk?.error && !sshOk && <span style={{ color: 'var(--danger)' }}>— {chk.error}</span>}{sshOk && <span className="faint"> — {chk!.os}, {chk!.arch}</span>}</Item>
            {sshOk && <Item ok={!!privOk}>{chk!.root ? 'Вход под root' : chk!.sudo ? 'sudo без пароля доступен' : 'Нужен root или sudo без пароля (NOPASSWD)'}</Item>}
            {sshOk && <Item ok={chk!.systemd}>systemd {chk!.systemd ? 'есть' : 'не найден'}</Item>}
            {sshOk && <Item ok={chk!.agentInstalled}>{chk!.agentInstalled ? `Агент установлен (${chk!.agentVersion})` : 'Агент ещё не установлен'}</Item>}
          </div>
          <div className="row mt16">
            <Button variant="primary" icon={<Download size={16} />} disabled={!canInstall} loading={installing} onClick={onInstall}>{chk?.agentInstalled ? 'Переустановить агент' : 'Установить агент'}</Button>
            <Button icon={<RefreshCw size={15} />} loading={checking} onClick={check}>Проверить снова</Button>
            <Button variant="ghost" icon={<Pencil size={15} />} onClick={onEdit}>Изменить подключение</Button>
          </div>
        </div>
      </div>
    </div>
  )
}

const sourceIcon = (t: string) => t === 'mysql' || t === 'postgres' ? <Database size={14} /> : t === 'configs' ? <Settings2 size={14} /> : <FolderTree size={14} />

function JobsTab({ id, st, onEdit, refresh }: { id: string; st: ServerStatus; onEdit: (j: JobView | 'new') => void; refresh: () => Promise<void> }) {
  const { state } = useApp()
  const { run, busy, confirm } = useUI()
  const jobs = st.info!.jobs
  if (!jobs.length) {
    return (
      <div className="card">
        <Empty icon={<Archive size={26} />} title="Задач пока нет" action={<Button variant="primary" icon={<Plus size={16} />} onClick={() => onEdit('new')}>Создать задачу</Button>}>
          Задача — это набор источников (базы, файлы сайта, конфиги), расписание и облака, куда выгружать.
          {!state.storages.length && <div className="mt8" style={{ color: 'var(--warning)' }}>Сначала подключите облако в разделе «Облака».</div>}
        </Empty>
      </div>
    )
  }
  const del = async (j: JobView) => {
    if (!await confirm(<>Удалить задачу <b>{j.name}</b>? Уже сделанные копии в облаке останутся.</>, 'Удалить', true)) return
    await run('d' + j.id, async () => { await api.deleteJob(id, j.id); await refresh() })
  }
  return (
    <div className="col">
      {jobs.map(j => {
        const activeRun = st.active.find(r => r.jobId === j.id)
        return (
          <div key={j.id} className="card job-card">
            <div className="job-icon"><Archive size={20} /></div>
            <div className="grow">
              <div className="row" style={{ gap: 8 }}>
                <span style={{ fontWeight: 600, fontSize: 15 }}>{j.name}</span>
                {j.encrypt && <span className="badge" title="Архивы шифруются"><Lock size={11} /> шифр.</span>}
                {j.cronError && <span className="badge danger">{j.cronError}</span>}
              </div>
              <div className="row wrap mt8" style={{ gap: 6 }}>
                {j.sources.map(s => <span key={s.id} className="chip">{sourceIcon(s.type)} {sourceLabels[s.type]}{s.type === 'files' && s.paths?.[0] ? `: ${s.paths[0]}${s.paths.length > 1 ? ' …' : ''}` : ''}</span>)}
                <span className="chip">→ {j.storages.map(sid => state.storages.find(s => s.id === sid)?.name || '?').join(', ')}</span>
              </div>
              <div className="row small muted mt8" style={{ gap: 16 }}>
                <span className="row" style={{ gap: 5 }}><Clock size={13} /> {scheduleText(j.schedule)}</span>
                {j.nextRun && <span>следующий {relative(j.nextRun)}</span>}
                <span>хранить {j.keep || '∞'}</span>
              </div>
            </div>
            <div style={{ textAlign: 'right', minWidth: 150 }}>
              {j.lastRun ? <><StatusBadge status={j.lastRun.status} /><div className="small faint mt8">{dateTime(j.lastRun.startedAt)} · {bytes(j.lastRun.bytes)}</div></> : <span className="small faint">ещё не запускалась</span>}
            </div>
            <div className="row" style={{ gap: 6 }}>
              <Button icon={<Play size={14} />} disabled={!!activeRun} loading={busy === 'r' + j.id} onClick={() => run('r' + j.id, async () => { await api.runJob(id, j.id); await refresh() }, 'Задача поставлена в очередь')}>{activeRun ? 'Идёт' : 'Запустить'}</Button>
              <Button icon={<Pencil size={14} />} onClick={() => onEdit(j)} title="Изменить" />
              <Button icon={<Trash2 size={14} />} variant="danger" loading={busy === 'd' + j.id} onClick={() => del(j)} title="Удалить" />
            </div>
          </div>
        )
      })}
    </div>
  )
}

function HistoryTab({ id, jobs }: { id: string; jobs: JobView[] }) {
  const [jobId, setJobId] = useState('')
  const [runs, setRuns] = useState<Run[] | null>(null)
  const [open, setOpen] = useState<Run | null>(null)
  usePoll(() => { api.runs(id, jobId, 100).then(setRuns).catch(() => {}) }, 10000, [id, jobId])
  const show = async (r: Run) => setOpen(await api.run(id, r.id))
  return (
    <>
      <div className="row" style={{ marginBottom: 12 }}>
        <select className="select" style={{ width: 260 }} value={jobId} onChange={e => setJobId(e.target.value)}>
          <option value="">Все задачи</option>
          {jobs.map(j => <option key={j.id} value={j.id}>{j.name}</option>)}
        </select>
      </div>
      <div className="card">
        {!runs ? <div className="card-pad"><Bar value={0} indeterminate /></div> : runs.length === 0 ? <div className="empty small">Запусков пока не было</div> : (
          <table className="table">
            <thead><tr><th>Задача</th><th>Запуск</th><th>Начало</th><th>Длительность</th><th>Данные → архив</th><th>Статус</th></tr></thead>
            <tbody>
              {runs.map(r => (
                <tr key={r.id} className="click" onClick={() => show(r)}>
                  <td style={{ fontWeight: 550 }}>{r.jobName}</td>
                  <td className="muted">{r.trigger}</td>
                  <td className="muted">{dateTime(r.startedAt || r.queuedAt)}</td>
                  <td className="muted">{duration(r.startedAt, r.finishedAt)}</td>
                  <td className="muted">{bytes(r.rawBytes)} → {bytes(r.bytes)}</td>
                  <td><StatusBadge status={r.status} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
      {open && <RunDetails run={open} onClose={() => setOpen(null)} />}
    </>
  )
}

function RunDetails({ run, onClose }: { run: Run; onClose: () => void }) {
  const { state } = useApp()
  return (
    <Modal wide title={<>{run.jobName} <span className="faint" style={{ fontWeight: 400, fontSize: 14 }}>· {dateTime(run.startedAt)}</span></>} onClose={onClose}>
      <div className="row wrap" style={{ gap: 18 }}>
        <StatusBadge status={run.status} />
        <span className="small muted">Длительность: <b>{duration(run.startedAt, run.finishedAt)}</b></span>
        <span className="small muted">Данные: <b>{bytes(run.rawBytes)}</b> → архив <b>{bytes(run.bytes)}</b></span>
        {run.chunkSize > 0 && <span className="small muted">Кусок: <b>{bytes(run.chunkSize)}</b> ({run.chunkBuffer === 'disk' ? 'буфер на диске' : 'буфер в памяти'})</span>}
      </div>
      {run.error && <div className="mt12"><Alert tone="danger"><span className="selectable">{run.error}</span></Alert></div>}
      {run.files?.length > 0 && (
        <div className="card mt16">
          <table className="table">
            <thead><tr><th>Файл</th><th>Данные</th><th>Архив</th><th>Частей</th><th /></tr></thead>
            <tbody>
              {run.files.map(f => (
                <tr key={f.name}>
                  <td className="mono">{f.name}</td>
                  <td className="muted">{bytes(f.rawSize)}</td>
                  <td className="muted">{bytes(f.size)}</td>
                  <td className="muted">{f.parts?.length || 0}</td>
                  <td>{f.error ? <span className="badge danger" title={f.error}>ошибка</span> : <span className="badge success">ok</span>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {run.remoteDir && <div className="small faint mt12">Папка в облаке: <span className="mono selectable">{run.remoteDir}</span> ({run.storages?.map(id => state.storages.find(s => s.id === id)?.name || id).join(', ')})</div>}
      <div className="section-title">Журнал</div>
      <div className="log">
        {(run.log || []).map((l, i) => (
          <div key={i} className={l.level}><span className="t">{new Date(l.time).toLocaleTimeString('ru-RU')}</span>{l.msg}</div>
        ))}
      </div>
    </Modal>
  )
}

function BackupsTab({ id, jobs }: { id: string; jobs: JobView[] }) {
  const { state } = useApp()
  const { run, busy, confirm, toast } = useUI()
  const [jobId, setJobId] = useState(jobs[0]?.id || '')
  const job = jobs.find(j => j.id === jobId)
  const [storageId, setStorageId] = useState(job?.storages[0] || '')
  const [list, setList] = useState<BackupEntry[] | null>(null)
  const [error, setError] = useState('')
  const [open, setOpen] = useState<string | null>(null)
  const [decrypt, setDecrypt] = useState(true)
  const [dl, setDl] = useState<Record<string, { done: number; total: number }>>({})

  useEffect(() => { if (job && !job.storages.includes(storageId)) setStorageId(job.storages[0] || '') }, [jobId])
  const load = async () => {
    if (!jobId || !storageId) return
    setList(null); setError('')
    try { setList(await api.backups(id, jobId, storageId)) } catch (e) { setError(String(e)); setList([]) }
  }
  useEffect(() => { load() }, [jobId, storageId])
  useEffect(() => onEvent('download', d => setDl(p => ({ ...p, [d.key]: d }))), [])

  if (!jobs.length) return <div className="card"><div className="empty">Нет задач — нет и копий</div></div>

  const download = (b: BackupEntry, f: BackupEntry['files'][0]) => run('dl' + b.dir + f.name, async () => {
    const path = await api.download(id, storageId, b.dir, f.name, decrypt && f.encrypted, f.size)
    if (path) toast('success', `Сохранено: ${path}`)
  })
  const remove = async (b: BackupEntry) => {
    if (!await confirm(<>Удалить копию от <b>{dateTime(b.started)}</b> из облака? Это необратимо.</>, 'Удалить', true)) return
    await run('rm' + b.dir, async () => { await api.deleteBackup(id, storageId, b.dir); await load() }, 'Копия удалена')
  }

  return (
    <>
      <div className="row wrap" style={{ marginBottom: 12 }}>
        <select className="select" style={{ width: 240 }} value={jobId} onChange={e => setJobId(e.target.value)}>
          {jobs.map(j => <option key={j.id} value={j.id}>{j.name}</option>)}
        </select>
        <select className="select" style={{ width: 240 }} value={storageId} onChange={e => setStorageId(e.target.value)}>
          {job?.storages.map(sid => <option key={sid} value={sid}>{state.storages.find(s => s.id === sid)?.name || sid}</option>)}
        </select>
        <Button icon={<RefreshCw size={15} />} onClick={load} />
        <span className="grow" />
        {job?.encrypt && <label className="check small"><input type="checkbox" checked={decrypt} onChange={e => setDecrypt(e.target.checked)} /> Расшифровывать при скачивании</label>}
      </div>
      {error && <Alert tone="danger">{error}</Alert>}
      <div className="card">
        {!list ? <div className="card-pad"><Bar value={0} indeterminate /><div className="small muted mt8">Читаем список копий из облака…</div></div>
          : list.length === 0 ? <div className="empty small">В этом облаке копий задачи пока нет</div> : (
            <table className="table">
              <thead><tr><th>Дата</th><th>Файлов</th><th>Размер</th><th>Статус</th><th /></tr></thead>
              <tbody>
                {list.map(b => (
                  <Fragment key={b.dir}>
                    <tr className="click" onClick={() => setOpen(open === b.dir ? null : b.dir)}>
                      <td style={{ fontWeight: 550 }}>{dateTime(b.started)}<div className="small faint mono">{b.name}</div></td>
                      <td className="muted">{b.files?.length || 0}</td>
                      <td className="muted">{bytes(b.size)}</td>
                      <td><StatusBadge status={b.status} /></td>
                      <td style={{ textAlign: 'right' }} onClick={e => e.stopPropagation()}>
                        <Button size="sm" variant="danger" icon={<Trash2 size={13} />} loading={busy === 'rm' + b.dir} onClick={() => remove(b)} title="Удалить копию" />
                      </td>
                    </tr>
                    {open === b.dir && (b.files || []).map(f => {
                      const p = dl[b.dir + '/' + f.name]
                      const loading = busy === 'dl' + b.dir + f.name
                      return (
                        <tr key={b.dir + f.name}>
                          <td colSpan={2} style={{ paddingLeft: 28 }}><span className="mono">{f.name}</span>{f.error && <span className="badge danger" style={{ marginLeft: 8 }}>ошибка</span>}</td>
                          <td className="muted">{bytes(f.size)} <span className="faint">({f.parts?.length} ч.)</span></td>
                          <td>{loading && p && p.total > 0 && <Bar value={p.done / p.total} />}{loading && p && <div className="small faint">{bytes(p.done)}</div>}</td>
                          <td style={{ textAlign: 'right' }}>
                            {!f.error && <Button size="sm" icon={<Download size={13} />} loading={loading} onClick={() => download(b, f)}>Скачать</Button>}
                          </td>
                        </tr>
                      )
                    })}
                  </Fragment>
                ))}
              </tbody>
            </table>
          )}
      </div>
      <div className="small faint mt12 row"><FolderOpen size={13} /> Куски склеиваются и (при необходимости) расшифровываются на сервере и передаются на ПК по SSH-туннелю.</div>
    </>
  )
}

function SystemTab({ st }: { st: ServerStatus }) {
  const s = st.info!.sys
  const tools = ['mysqldump', 'pg_dump', 'pg_dumpall', 'tar', 'sudo', 'runuser']
  const up = s.uptime > 86400 ? `${Math.floor(s.uptime / 86400)} дн` : `${Math.floor(s.uptime / 3600)} ч`
  return (
    <div className="grid g2">
      <div className="card card-pad">
        <div className="card-title">Система</div>
        <div className="kv mt12">
          <span className="k">Хост</span><span>{s.hostname}</span>
          <span className="k">ОС</span><span>{s.os}</span>
          <span className="k">Ядро</span><span>{s.kernel} ({s.arch})</span>
          <span className="k">CPU</span><span>{s.cpus} ядер, загрузка {s.load1.toFixed(2)}</span>
          <span className="k">Аптайм</span><span>{up}</span>
          <span className="k">Часовой пояс</span><span>{s.tz} (UTC{s.tzOffset >= 0 ? '+' : ''}{s.tzOffset / 3600})</span>
          <span className="k">Агент</span><span>{st.info!.version}</span>
        </div>
      </div>
      <div className="card card-pad">
        <div className="card-title">Ресурсы для бэкапа</div>
        <div className="col mt12" style={{ gap: 14 }}>
          <div>
            <div className="row small"><span className="grow muted">Память свободно</span><b>{bytes(s.memAvailable)}</b><span className="faint">из {bytes(s.memTotal)}</span></div>
            <div className="mt8"><Bar value={1 - s.memAvailable / s.memTotal} warn={s.memAvailable / s.memTotal < .15} /></div>
          </div>
          {s.swapTotal > 0 && <div>
            <div className="row small"><span className="grow muted">Swap свободно</span><b>{bytes(s.swapFree)}</b><span className="faint">из {bytes(s.swapTotal)}</span></div>
            <div className="mt8"><Bar value={1 - s.swapFree / s.swapTotal} /></div>
          </div>}
          <div>
            <div className="row small"><span className="grow muted">Диск ({s.tmpDir}) свободно</span><b>{bytes(s.diskFree)}</b><span className="faint">из {bytes(s.diskTotal)}</span></div>
            <div className="mt8"><Bar value={1 - s.diskFree / s.diskTotal} warn={s.diskFree / s.diskTotal < .1} /></div>
          </div>
          <div className="row wrap" style={{ gap: 6 }}>
            {tools.map(t => <span key={t} className={cls('badge', s.tools[t] ? 'success' : '')}>{s.tools[t] ? '✓' : '—'} {t}</span>)}
          </div>
          <div className="small faint">Агент работает с пониженным приоритетом (nice 10 и низкий приоритет ввода-вывода), чтобы бэкап не тормозил сайты.</div>
        </div>
      </div>
      <div className="card card-pad" style={{ gridColumn: 'span 2' }}>
        <div className="card-title">Облака на агенте</div>
        <div className="row wrap mt12" style={{ gap: 8 }}>
          {st.info!.storages.map(c => <span key={c.id} className="chip">{c.name} · {kindLabels[c.kind]} · {c.root}</span>)}
          {!st.info!.storages.length && <span className="small faint">Нет — добавьте облако в разделе «Облака», оно синхронизируется автоматически.</span>}
        </div>
      </div>
    </div>
  )
}
