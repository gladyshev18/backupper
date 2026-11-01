import { useEffect, useState } from 'react'
import { Clock, FileArchive, Globe, History, MoreHorizontal, Pencil, Play, RefreshCw, Server as ServerIcon, Trash2 } from 'lucide-react'
import { useApp } from '../App'
import { api, errText } from '../api'
import type { JobView, Run } from '../types'
import { Alert, Bar, Button, Empty, Field, Menu, Modal, StatusBadge, usePoll, useUI } from '../components/ui'
import RunProgress from '../components/RunProgress'
import { bytes, cls, dateTime, policy, relative, retentionText, scheduleText } from '../util'
import { t } from '../i18n'
import JobEditor from './JobEditor'
import { BackupsTab, HistoryTab } from './Server'

/** A site on shared hosting: a site job kept on the agent of the server that relays it. */
export default function SitePage({ server, job: jobId }: { server: string; job: string }) {
  const { state, statuses, setStatus, go } = useApp()
  const { run, busy, confirm } = useUI()
  const srv = state.servers.find(s => s.id === server)!
  const st = statuses[server]
  const job = st?.info?.jobs.find(j => j.id === jobId)
  const [tab, setTab] = useState<'history' | 'backups'>('history')
  const [editing, setEditing] = useState(false)
  const [restoring, setRestoring] = useState(false)
  const [loaded, setLoaded] = useState(false)

  const refresh = async () => {
    setStatus(await api.serverStatus(server))
    setLoaded(true)
  }
  const active = st?.active?.filter(r => r.jobId === jobId) || []
  usePoll(() => { refresh() }, active.length ? 2500 : 15000, [server, active.length])

  const remove = async () => {
    if (!job || !await confirm(t('Удалить сайт «{name}»? Бэкапы по расписанию прекратятся, уже сделанные копии в облаке останутся.', { name: job.name }), t('Удалить'), true)) return
    await run('del', async () => { await api.deleteJob(server, job.id); await refresh(); go({ page: 'overview' }) })
  }
  const serverLink = <a href="#" onClick={e => { e.preventDefault(); go({ page: 'server', id: server }) }}>{srv.name}</a>

  if (!job) {
    return (
      <div className="page">
        <div className="card card-pad">
          {!loaded && (!st || st.online) ? <><Bar value={0} indeterminate /><div className="small muted mt8">{t('Подключаемся к серверу {name}…', { name: srv.name })}</div></>
            : st && !st.online ? <Alert tone="danger">{t('Нет связи с сервером, через который сохраняется сайт:')} {serverLink}. {st.error}</Alert>
              : <Empty icon={<Globe size={26} />} title={t('Сайт не найден')}>{t('На сервере {name} такого сайта больше нет — возможно, его удалили или перенесли.', { name: srv.name })}</Empty>}
        </div>
      </div>
    )
  }

  const site = job.sources[0]?.site
  const storages = job.storages.map(id => state.storages.find(s => s.id === id)?.name || '?').join(', ')
  const parts = [site?.db && t('база'), site?.files && t('файлы')].filter(Boolean).join(' + ')

  return (
    <div className="page">
      <div className="page-head">
        <div className="job-icon" style={{ width: 48, height: 48, borderRadius: 13 }}><Globe size={22} /></div>
        <div className="grow">
          <h1 className="row">{job.name} {!st.online && <span className="badge danger">{t('нет связи')}</span>}</h1>
          <div className="desc">
            {site?.url} · <ServerIcon size={12} style={{ verticalAlign: -1 }} /> {t('через сервер')} {serverLink}
          </div>
        </div>
        <div className="actions">
          {st.online && <Button variant="primary" icon={<Play size={15} />} disabled={active.length > 0 || !!job.migrating} loading={busy === 'run'}
            onClick={() => run('run', async () => { await api.runJob(server, job.id); await refresh() }, t('Бэкап сайта поставлен в очередь'))}>{active.length ? t('Идёт') : t('Запустить')}</Button>}
          {st.online && <Button icon={<Pencil size={15} />} onClick={() => setEditing(true)}>{t('Изменить')}</Button>}
          <Button icon={<RefreshCw size={15} />} onClick={refresh} />
          {st.online && <Menu trigger={<Button icon={<MoreHorizontal size={16} />} />}>
            <button disabled={!!job.migrationSource} onClick={() => setRestoring(true)}><History size={15} /> {t('Восстановить данные со старого сервера')}</button>
            <button className="danger" onClick={remove}><Trash2 size={15} /> {t('Удалить сайт')}</button>
          </Menu>}
        </div>
      </div>

      {!st.online && <div style={{ marginBottom: 16 }}><Alert tone="danger">{t('Нет связи с сервером, через который сохраняется сайт:')} {serverLink}. {st.error}</Alert></div>}
      {job.migrating && <div className="mt12"><Alert tone="warning">{t('Сайт приостановлен для переноса.')}
        {job.migrationSource && <div className="mt8"><Button size="sm" loading={busy === 'resume'} onClick={() => run('resume', async () => { await api.moveSite(job.migrationSource!, server, job); await refresh() }, t('Перенос завершён'))}>{t('Продолжить перенос')}</Button></div>}
      </Alert></div>}

      <div className="grid g3">
        <div className="card stat">
          <div className="label"><Clock size={14} /> {t('Расписание')}</div>
          <div className="value" style={{ fontSize: 17 }}>{scheduleText(job.schedule)}</div>
          <div className="foot">{job.nextRun ? t('следующий {when}', { when: relative(job.nextRun) }) : job.cronError || '—'}</div>
        </div>
        <div className="card stat">
          <div className="label"><FileArchive size={14} /> {t('Хранение')}</div>
          <div className="value" style={{ fontSize: 17 }}>{retentionText(policy(job))}</div>
          <div className="foot ellipsis">{storages} · <span className="mono">sites/{job.name}</span></div>
        </div>
        <div className="card stat">
          <div className="label"><History size={14} /> {t('Последний бэкап')}</div>
          <div className="value" style={{ fontSize: 17 }}>{job.lastRun ? <StatusBadge status={job.lastRun.status} /> : <span className="faint">{t('ещё не запускался')}</span>}</div>
          <div className="foot">{job.lastRun ? `${dateTime(job.lastRun.startedAt)} · ${bytes(job.lastRun.bytes)}` : parts}</div>
        </div>
      </div>
      <div className="small faint mt12">{(site?.protocol || 'ftp').toUpperCase()} {site?.host} · {site?.root} · {parts}</div>

      {active.length > 0 && (
        <div className="col mt16">
          {active.map(r => <RunProgress key={r.id} run={r} cancelling={busy === 'c' + r.id} onCancel={() => run('c' + r.id, () => api.cancelRun(server, r.id).then(refresh))} />)}
        </div>
      )}

      {st.online && <>
        <div className="tabs mt16">
          <button className={cls(tab === 'history' && 'on')} onClick={() => setTab('history')}><History size={15} /> {t('История')}</button>
          <button className={cls(tab === 'backups' && 'on')} onClick={() => setTab('backups')}><FileArchive size={15} /> {t('Копии в облаке')}</button>
        </div>
        {tab === 'history' && <HistoryTab id={server} jobs={[job]} fixed />}
        {tab === 'backups' && <BackupsTab id={server} jobs={[job]} fixed />}
      </>}

      {editing && <JobEditor site serverId={server} job={job} onClose={() => setEditing(false)}
        onSaved={(sid, jid) => {
          setEditing(false)
          // After a move the old server must drop the site from the menu too.
          refresh()
          if (sid !== server || jid !== job.id) go({ page: 'site', server: sid, job: jid })
        }} />}
      {restoring && <RestoreSite server={server} job={job} onClose={() => setRestoring(false)} onDone={() => { setRestoring(false); refresh() }} />}
    </div>
  )
}

function RestoreSite({ server, job, onClose, onDone }: { server: string; job: JobView; onClose: () => void; onDone: () => void }) {
  const { state, statuses } = useApp()
  const { run, busy } = useUI()
  const [source, setSource] = useState('')
  const [runs, setRuns] = useState<Run[]>([])
  const [oldId, setOldId] = useState('')
  const [error, setError] = useState('')
  useEffect(() => {
    setRuns([]); setOldId(''); setError('')
    if (!source) return
    let cancelled = false
    api.runs(source, '', 300).then(items => { if (!cancelled) setRuns(items.filter(r => r.jobName === job.name)) }).catch(e => { if (!cancelled) setError(errText(e)) })
    return () => { cancelled = true }
  }, [source, job.name])
  const ids = [...new Set(runs.map(r => r.jobId))]
  return <Modal title={t('Восстановить данные со старого сервера')} onClose={onClose} footer={<><Button onClick={onClose}>{t('Отмена')}</Button><Button variant="primary" disabled={!source || !oldId} loading={busy === 'restore'} onClick={() => run('restore', async () => { await api.restoreSiteData(source, oldId, server, job.id); onDone() }, t('Данные восстановлены'))}>{t('Восстановить')}</Button></>}>
    <Alert>{t('Выберите прежний сервер и задачу. История и индекс Telegram будут объединены с данными текущего сайта.')}</Alert>
    <Field label={t('Старый сервер')}><select className="select" value={source} onChange={e => setSource(e.target.value)}><option value="">—</option>{state.servers.filter(s => s.id !== server).map(s => <option key={s.id} value={s.id} disabled={!statuses[s.id]?.online || statuses[s.id]?.outdated}>{s.name}</option>)}</select></Field>
    <Field label={t('Исходная задача')}><select className="select" value={oldId} onChange={e => setOldId(e.target.value)}><option value="">—</option>{ids.map(id => { const r = runs.find(x => x.jobId === id)!; return <option key={id} value={id}>{r.jobName} · {id} · {dateTime(r.startedAt || r.queuedAt)}</option> })}</select></Field>
    {source && !ids.length && !error && <div className="hint">{t('Подходящих записей истории не найдено.')}</div>}
    {error && <Alert tone="danger">{error}</Alert>}
  </Modal>
}
