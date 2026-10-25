import { useState } from 'react'
import { Clock, FileArchive, Globe, History, MoreHorizontal, Pencil, Play, RefreshCw, Server as ServerIcon, Trash2 } from 'lucide-react'
import { useApp } from '../App'
import { api } from '../api'
import { Alert, Bar, Button, Empty, Menu, StatusBadge, usePoll, useUI } from '../components/ui'
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
          {st.online && <Button variant="primary" icon={<Play size={15} />} disabled={active.length > 0} loading={busy === 'run'}
            onClick={() => run('run', async () => { await api.runJob(server, job.id); await refresh() }, t('Бэкап сайта поставлен в очередь'))}>{active.length ? t('Идёт') : t('Запустить')}</Button>}
          {st.online && <Button icon={<Pencil size={15} />} onClick={() => setEditing(true)}>{t('Изменить')}</Button>}
          <Button icon={<RefreshCw size={15} />} onClick={refresh} />
          {st.online && <Menu trigger={<Button icon={<MoreHorizontal size={16} />} />}>
            <button className="danger" onClick={remove}><Trash2 size={15} /> {t('Удалить сайт')}</button>
          </Menu>}
        </div>
      </div>

      {!st.online && <div style={{ marginBottom: 16 }}><Alert tone="danger">{t('Нет связи с сервером, через который сохраняется сайт:')} {serverLink}. {st.error}</Alert></div>}

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
    </div>
  )
}
