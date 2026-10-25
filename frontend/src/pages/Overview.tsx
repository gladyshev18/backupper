import { useState } from 'react'
import { AlertOctagon, CheckCircle2, Cloud, HardDrive, ListChecks, RefreshCw, Server as ServerIcon, Wifi } from 'lucide-react'
import { useApp } from '../App'
import { api } from '../api'
import type { Run } from '../types'
import { Bar, Button, Empty, StatusBadge, usePoll, useUI } from '../components/ui'
import RunProgress from '../components/RunProgress'
import { bytes, dateTime, duration, relative } from '../util'
import { t } from '../i18n'

export default function Overview() {
  const { state, statuses, refreshAll, go, addServer } = useApp()
  const { run, busy } = useUI()
  const [first, setFirst] = useState(true)
  const anyActive = Object.values(statuses).some(s => s.active?.length)
  usePoll(() => { refreshAll().finally(() => setFirst(false)) }, anyActive ? 3000 : 15000, [anyActive])

  if (!state.servers.length) {
    return (
      <div className="page">
        <div className="card" style={{ marginTop: 40 }}>
          <Empty icon={<ShieldIcon />} title={t('Добро пожаловать в Backupper')} action={<div className="row" style={{ justifyContent: 'center' }}>
            {!state.storages.length && <Button icon={<Cloud size={16} />} onClick={() => go({ page: 'clouds' })}>{t('Подключить облако')}</Button>}
            <Button variant="primary" icon={<ServerIcon size={16} />} onClick={addServer}>{t('Добавить сервер')}</Button>
          </div>}>
            <div style={{ maxWidth: 520, margin: '8px auto 0', textAlign: 'left' }}>
              <p className="muted">{t('Бэкапы выполняет агент на самом сервере — по расписанию и без участия этого компьютера. Архив сразу уходит в облако кусками, не занимая место на диске сервера.')}</p>
              <ol className="steps mt16">
                <li>{t('Подключите Яндекс Диск или Облако Mail.ru')}</li>
                <li>{t('Добавьте сервер — доступ по вашему SSH-ключу')}</li>
                <li>{t('Установите агент одной кнопкой')}</li>
                <li>{t('Создайте задачу: базы, файлы сайта, конфиги и расписание')}</li>
              </ol>
            </div>
          </Empty>
        </div>
      </div>
    )
  }

  const list = state.servers.map(s => ({ s, st: statuses[s.id] }))
  const online = list.filter(x => x.st?.online).length
  const jobs = list.reduce((n, x) => n + (x.st?.info?.jobs.length || 0), 0)
  const recent: (Run & { server: string; sid: string })[] = list
    .flatMap(x => (x.st?.recent || []).map(r => ({ ...r, server: x.s.name, sid: x.s.id })))
    .filter(r => r.status !== 'queued' && r.status !== 'running')
    .sort((a, b) => (b.startedAt || '').localeCompare(a.startedAt || ''))
  const week = Date.now() - 7 * 86400e3
  const lastWeek = recent.filter(r => new Date(r.startedAt).getTime() > week)
  const ok = lastWeek.filter(r => r.status === 'success' || r.status === 'warning').length
  const failed = lastWeek.filter(r => r.status === 'failed').length
  const active = list.flatMap(x => (x.st?.active || []).map(r => ({ r, x })))

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <h1>{t('Обзор')}</h1>
          <div className="desc">{t('Состояние серверов и последних бэкапов')}</div>
        </div>
        <div className="actions">
          <Button icon={<RefreshCw size={15} className={busy === 'r' ? 'spin' : ''} />} onClick={() => run('r', refreshAll)}>{t('Обновить')}</Button>
        </div>
      </div>

      <div className="grid g4">
        <div className="card stat"><div className="label"><Wifi size={14} /> {t('Серверы на связи')}</div><div className="value">{first && !Object.keys(statuses).length ? '…' : `${online} / ${state.servers.length}`}</div><div className="foot">{t('агенты отвечают')}</div></div>
        <div className="card stat"><div className="label"><ListChecks size={14} /> {t('Задач')}</div><div className="value">{jobs}</div><div className="foot">{t('на всех серверах')}</div></div>
        <div className="card stat"><div className="label"><CheckCircle2 size={14} /> {t('Успешно за 7 дней')}</div><div className="value" style={{ color: 'var(--success)' }}>{ok}</div><div className="foot">{t('выполненных бэкапов')}</div></div>
        <div className="card stat"><div className="label"><AlertOctagon size={14} /> {t('Ошибок за 7 дней')}</div><div className="value" style={{ color: failed ? 'var(--danger)' : undefined }}>{failed}</div><div className="foot">{failed ? t('проверьте историю') : t('всё в порядке')}</div></div>
      </div>

      {active.length > 0 && (
        <>
          <div className="section-title">{t('Сейчас выполняется')}</div>
          <div className="col">
            {active.map(({ r, x }) => (
              <RunProgress key={r.id} run={r} server={x.s.name}
                cancelling={busy === 'c' + r.id}
                onCancel={() => run('c' + r.id, () => api.cancelRun(x.s.id, r.id).then(refreshAll))} />
            ))}
          </div>
        </>
      )}

      <div className="section-title">{t('Серверы')}</div>
      <div className="grid g3">
        {list.map(({ s, st }) => {
          const sys = st?.info?.sys
          const next = st?.info?.jobs.map(j => j.nextRun).filter(Boolean).sort()[0]
          const last = st?.recent?.find(r => r.status !== 'queued' && r.status !== 'running')
          return (
            <div key={s.id} className="card server-card" onClick={() => go({ page: 'server', id: s.id })}>
              <div className="row">
                <span className={`dot ${!st ? '' : st.online ? 'on' : 'off'}`} />
                <div className="grow">
                  <div style={{ fontWeight: 600 }} className="ellipsis">{s.name}</div>
                  <div className="small faint ellipsis">{s.user}@{s.host}</div>
                </div>
                {last && <StatusBadge status={last.status} />}
              </div>
              {st?.online && sys ? (
                <div className="col mt12" style={{ gap: 8 }}>
                  <div className="small muted ellipsis">{sys.os}</div>
                  <div>
                    <div className="row small"><HardDrive size={13} className="faint" /><span className="grow muted">{t('Свободно RAM')}</span><b>{bytes(sys.memAvailable)}</b><span className="faint">{t('из {total}', { total: bytes(sys.memTotal) })}</span></div>
                    <div className="mt8"><Bar value={1 - sys.memAvailable / sys.memTotal} warn={sys.memAvailable / sys.memTotal < 0.15} /></div>
                  </div>
                  <div className="row small muted">
                    <span className="grow">{t('Задач:')} <b>{st.info!.jobs.length}</b></span>
                    <span>{next ? <>{t('След.:')} <b>{relative(next)}</b></> : t('Нет расписания')}</span>
                  </div>
                </div>
              ) : (
                <div className="small mt12" style={{ color: st ? 'var(--danger)' : 'var(--text-3)' }}>
                  {!st ? t('Подключение…') : st.noAgent ? t('Агент не установлен') : st.error}
                </div>
              )}
            </div>
          )
        })}
      </div>

      <div className="section-title">{t('Последние запуски')}</div>
      <div className="card">
        {recent.length === 0 ? <div className="empty small">{t('Запусков пока не было')}</div> : (
          <table className="table">
            <thead><tr><th>{t('Задача')}</th><th>{t('Сервер')}</th><th>{t('Начало')}</th><th>{t('Длительность')}</th><th>{t('Размер')}</th><th>{t('Статус')}</th></tr></thead>
            <tbody>
              {recent.slice(0, 12).map(r => (
                <tr key={r.id} className="click" onClick={() => go(statuses[r.sid]?.info?.jobs.some(j => j.id === r.jobId && j.kind === 'site')
                  ? { page: 'site', server: r.sid, job: r.jobId } : { page: 'server', id: r.sid })}>
                  <td style={{ fontWeight: 550 }}>{r.jobName}<div className="small faint">{t(r.trigger)}</div></td>
                  <td className="muted">{r.server}</td>
                  <td className="muted">{dateTime(r.startedAt)}</td>
                  <td className="muted">{duration(r.startedAt, r.finishedAt)}</td>
                  <td className="muted">{bytes(r.bytes)}</td>
                  <td><StatusBadge status={r.status} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}

function ShieldIcon() {
  return <svg width="26" height="26" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" /><path d="m9 12 2 2 4-4" /></svg>
}
