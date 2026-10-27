import { Fragment, useEffect, useState } from 'react'
import {
  Archive, ArrowUpCircle, Clock, Cpu, Database, Download, FileArchive, FolderTree, History, MoreHorizontal,
  Pencil, Play, Plus, RefreshCw, Globe, Server as ServerIcon, Settings2, Trash2, Wrench, XCircle, CheckCircle2, Lock, FolderOpen,
} from 'lucide-react'
import { useApp } from '../App'
import { api, errText, onEvent } from '../api'
import type { BackupEntry, JobView, Run, ServerCheck, ServerStatus } from '../types'
import { Alert, Bar, Button, Empty, Menu, Modal, StatusBadge, Toggle, usePoll, useUI } from '../components/ui'
import RunProgress from '../components/RunProgress'
import { bytes, cls, dateTime, duration, kindLabels, policy, relative, retentionText, scheduleText, siteHost, sourceLabels } from '../util'
import { locale, t } from '../i18n'
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
    if (!await confirm(t('Удалить сервер «{name}» из панели? Агент на сервере продолжит делать бэкапы по расписанию — чтобы остановить его, сначала удалите агент.', { name: server.name }), t('Удалить'), true)) return
    await run('del', async () => { await api.deleteServer(id); await reload(); go({ page: 'overview' }) })
  }
  const uninstall = async () => {
    if (!await confirm(t('Удалить агент с сервера «{name}»? Расписание перестанет работать. Настройки задач и история останутся на сервере в /etc/backupper (на случай переустановки). Копии в облаке не удаляются.', { name: server.name }), t('Удалить агент'), true)) return
    await run('un', async () => { await api.uninstallAgent(id, false); await reload(); await refresh() }, t('Агент удалён'))
  }
  const install = () => run('inst', async () => {
    const v = await api.installAgent(id)
    await reload()
    await refresh()
    return v
  }, t('Агент установлен и запущен'))

  const info = st?.info
  // Hosting sites relayed by this server have their own pages (menu "Сайты на хостинге").
  const jobs = info?.jobs.filter(j => j.kind !== 'site') || []
  const sites = info?.jobs.filter(j => j.kind === 'site') || []

  return (
    <div className="page">
      <div className="page-head">
        <div className="job-icon" style={{ width: 48, height: 48, borderRadius: 13 }}><ServerIcon size={22} /></div>
        <div className="grow">
          <h1 className="row">{server.name} {st && <span className={cls('badge', st.online ? 'success' : 'danger')}>{st.online ? t('на связи') : t('нет связи')}</span>}</h1>
          <div className="desc">{server.user}@{server.host}:{server.port}{info && <> · {info.sys.os} · {t('агент {v}', { v: info.version })}</>}</div>
        </div>
        <div className="actions">
          {st?.online && <Button variant="primary" icon={<Plus size={16} />} onClick={() => setJob('new')}>{t('Новая задача')}</Button>}
          <Button icon={<RefreshCw size={15} className={loading ? 'spin' : ''} />} onClick={() => { setLoading(true); refresh() }} />
          <Menu trigger={<Button icon={<MoreHorizontal size={16} />} />}>
            <button onClick={() => setEditing(true)}><Settings2 size={15} /> {t('Настройки подключения')}</button>
            {st?.online && <button onClick={install}><ArrowUpCircle size={15} /> {t('Переустановить агент')}</button>}
            {st?.online && <button className="danger" onClick={uninstall}><XCircle size={15} /> {t('Удалить агент с сервера')}</button>}
            <hr />
            <button className="danger" onClick={removeServer}><Trash2 size={15} /> {t('Удалить сервер из панели')}</button>
          </Menu>
        </div>
      </div>

      {!st && <div className="card card-pad"><Bar value={0} indeterminate /><div className="small muted mt8">{t('Подключаемся по SSH…')}</div></div>}

      {st && !st.online && <InstallPanel id={id} status={st} onInstall={install} installing={busy === 'inst'} onEdit={() => setEditing(true)} onRetry={refresh} />}

      {st?.online && info && (
        <>
          {st.outdated && (
            <div className="mt8" style={{ marginBottom: 16 }}>
              <Alert tone="warning" icon={<ArrowUpCircle size={17} />}>
                <div className="row">
                  <span className="grow">{t('На сервере агент версии {a}, в панели — {p}. Обновите, чтобы получить новые возможности.', { a: info.version, p: state.version })}</span>
                  <Button size="sm" loading={busy === 'inst'} onClick={install}>{t('Обновить агент')}</Button>
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
            <button className={cls(tab === 'jobs' && 'on')} onClick={() => setTab('jobs')}><Archive size={15} /> {t('Задачи')} <span className="faint">{jobs.length}</span></button>
            <button className={cls(tab === 'history' && 'on')} onClick={() => setTab('history')}><History size={15} /> {t('История')}</button>
            <button className={cls(tab === 'backups' && 'on')} onClick={() => setTab('backups')}><FileArchive size={15} /> {t('Копии в облаке')}</button>
            <button className={cls(tab === 'system' && 'on')} onClick={() => setTab('system')}><Cpu size={15} /> {t('Сервер')}</button>
          </div>
          {tab === 'jobs' && <JobsTab id={id} st={st} jobs={jobs} onEdit={setJob} refresh={refresh} />}
          {tab === 'jobs' && sites.length > 0 && (
            <div className="row wrap small muted mt16" style={{ gap: 6 }}>
              <Globe size={14} /> {t('Через этот сервер также сохраняются сайты на хостинге:')}
              {sites.map(j => <button key={j.id} className="chip" style={{ cursor: 'pointer' }} onClick={() => go({ page: 'site', server: id, job: j.id })}>{j.name}</button>)}
            </div>
          )}
          {tab === 'history' && <HistoryTab id={id} jobs={info.jobs} />}
          {tab === 'backups' && <BackupsTab id={id} jobs={info.jobs} />}
          {tab === 'system' && <SystemTab st={st} />}
        </>
      )}

      {editing && <ServerForm server={server} onClose={() => setEditing(false)} onSaved={() => { setEditing(false); refresh() }} />}
      {job && st?.info && <JobEditor serverId={id} job={job === 'new' ? null : job} onClose={() => setJob(null)} onSaved={() => { setJob(null); refresh() }} />}
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
          <div className="card-title" style={{ fontSize: 17 }}>{status.noAgent ? t('Установите агент на сервер') : t('Нет связи с агентом')}</div>
          <div className="muted mt8" style={{ maxWidth: 640 }}>
            {t('Агент — это небольшая служба (~10 МБ), которая сама делает бэкапы по расписанию, даже когда этот компьютер выключен. Панель подключается к нему через SSH-туннель — никаких открытых портов.')}
          </div>
          {!status.noAgent && status.error && <div className="mt12"><Alert tone="danger">{status.error}</Alert></div>}
          <div className="col mt16" style={{ gap: 8 }}>
            <Item ok={checking ? undefined : !!sshOk}>{t('Подключение по SSH')} {chk?.error && !sshOk && <span style={{ color: 'var(--danger)' }}>— {chk.error}</span>}{sshOk && <span className="faint"> — {chk!.os}, {chk!.arch}</span>}</Item>
            {sshOk && <Item ok={!!privOk}>{chk!.root ? t('Вход под root') : chk!.sudo ? t('sudo без пароля доступен') : t('Нужен root или sudo без пароля (NOPASSWD)')}</Item>}
            {sshOk && <Item ok={chk!.systemd}>{chk!.systemd ? t('systemd есть') : t('systemd не найден')}</Item>}
            {sshOk && <Item ok={chk!.agentInstalled}>{chk!.agentInstalled ? t('Агент установлен ({v})', { v: chk!.agentVersion }) : t('Агент ещё не установлен')}</Item>}
          </div>
          <div className="row mt16">
            <Button variant="primary" icon={<Download size={16} />} disabled={!canInstall} loading={installing} onClick={onInstall}>{chk?.agentInstalled ? t('Переустановить агент') : t('Установить агент')}</Button>
            <Button icon={<RefreshCw size={15} />} loading={checking} onClick={check}>{t('Проверить снова')}</Button>
            <Button variant="ghost" icon={<Pencil size={15} />} onClick={onEdit}>{t('Изменить подключение')}</Button>
          </div>
        </div>
      </div>
    </div>
  )
}

const sourceIcon = (type: string) => type === 'mysql' || type === 'postgres' ? <Database size={14} /> : type === 'configs' ? <Settings2 size={14} /> : type === 'site' ? <Globe size={14} /> : <FolderTree size={14} />

function JobsTab({ id, st, jobs, onEdit, refresh }: { id: string; st: ServerStatus; jobs: JobView[]; onEdit: (j: JobView | 'new') => void; refresh: () => Promise<void> }) {
  const { state } = useApp()
  const { run, busy, confirm } = useUI()
  if (!jobs.length) {
    return (
      <div className="card">
        <Empty icon={<Archive size={26} />} title={t('Задач пока нет')} action={<Button variant="primary" icon={<Plus size={16} />} onClick={() => onEdit('new')}>{t('Создать задачу')}</Button>}>
          {t('Задача — это набор источников (базы, файлы сайта, конфиги), расписание и облака, куда выгружать.')}
          {!state.storages.length && <div className="mt8" style={{ color: 'var(--warning)' }}>{t('Сначала подключите облако в разделе «Облака».')}</div>}
        </Empty>
      </div>
    )
  }
  const del = async (j: JobView) => {
    if (!await confirm(t('Удалить задачу «{name}»? Уже сделанные копии в облаке останутся.', { name: j.name }), t('Удалить'), true)) return
    await run('d' + j.id, async () => { await api.deleteJob(id, j.id); await refresh() })
  }
  // Pausing keeps the schedule itself: switching back on resumes it unchanged.
  const setScheduled = (j: JobView, on: boolean) => run('s' + j.id, async () => {
    const { nextRun, lastRun, lastSize, cronError, ...job } = j
    await api.saveJob(id, { ...job, schedule: { ...job.schedule, enabled: on } })
    await refresh()
  }, on ? t('Расписание включено') : t('Расписание выключено: задача запускается только вручную'))
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
                {j.encrypt && <span className="badge" title={t('Архивы шифруются')}><Lock size={11} /> {t('шифр.')}</span>}
                {j.cronError && <span className="badge danger">{j.cronError}</span>}
              </div>
              <div className="row wrap mt8" style={{ gap: 6 }}>
                {j.sources.map(s => <span key={s.id} className="chip">{sourceIcon(s.type)} {t(sourceLabels[s.type])}{s.type === 'files' && s.paths?.[0] ? `: ${s.paths[0]}${s.paths.length > 1 ? ' …' : ''}` : ''}{s.type === 'site' && s.site?.url ? `: ${siteHost(s.site.url)}` : ''}</span>)}
                <span className="chip">→ {j.storages.map(sid => state.storages.find(s => s.id === sid)?.name || '?').join(', ')}</span>
              </div>
              <div className="row small muted mt8" style={{ gap: 16 }}>
                <Toggle small on={j.schedule.enabled} disabled={busy === 's' + j.id} onChange={v => setScheduled(j, v)}
                  title={j.schedule.enabled ? t('Выключить расписание') : t('Включить расписание')}
                  label={<span className="row" style={{ gap: 5, whiteSpace: 'nowrap' }}><Clock size={13} /> {scheduleText(j.schedule)}</span>} />
                {j.nextRun && <span>{t('следующий {when}', { when: relative(j.nextRun) })}</span>}
                <span>{t('хранить: {n}', { n: retentionText(policy(j)) })}</span>
              </div>
            </div>
            <div style={{ textAlign: 'right', minWidth: 150 }}>
              {j.lastRun ? <><StatusBadge status={j.lastRun.status} /><div className="small faint mt8">{dateTime(j.lastRun.startedAt)} · {bytes(j.lastRun.bytes)}</div></> : <span className="small faint">{t('ещё не запускалась')}</span>}
            </div>
            <div className="row" style={{ gap: 6 }}>
              <Button icon={<Play size={14} />} disabled={!!activeRun} loading={busy === 'r' + j.id} onClick={() => run('r' + j.id, async () => { await api.runJob(id, j.id); await refresh() }, t('Задача поставлена в очередь'))}>{activeRun ? t('Идёт') : t('Запустить')}</Button>
              <Button icon={<Pencil size={14} />} onClick={() => onEdit(j)} title={t('Изменить')} />
              <Button icon={<Trash2 size={14} />} variant="danger" loading={busy === 'd' + j.id} onClick={() => del(j)} title={t('Удалить')} />
            </div>
          </div>
        )
      })}
    </div>
  )
}

/** Runs of the server's jobs; `fixed` shows only the first job (a site page). */
export function HistoryTab({ id, jobs, fixed }: { id: string; jobs: JobView[]; fixed?: boolean }) {
  const [jobId, setJobId] = useState(fixed ? jobs[0].id : '')
  const [runs, setRuns] = useState<Run[] | null>(null)
  const [error, setError] = useState('')
  const [open, setOpen] = useState<Run | null>(null)
  usePoll(() => {
    api.runs(id, jobId, 100).then(r => { setRuns(r || []); setError('') }).catch(e => { setError(errText(e)); setRuns(v => v || []) })
  }, 10000, [id, jobId])
  const show = async (r: Run) => setOpen(await api.run(id, r.id))
  return (
    <>
      {!fixed && <div className="row" style={{ marginBottom: 12 }}>
        <select className="select" style={{ width: 260 }} value={jobId} onChange={e => setJobId(e.target.value)}>
          <option value="">{t('Все задачи')}</option>
          {jobs.map(j => <option key={j.id} value={j.id}>{j.name}</option>)}
        </select>
      </div>}
      {error && <Alert tone="danger"><span className="selectable">{error}</span></Alert>}
      <div className="card">
        {!runs ? <div className="card-pad"><Bar value={0} indeterminate /></div> : runs.length === 0 ? <div className="empty small">{error ? t('Не удалось загрузить историю') : t('Запусков пока не было')}</div> : (
          <table className="table">
            <thead><tr><th>{t('Задача')}</th><th>{t('Запуск')}</th><th>{t('Начало')}</th><th>{t('Длительность')}</th><th>{t('Данные → архив')}</th><th>{t('Статус')}</th></tr></thead>
            <tbody>
              {runs.map(r => (
                <tr key={r.id} className="click" onClick={() => show(r)}>
                  <td style={{ fontWeight: 550 }}>{r.jobName}</td>
                  <td className="muted">{t(r.trigger)}</td>
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
        <span className="small muted">{t('Длительность:')} <b>{duration(run.startedAt, run.finishedAt)}</b></span>
        <span className="small muted">{t('Данные:')} <b>{bytes(run.rawBytes)}</b> → {t('архив')} <b>{bytes(run.bytes)}</b></span>
        {run.chunkSize > 0 && <span className="small muted">{t('Кусок:')} <b>{bytes(run.chunkSize)}</b> ({run.chunkBuffer === 'disk' ? t('буфер на диске') : t('буфер в памяти')})</span>}
      </div>
      {run.error && <div className="mt12"><Alert tone="danger"><span className="selectable">{run.error}</span></Alert></div>}
      {run.files?.length > 0 && (
        <div className="card mt16">
          <table className="table">
            <thead><tr><th>{t('Файл')}</th><th>{t('Данные')}</th><th>{t('Архив')}</th><th>{t('Частей')}</th><th /></tr></thead>
            <tbody>
              {run.files.map(f => (
                <tr key={f.name}>
                  <td className="mono">{f.name}</td>
                  <td className="muted">{bytes(f.rawSize)}</td>
                  <td className="muted">{bytes(f.size)}</td>
                  <td className="muted">{f.parts?.length || 0}</td>
                  <td>{f.error ? <span className="badge danger" title={f.error}>{t('ошибка')}</span> : <span className="badge success">ok</span>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {run.remoteDir && <div className="small faint mt12">{t('Папка в облаке:')} <span className="mono selectable">{run.remoteDir}</span> ({run.storages?.map(id => state.storages.find(s => s.id === id)?.name || id).join(', ')})</div>}
      <div className="section-title">{t('Журнал')}</div>
      <div className="log">
        {(run.log || []).map((l, i) => (
          <div key={i} className={l.level}><span className="t">{new Date(l.time).toLocaleTimeString(locale())}</span>{l.msg}</div>
        ))}
      </div>
    </Modal>
  )
}

export function BackupsTab({ id, jobs, fixed }: { id: string; jobs: JobView[]; fixed?: boolean }) {
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
    try { setList(await api.backups(id, jobId, storageId) || []) } catch (e) { setError(String(e)); setList([]) }
  }
  useEffect(() => { load() }, [jobId, storageId])
  useEffect(() => onEvent('download', d => setDl(p => ({ ...p, [d.key]: d }))), [])

  if (!jobs.length) return <div className="card"><div className="empty">{t('Нет задач — нет и копий')}</div></div>

  const download = (b: BackupEntry, f: BackupEntry['files'][0]) => run('dl' + b.dir + f.name, async () => {
    const path = await api.download(id, storageId, b.dir, f.name, decrypt && f.encrypted, f.size)
    if (path) toast('success', t('Сохранено: {path}', { path }))
  })
  const remove = async (b: BackupEntry) => {
    if (!await confirm(t('Удалить копию от {date} из облака? Это необратимо.', { date: dateTime(b.started) }), t('Удалить'), true)) return
    await run('rm' + b.dir, async () => { await api.deleteBackup(id, storageId, b.dir); await load() }, t('Копия удалена'))
  }

  return (
    <>
      <div className="row wrap" style={{ marginBottom: 12 }}>
        {!fixed && <select className="select" style={{ width: 240 }} value={jobId} onChange={e => setJobId(e.target.value)}>
          {jobs.map(j => <option key={j.id} value={j.id}>{j.name}</option>)}
        </select>}
        <select className="select" style={{ width: 240 }} value={storageId} onChange={e => setStorageId(e.target.value)}>
          {job?.storages.map(sid => <option key={sid} value={sid}>{state.storages.find(s => s.id === sid)?.name || sid}</option>)}
        </select>
        <Button icon={<RefreshCw size={15} />} onClick={load} />
        <span className="grow" />
        {job?.encrypt && <label className="check small"><input type="checkbox" checked={decrypt} onChange={e => setDecrypt(e.target.checked)} /> {t('Расшифровывать при скачивании')}</label>}
      </div>
      {error && <Alert tone="danger">{error}</Alert>}
      <div className="card">
        {!list ? <div className="card-pad"><Bar value={0} indeterminate /><div className="small muted mt8">{t('Читаем список копий из облака…')}</div></div>
          : list.length === 0 ? <div className="empty small">{t('В этом облаке копий задачи пока нет')}</div> : (
            <table className="table">
              <thead><tr><th>{t('Дата')}</th><th>{t('Файлов')}</th><th>{t('Размер')}</th><th>{t('Статус')}</th><th /></tr></thead>
              <tbody>
                {list.map(b => (
                  <Fragment key={b.dir}>
                    <tr className="click" onClick={() => setOpen(open === b.dir ? null : b.dir)}>
                      <td style={{ fontWeight: 550 }}>{dateTime(b.started)}<div className="small faint mono">{b.name}</div></td>
                      <td className="muted">{b.files?.length || 0}</td>
                      <td className="muted">{bytes(b.size)}</td>
                      <td><StatusBadge status={b.status} /></td>
                      <td style={{ textAlign: 'right' }} onClick={e => e.stopPropagation()}>
                        <Button size="sm" variant="danger" icon={<Trash2 size={13} />} loading={busy === 'rm' + b.dir} onClick={() => remove(b)} title={t('Удалить копию')} />
                      </td>
                    </tr>
                    {open === b.dir && (b.files || []).map(f => {
                      const p = dl[b.dir + '/' + f.name]
                      const loading = busy === 'dl' + b.dir + f.name
                      return (
                        <tr key={b.dir + f.name}>
                          <td colSpan={2} style={{ paddingLeft: 28 }}><span className="mono">{f.name}</span>{f.error && <span className="badge danger" style={{ marginLeft: 8 }}>{t('ошибка')}</span>}</td>
                          <td className="muted">{bytes(f.size)} <span className="faint">({t('{n} ч.', { n: f.parts?.length || 0 })})</span></td>
                          <td>{loading && p && p.total > 0 && <Bar value={p.done / p.total} />}{loading && p && <div className="small faint">{bytes(p.done)}</div>}</td>
                          <td style={{ textAlign: 'right' }}>
                            {!f.error && <Button size="sm" icon={<Download size={13} />} loading={loading} onClick={() => download(b, f)}>{t('Скачать')}</Button>}
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
      <div className="small faint mt12 row"><FolderOpen size={13} /> {t('Куски склеиваются и (при необходимости) расшифровываются на сервере и передаются на ПК по SSH-туннелю.')}</div>
    </>
  )
}

function SystemTab({ st }: { st: ServerStatus }) {
  const s = st.info!.sys
  const tools = ['mysqldump', 'pg_dump', 'pg_dumpall', 'tar', 'sudo', 'runuser']
  const up = s.uptime > 86400 ? t('{n} дн', { n: Math.floor(s.uptime / 86400) }) : t('{n} ч', { n: Math.floor(s.uptime / 3600) })
  return (
    <div className="grid g2">
      <div className="card card-pad">
        <div className="card-title">{t('Система')}</div>
        <div className="kv mt12">
          <span className="k">{t('Хост')}</span><span>{s.hostname}</span>
          <span className="k">{t('ОС')}</span><span>{s.os}</span>
          <span className="k">{t('Ядро')}</span><span>{s.kernel} ({s.arch})</span>
          <span className="k">CPU</span><span>{t('{n} ядер, загрузка {load}', { n: s.cpus, load: s.load1.toFixed(2) })}</span>
          <span className="k">{t('Аптайм')}</span><span>{up}</span>
          <span className="k">{t('Часовой пояс')}</span><span>{s.tz} (UTC{s.tzOffset >= 0 ? '+' : ''}{s.tzOffset / 3600})</span>
          <span className="k">{t('Агент')}</span><span>{st.info!.version}</span>
        </div>
      </div>
      <div className="card card-pad">
        <div className="card-title">{t('Ресурсы для бэкапа')}</div>
        <div className="col mt12" style={{ gap: 14 }}>
          <div>
            <div className="row small"><span className="grow muted">{t('Память свободно')}</span><b>{bytes(s.memAvailable)}</b><span className="faint">{t('из {total}', { total: bytes(s.memTotal) })}</span></div>
            <div className="mt8"><Bar value={1 - s.memAvailable / s.memTotal} warn={s.memAvailable / s.memTotal < .15} /></div>
          </div>
          {s.swapTotal > 0 && <div>
            <div className="row small"><span className="grow muted">{t('Swap свободно')}</span><b>{bytes(s.swapFree)}</b><span className="faint">{t('из {total}', { total: bytes(s.swapTotal) })}</span></div>
            <div className="mt8"><Bar value={1 - s.swapFree / s.swapTotal} /></div>
          </div>}
          <div>
            <div className="row small"><span className="grow muted">{t('Диск ({dir}) свободно', { dir: s.tmpDir })}</span><b>{bytes(s.diskFree)}</b><span className="faint">{t('из {total}', { total: bytes(s.diskTotal) })}</span></div>
            <div className="mt8"><Bar value={1 - s.diskFree / s.diskTotal} warn={s.diskFree / s.diskTotal < .1} /></div>
          </div>
          <div className="row wrap" style={{ gap: 6 }}>
            {tools.map(tool => <span key={tool} className={cls('badge', s.tools[tool] ? 'success' : '')}>{s.tools[tool] ? '✓' : '—'} {tool}</span>)}
          </div>
          <div className="small faint">{t('Агент работает с пониженным приоритетом (nice 10 и низкий приоритет ввода-вывода), чтобы бэкап не тормозил сайты.')}</div>
        </div>
      </div>
      <div className="card card-pad" style={{ gridColumn: 'span 2' }}>
        <div className="card-title">{t('Облака на агенте')}</div>
        <div className="row wrap mt12" style={{ gap: 8 }}>
          {st.info!.storages.map(c => <span key={c.id} className="chip">{c.name} · {t(kindLabels[c.kind])} · {c.root}</span>)}
          {!st.info!.storages.length && <span className="small faint">{t('Нет — добавьте облако в разделе «Облака», оно синхронизируется автоматически.')}</span>}
        </div>
      </div>
    </div>
  )
}
