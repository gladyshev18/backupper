import { Loader2, Square } from 'lucide-react'
import type { Run } from '../types'
import { bytes, duration, speed } from '../util'
import { t } from '../i18n'
import { Bar, Button } from './ui'

/** How far the run is: for the whole run when its size is expected, else for the current file. */
function estimate(run: Run) {
  const p = run.progress
  const since = (s?: string) => (s && !s.startsWith('0001-') ? (Date.now() - new Date(s).getTime()) / 1000 : 0)
  if (run.estRawBytes) return { done: run.rawBytes + (p?.rawBytes || 0), total: run.estRawBytes, secs: since(run.startedAt), whole: true }
  if (p?.estRaw) return { done: p.rawBytes, total: p.estRaw, secs: since(p.since), whole: false }
  return null
}

function timeLeft(s: number): string {
  if (s < 60) return t('меньше минуты')
  if (s < 3600) return t('{n} мин', { n: Math.round(s / 60) })
  return t('{h} ч {m} мин', { h: Math.floor(s / 3600), m: Math.round((s % 3600) / 60) })
}

export default function RunProgress({ run, server, onCancel, cancelling }: { run: Run; server?: string; onCancel?: () => void; cancelling?: boolean }) {
  const p = run.progress
  const queued = run.status === 'queued'
  const e = estimate(run)
  const over = !!e && e.done >= e.total
  // The average speed so far: steadier than the last part's.
  const rate = e && e.secs > 5 ? e.done / e.secs : 0
  return (
    <div className="card card-pad">
      <div className="row">
        <div className="job-icon" style={{ width: 36, height: 36 }}><Loader2 size={18} className={queued ? '' : 'spin'} /></div>
        <div className="grow">
          <div style={{ fontWeight: 600 }}>{run.jobName}{server && <span className="faint" style={{ fontWeight: 400 }}> · {server}</span>}</div>
          <div className="small muted ellipsis">
            {queued ? t('Ожидает в очереди') : <>{p?.stage ? t(p.stage) : ''}{p?.source ? ` · ${p.source}` : ''}</>}
          </div>
        </div>
        {!queued && <div className="small faint">{duration(run.startedAt)}</div>}
        {onCancel && <Button size="sm" icon={<Square size={12} />} loading={cancelling} onClick={onCancel}>{t('Остановить')}</Button>}
      </div>
      {!queued && (
        <>
          <div className="mt12">{e && !over ? <Bar value={e.done / e.total} /> : <Bar value={0} indeterminate />}</div>
          <div className="row mt8 small muted wrap" style={{ gap: 18 }}>
            {!e ? <span>{t('Прочитано:')} <b>{bytes(p?.rawBytes)}</b></span>
              : over ? <span>{t('Прочитано:')} <b>{bytes(e.done)}</b> <span className="faint">{t('(больше ожидаемых ≈{total})', { total: bytes(e.total) })}</span></span>
              : <span>{t('Прочитано:')} <b>{bytes(e.done)}</b> {t('из ≈{total}', { total: bytes(e.total) })} <span className="faint">({Math.floor(e.done / e.total * 100)}%{e.whole ? '' : t(', текущий файл')})</span></span>}
            {e && !over && <span>{t('Осталось:')} <b>≈{bytes(e.total - e.done)}</b>{rate > 0 && <>, {t('примерно {time}', { time: timeLeft((e.total - e.done) / rate) })}</>}</span>}
            <span>{t('Выгружено:')} <b>{bytes(run.bytes + (p?.bytes || 0))}</b></span>
            {!!p?.part && <span>{t('Кусок №{n} по {size}', { n: p.part, size: bytes(run.chunkSize) })}</span>}
            {!!p?.speedBps && <span>{t('Скорость:')} {speed(p.speedBps)}</span>}
          </div>
        </>
      )}
    </div>
  )
}
