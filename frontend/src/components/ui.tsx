import { createContext, ReactNode, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { AlertTriangle, CheckCircle2, Info, Loader2, X, XCircle } from 'lucide-react'
import { cls, statusLabels } from '../util'
import { errText } from '../api'
import { t } from '../i18n'

export function Button({ children, variant, size, icon, loading, className, ...p }: {
  variant?: 'primary' | 'danger' | 'ghost'; size?: 'sm'; icon?: ReactNode; loading?: boolean
} & React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button className={cls('btn', variant, size, !children && 'icon', className)} disabled={loading || p.disabled} {...p}>
      {loading ? <Loader2 size={size === 'sm' ? 14 : 16} className="spin" /> : icon}
      {children}
    </button>
  )
}

export function Modal({ title, onClose, children, footer, wide, drawer, icon }: {
  title: ReactNode; onClose: () => void; children: ReactNode; footer?: ReactNode; wide?: boolean; drawer?: boolean; icon?: ReactNode
}) {
  useEffect(() => {
    const h = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onClose])
  return (
    <div className="overlay" onMouseDown={e => e.target === e.currentTarget && !drawer && onClose()}>
      <div className={cls(drawer ? 'drawer' : 'modal', wide && 'wide')}>
        <div className="modal-head">
          {icon}
          <h2 className="grow">{title}</h2>
          <Button variant="ghost" size="sm" icon={<X size={16} />} onClick={onClose} />
        </div>
        <div className="modal-body">{children}</div>
        {footer && <div className="modal-foot">{footer}</div>}
      </div>
    </div>
  )
}

export function Field({ label, hint, children, className }: { label?: ReactNode; hint?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <div className={cls('field', className)}>
      {label && <label>{label}</label>}
      {children}
      {hint && <div className="hint">{hint}</div>}
    </div>
  )
}

export function Toggle({ on, onChange, label }: { on: boolean; onChange: (v: boolean) => void; label?: ReactNode }) {
  return (
    <span className={cls('toggle', on && 'on')} onClick={() => onChange(!on)} role="switch" aria-checked={on}>
      <span className="track" />
      {label && <span>{label}</span>}
    </span>
  )
}

export function Seg<T extends string>({ value, options, onChange }: { value: T; options: [T, ReactNode][]; onChange: (v: T) => void }) {
  return (
    <div className="seg">
      {options.map(([v, l]) => <button key={v} className={cls(v === value && 'on')} onClick={() => onChange(v)} type="button">{l}</button>)}
    </div>
  )
}

export function StatusBadge({ status }: { status: string }) {
  const tone = ({ success: 'success', warning: 'warning', failed: 'danger', running: 'accent', queued: 'accent', incomplete: 'warning' } as Record<string, string>)[status]
  return (
    <span className={cls('badge', tone)}>
      {status === 'running' && <Loader2 size={12} className="spin" />}
      {statusLabels[status] ? t(statusLabels[status]) : status}
    </span>
  )
}

export function Bar({ value, warn, indeterminate }: { value: number; warn?: boolean; indeterminate?: boolean }) {
  return (
    <div className={cls('bar', warn && 'warn', indeterminate && 'indeterminate')}>
      <div style={{ width: `${Math.min(100, Math.max(0, value * 100))}%` }} />
    </div>
  )
}

export function Alert({ tone, children, icon }: { tone?: 'warning' | 'danger' | 'success'; children: ReactNode; icon?: ReactNode }) {
  const Ico = tone === 'danger' ? XCircle : tone === 'warning' ? AlertTriangle : tone === 'success' ? CheckCircle2 : Info
  return <div className={cls('alert', tone)}>{icon || <Ico size={17} />}<div className="grow">{children}</div></div>
}

export function Empty({ icon, title, children, action }: { icon: ReactNode; title: string; children?: ReactNode; action?: ReactNode }) {
  return (
    <div className="empty">
      <div className="ico">{icon}</div>
      <h3>{title}</h3>
      <div>{children}</div>
      {action && <div className="mt16">{action}</div>}
    </div>
  )
}

export function Menu({ trigger, children }: { trigger: ReactNode; children: ReactNode }) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const h = (e: MouseEvent) => { if (!ref.current?.contains(e.target as Node)) setOpen(false) }
    window.addEventListener('mousedown', h)
    return () => window.removeEventListener('mousedown', h)
  }, [open])
  return (
    <div style={{ position: 'relative' }} ref={ref}>
      <span onClick={() => setOpen(!open)}>{trigger}</span>
      {open && <div className="menu" onClick={() => setOpen(false)}>{children}</div>}
    </div>
  )
}

// ---- toasts & confirm ----

type Toast = { id: number; tone: 'success' | 'error' | 'info'; text: string }
type Confirm = { text: ReactNode; ok: string; danger?: boolean; resolve: (v: boolean) => void }

const Ctx = createContext<{
  toast: (tone: Toast['tone'], text: string) => void
  confirm: (text: ReactNode, ok?: string, danger?: boolean) => Promise<boolean>
}>(null as any)

export function UIProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])
  const [conf, setConf] = useState<Confirm | null>(null)
  const toast = useCallback((tone: Toast['tone'], text: string) => {
    const id = Date.now() + Math.random()
    setToasts(t => [...t, { id, tone, text }])
    setTimeout(() => setToasts(t => t.filter(x => x.id !== id)), tone === 'error' ? 8000 : 4000)
  }, [])
  const confirm = useCallback((text: ReactNode, ok = t('Подтвердить'), danger = false) =>
    new Promise<boolean>(resolve => setConf({ text, ok, danger, resolve })), [])
  const close = (v: boolean) => { conf?.resolve(v); setConf(null) }
  return (
    <Ctx.Provider value={{ toast, confirm }}>
      {children}
      <div className="toasts">
        {toasts.map(t => (
          <div key={t.id} className={cls('toast', t.tone)}>
            {t.tone === 'error' ? <XCircle size={18} /> : t.tone === 'success' ? <CheckCircle2 size={18} /> : <Info size={18} />}
            <div className="grow selectable">{t.text}</div>
          </div>
        ))}
      </div>
      {conf && (
        <Modal title={t('Подтверждение')} onClose={() => close(false)} footer={<>
          <Button onClick={() => close(false)}>{t('Отмена')}</Button>
          <Button variant={conf.danger ? 'danger' : 'primary'} onClick={() => close(true)} autoFocus>{conf.ok}</Button>
        </>}>
          <div className="muted">{conf.text}</div>
        </Modal>
      )}
    </Ctx.Provider>
  )
}

export function useUI() {
  const ctx = useContext(Ctx)
  // Wraps an async action: shows errors as toasts and tracks loading state.
  const [busy, setBusy] = useState<string | null>(null)
  const run = useCallback(async <T,>(key: string, fn: () => Promise<T>, okText?: string): Promise<T | undefined> => {
    setBusy(key)
    try {
      const r = await fn()
      if (okText) ctx.toast('success', okText)
      return r
    } catch (e) {
      ctx.toast('error', errText(e))
      return undefined
    } finally {
      setBusy(null)
    }
  }, [ctx])
  return { ...ctx, busy, run }
}

export function usePoll(fn: () => void, ms: number, deps: any[]) {
  useEffect(() => {
    fn()
    const t = setInterval(fn, ms)
    return () => clearInterval(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ms, ...deps])
}
