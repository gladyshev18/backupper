import { Loader2, Square } from 'lucide-react'
import type { Run } from '../types'
import { bytes, duration, speed } from '../util'
import { t } from '../i18n'
import { Bar, Button } from './ui'

export default function RunProgress({ run, server, onCancel, cancelling }: { run: Run; server?: string; onCancel?: () => void; cancelling?: boolean }) {
  const p = run.progress
  const queued = run.status === 'queued'
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
          <div className="mt12"><Bar value={0} indeterminate /></div>
          <div className="row mt8 small muted wrap" style={{ gap: 18 }}>
            <span>{t('Прочитано:')} <b>{bytes(p?.rawBytes)}</b></span>
            <span>{t('Выгружено:')} <b>{bytes(run.bytes + (p?.bytes || 0))}</b></span>
            {!!p?.part && <span>{t('Кусок №{n} по {size}', { n: p.part, size: bytes(run.chunkSize) })}</span>}
            {!!p?.speedBps && <span>{t('Скорость:')} {speed(p.speedBps)}</span>}
          </div>
        </>
      )}
    </div>
  )
}
