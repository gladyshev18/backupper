import { useState } from 'react'
import { AlertOctagon, CheckCircle2, Cloud, HardDrive, ListChecks, RefreshCw, Server as ServerIcon, Wifi } from 'lucide-react'
import { useApp } from '../App'
import { api } from '../api'
import type { Run } from '../types'
import { Bar, Button, Empty, StatusBadge, usePoll, useUI } from '../components/ui'
import RunProgress from '../components/RunProgress'
import { bytes, dateTime, duration, relative } from '../util'

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
          <Empty icon={<ShieldIcon />} title="Добро пожаловать в Backupper" action={<div className="row" style={{ justifyContent: 'center' }}>
            {!state.storages.length && <Button icon={<Cloud size={16} />} onClick={() => go({ page: 'clouds' })}>Подключить облако</Button>}
            <Button variant="primary" icon={<ServerIcon size={16} />} onClick={addServer}>Добавить сервер</Button>
          </div>}>
            <div style={{ maxWidth: 520, margin: '8px auto 0', textAlign: 'left' }}>
              <p className="muted">Бэкапы выполняет агент на самом сервере — по расписанию и без участия этого компьютера. Архив сразу уходит в облако кусками, не занимая место на диске сервера.</p>
              <ol className="steps mt16">
                <li>Подключите Яндекс Диск или Облако Mail.ru</li>
                <li>Добавьте сервер — доступ по вашему SSH-ключу</li>
                <li>Установите агент одной кнопкой</li>
                <li>Создайте задачу: базы, файлы сайта, конфиги и расписание</li>
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
          <h1>Обзор</h1>
          <div className="desc">Состояние серверов и последних бэкапов</div>
        </div>
        <div className="actions">
          <Button icon={<RefreshCw size={15} className={busy === 'r' ? 'spin' : ''} />} onClick={() => run('r', refreshAll)}>Обновить</Button>
        </div>
      </div>

      <div className="grid g4">
        <div className="card stat"><div className="label"><Wifi size={14} /> Серверы на связи</div><div className="value">{first && !Object.keys(statuses).length ? '…' : `${online} / ${state.servers.length}`}</div><div className="foot">агенты отвечают</div></div>
        <div className="card stat"><div className="label"><ListChecks size={14} /> Задач</div><div className="value">{jobs}</div><div className="foot">на всех серверах</div></div>
        <div className="card stat"><div className="label"><CheckCircle2 size={14} /> Успешно за 7 дней</div><div className="value" style={{ color: 'var(--success)' }}>{ok}</div><div className="foot">выполненных бэкапов</div></div>
        <div className="card stat"><div className="label"><AlertOctagon size={14} /> Ошибок за 7 дней</div><div className="value" style={{ color: failed ? 'var(--danger)' : undefined }}>{failed}</div><div className="foot">{failed ? 'проверьте историю' : 'всё в порядке'}</div></div>
      </div>

      {active.length > 0 && (
        <>
          <div className="section-title">Сейчас выполняется</div>
          <div className="col">
            {active.map(({ r, x }) => (
              <RunProgress key={r.id} run={r} server={x.s.name}
                cancelling={busy === 'c' + r.id}
                onCancel={() => run('c' + r.id, () => api.cancelRun(x.s.id, r.id).then(refreshAll))} />
            ))}
          </div>
        </>
      )}

      <div className="section-title">Серверы</div>
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
                    <div className="row small"><HardDrive size={13} className="faint" /><span className="grow muted">Свободно RAM</span><b>{bytes(sys.memAvailable)}</b><span className="faint">из {bytes(sys.memTotal)}</span></div>
                    <div className="mt8"><Bar value={1 - sys.memAvailable / sys.memTotal} warn={sys.memAvailable / sys.memTotal < 0.15} /></div>
                  </div>
                  <div className="row small muted">
                    <span className="grow">Задач: <b>{st.info!.jobs.length}</b></span>
                    <span>{next ? <>След.: <b>{relative(next)}</b></> : 'Нет расписания'}</span>
                  </div>
                </div>
              ) : (
                <div className="small mt12" style={{ color: st ? 'var(--danger)' : 'var(--text-3)' }}>
                  {!st ? 'Подключение…' : st.noAgent ? 'Агент не установлен' : st.error}
                </div>
              )}
            </div>
          )
        })}
      </div>

      <div className="section-title">Последние запуски</div>
      <div className="card">
        {recent.length === 0 ? <div className="empty small">Запусков пока не было</div> : (
          <table className="table">
            <thead><tr><th>Задача</th><th>Сервер</th><th>Начало</th><th>Длительность</th><th>Размер</th><th>Статус</th></tr></thead>
            <tbody>
              {recent.slice(0, 12).map(r => (
                <tr key={r.id} className="click" onClick={() => go({ page: 'server', id: r.sid })}>
                  <td style={{ fontWeight: 550 }}>{r.jobName}<div className="small faint">{r.trigger}</div></td>
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
