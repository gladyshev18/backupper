import { Loader2, Square } from 'lucide-react'
import type { Run } from '../types'
import { bytes, duration, speed } from '../util'
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
            {queued ? 'Ожидает в очереди' : <>{p?.stage}{p?.source ? ` · ${p.source}` : ''}</>}
          </div>
        </div>
        {!queued && <div className="small faint">{duration(run.startedAt)}</div>}
        {onCancel && <Button size="sm" icon={<Square size={12} />} loading={cancelling} onClick={onCancel}>Остановить</Button>}
      </div>
      {!queued && (
        <>
          <div className="mt12"><Bar value={0} indeterminate /></div>
          <div className="row mt8 small muted wrap" style={{ gap: 18 }}>
            <span>Прочитано: <b>{bytes(p?.rawBytes)}</b></span>
            <span>Выгружено: <b>{bytes(run.bytes + (p?.bytes || 0))}</b></span>
            {!!p?.part && <span>Кусок №{p.part} по {bytes(run.chunkSize)}</span>}
            {!!p?.speedBps && <span>Скорость: {speed(p.speedBps)}</span>}
          </div>
        </>
      )}
    </div>
  )
}
