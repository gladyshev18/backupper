import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Archive, ChevronDown, ChevronRight, Database, FolderTree, Gauge, HardDrive, Lock, MemoryStick, Plus, Search,
  Settings2, Timer, Trash2, Zap,
} from 'lucide-react'
import { useApp } from '../App'
import { api, errText } from '../api'
import type { ChunkPlan, Job, JobView, ProbeResult, Source, SysInfo } from '../types'
import { Alert, Bar, Button, Field, Modal, Seg, Toggle, useUI } from '../components/ui'
import { bytes, cls, configPresets, dateTime, kindLabels, newJob, newSource, sourceLabels, speed } from '../util'

// Same "round" sizes as the agent's planner (internal/chunk).
const SIZES_MB = [8, 16, 24, 32, 48, 64, 96, 128, 192, 256, 384, 512, 768, 1024, 1536, 2048]

export default function JobEditor({ serverId, sys, job, onClose, onSaved }: {
  serverId: string; sys: SysInfo; job: JobView | null; onClose: () => void; onSaved: () => void
}) {
  const { state, go } = useApp()
  const { run, busy } = useUI()
  const [j, setJ] = useState<Job>(() => job ? structuredClone(job) : newJob(state.storages.slice(0, 1).map(s => s.id)))
  const set = (p: Partial<Job>) => setJ(v => ({ ...v, ...p }))
  const [open, setOpen] = useState<string | null>(job ? null : 'new')

  const save = () => run('save', async () => {
    const { nextRun, lastRun, cronError, ...clean } = j as JobView
    await api.saveJob(serverId, clean)
    onSaved()
  }, job ? 'Задача сохранена' : 'Задача создана')

  const addSource = (t: Source['type']) => {
    const s = newSource(t)
    const same = j.sources.filter(x => x.type === t).length
    if (same) s.name = `${s.name}${same + 1}`
    set({ sources: [...j.sources, s] })
    setOpen(s.id)
    if (!j.name) set({ sources: [...j.sources, s], name: t === 'configs' ? 'Конфиги сервера' : t === 'files' ? 'Сайт' : 'Базы данных' })
  }
  const updSource = (id: string, p: Partial<Source>) => set({ sources: j.sources.map(s => s.id === id ? { ...s, ...p } : s) })

  return (
    <Modal drawer title={job ? `Задача «${job.name}»` : 'Новая задача'} icon={<div className="job-icon" style={{ width: 34, height: 34 }}><Archive size={17} /></div>} onClose={onClose}
      footer={<>
        <span className="small faint grow">Задача хранится на сервере и выполняется агентом, даже когда панель закрыта.</span>
        <Button onClick={onClose}>Отмена</Button>
        <Button variant="primary" loading={busy === 'save'} onClick={save}>{job ? 'Сохранить' : 'Создать задачу'}</Button>
      </>}>

      <div className="editor-section">
        <h3><span className="num">1</span> Название</h3>
        <input className="input" value={j.name} onChange={e => set({ name: e.target.value })} placeholder="Например: Магазин — база и файлы" autoFocus={!job} />
        <div className="hint small faint mt8">Используется как имя папки в облаке.</div>
      </div>

      <div className="editor-section">
        <h3><span className="num">2</span> Что бэкапить</h3>
        <div className="col">
          {j.sources.map(s => (
            <SourceEditor key={s.id} serverId={serverId} src={s} open={open === s.id} onToggle={() => setOpen(open === s.id ? null : s.id)}
              onChange={p => updSource(s.id, p)} onRemove={() => set({ sources: j.sources.filter(x => x.id !== s.id) })} />
          ))}
        </div>
        <div className={cls('add-source', j.sources.length > 0 && 'mt12')}>
          <button onClick={() => addSource('mysql')}><Database size={20} /> MySQL / MariaDB</button>
          <button onClick={() => addSource('postgres')}><Database size={20} /> PostgreSQL</button>
          <button onClick={() => addSource('files')}><FolderTree size={20} /> Файлы сайта</button>
          <button onClick={() => addSource('configs')}><Settings2 size={20} /> Конфиги сервера</button>
        </div>
      </div>

      <ScheduleSection j={j} set={set} sys={sys} />

      <div className="editor-section">
        <h3><span className="num">4</span> Куда выгружать и сколько хранить</h3>
        {state.storages.length === 0 ? (
          <Alert tone="warning">Нет подключённых облаков. <a href="#" onClick={e => { e.preventDefault(); onClose(); go({ page: 'clouds' }) }}>Подключить облако</a></Alert>
        ) : (
          <div className="grid g2">
            {state.storages.map(s => {
              const on = j.storages.includes(s.id)
              return (
                <label key={s.id} className={cls('kind-tile', on && 'on')} style={{ flexDirection: 'row', alignItems: 'center' }}>
                  <input type="checkbox" checked={on} style={{ accentColor: 'var(--accent)', width: 16, height: 16 }}
                    onChange={() => set({ storages: on ? j.storages.filter(x => x !== s.id) : [...j.storages, s.id] })} />
                  <div className="grow">
                    <div style={{ fontWeight: 600 }}>{s.name}</div>
                    <div className="small faint">{kindLabels[s.kind]} · {s.root}</div>
                  </div>
                </label>
              )
            })}
          </div>
        )}
        <div className="grid g3 mt16">
          <Field label="Хранить последних копий" hint="Более старые удаляются после успешного бэкапа. 0 — не удалять.">
            <input className="input" type="number" min={0} value={j.keep} onChange={e => set({ keep: Math.max(0, +e.target.value) })} />
          </Field>
          <Field label="Уведомления в Telegram" hint={state.settings.telegram.enabled ? undefined : 'Telegram не настроен в «Настройках»'}>
            <select className="select" value={j.notify} onChange={e => set({ notify: e.target.value as Job['notify'] })}>
              <option value="errors">Только об ошибках</option>
              <option value="always">О каждом запуске</option>
              <option value="never">Не уведомлять</option>
            </select>
          </Field>
          <Field label="Сжатие (gzip)" hint={j.compression <= 3 ? 'Быстро, меньше нагрузка на CPU' : j.compression >= 8 ? 'Максимум, сильно грузит CPU' : 'Баланс скорости и размера'}>
            <div className="row"><input type="range" min={1} max={9} value={j.compression} onChange={e => set({ compression: +e.target.value })} /><b style={{ width: 16 }}>{j.compression}</b></div>
          </Field>
        </div>
        <div className="mt16">
          <Toggle on={j.encrypt} onChange={v => set({ encrypt: v })} label={<span className="row" style={{ gap: 6 }}><Lock size={14} /> Шифровать архивы (age, AES-256 / ChaCha20)</span>} />
          {j.encrypt && !state.settings.encryptionPassword && <div className="mt8"><Alert tone="warning">Задайте пароль шифрования в «Настройках», иначе бэкап завершится ошибкой.</Alert></div>}
          {j.encrypt && state.settings.encryptionPassword && <div className="small faint mt8">Без пароля из «Настроек» копии восстановить невозможно — сохраните его в надёжном месте.</div>}
        </div>
      </div>

      <ChunkSection serverId={serverId} j={j} set={set} />
    </Modal>
  )
}

function SourceEditor({ serverId, src, open, onToggle, onChange, onRemove }: {
  serverId: string; src: Source; open: boolean; onToggle: () => void; onChange: (p: Partial<Source>) => void; onRemove: () => void
}) {
  const [probe, setProbe] = useState<ProbeResult | null>(null)
  const [probing, setProbing] = useState(false)
  const isDB = src.type === 'mysql' || src.type === 'postgres'
  const doProbe = async () => {
    setProbing(true)
    try { setProbe(await api.probe(serverId, src)) } catch (e) { setProbe({ ok: false, error: errText(e) }) } finally { setProbing(false) }
  }
  const Icon = isDB ? Database : src.type === 'configs' ? Settings2 : FolderTree
  const summary = isDB
    ? (src.databases?.length ? src.databases.join(', ') : 'все базы') + (src.perDatabase ? ' · по файлу на базу' : '')
    : src.type === 'configs' ? `${src.paths?.length || 0} путей (отсутствующие пропускаются)` : (src.paths || []).join(', ')
  const lines = (a?: string[]) => (a || []).join('\n')
  const parse = (s: string) => s.split('\n').map(x => x.trim()).filter(Boolean)
  const custom = (src.paths || []).filter(p => !configPresets.includes(p))

  return (
    <div className="source-card">
      <div className="source-head" style={{ cursor: 'pointer' }} onClick={onToggle}>
        {open ? <ChevronDown size={16} className="faint" /> : <ChevronRight size={16} className="faint" />}
        <div className="job-icon" style={{ width: 30, height: 30, borderRadius: 8 }}><Icon size={15} /></div>
        <div className="grow">
          <div style={{ fontWeight: 600 }}>{sourceLabels[src.type]} <span className="faint mono" style={{ fontWeight: 400 }}>· {src.name}</span></div>
          <div className="small faint ellipsis">{summary}</div>
        </div>
        <Button size="sm" variant="danger" icon={<Trash2 size={13} />} onClick={e => { e.stopPropagation(); onRemove() }} title="Убрать источник" />
      </div>
      {open && (
        <div className="source-body">
          <Field label="Имя в архиве" hint={`Файлы будут называться ${src.name || '…'}${isDB ? (src.perDatabase ? '__<база>.sql.gz' : '.sql.gz') : '.tar.gz'}`}>
            <input className="input mono" value={src.name} onChange={e => onChange({ name: e.target.value })} />
          </Field>
          {isDB && (
            <>
              <div className="grid g2">
                <Field label="Пользователь" hint={src.type === 'mysql' ? 'Пусто — root через сокет или ~/.my.cnf' : 'Пусто — от системного пользователя postgres'}>
                  <input className="input" value={src.user} onChange={e => onChange({ user: e.target.value })} />
                </Field>
                <Field label="Пароль" hint="Передаётся через временный файл, не виден в списке процессов">
                  <input className="input" type="password" value={src.password} onChange={e => onChange({ password: e.target.value })} />
                </Field>
              </div>
              <div className="grid g2">
                <Field label="Хост" hint="Пусто — локальный сервер">
                  <input className="input" value={src.host} onChange={e => onChange({ host: e.target.value })} placeholder="localhost" />
                </Field>
                <Field label="Порт">
                  <input className="input" type="number" value={src.port || ''} onChange={e => onChange({ port: +e.target.value })} placeholder={src.type === 'mysql' ? '3306' : '5432'} />
                </Field>
              </div>
              <Field label="Базы данных" hint="Пусто — все базы (системные пропускаются). Нажмите «Проверить», чтобы выбрать из списка.">
                <input className="input mono" value={(src.databases || []).join(', ')} onChange={e => onChange({ databases: e.target.value.split(',').map(x => x.trim()).filter(Boolean) })} placeholder="все базы" />
              </Field>
              <Toggle on={!!src.perDatabase} onChange={v => onChange({ perDatabase: v })} label="Отдельный файл на каждую базу (удобнее восстанавливать одну базу)" />
              <Field label="Дополнительные параметры" hint={src.type === 'mysql' ? 'Например: --events --ignore-table=shop.sessions' : 'Например: --exclude-table-data=logs'}>
                <input className="input mono" value={src.extraArgs || ''} onChange={e => onChange({ extraArgs: e.target.value })} />
              </Field>
            </>
          )}
          {src.type === 'files' && (
            <div className="grid g2">
              <Field label="Папки и файлы (по одному в строке)">
                <textarea className="textarea" rows={4} value={lines(src.paths)} onChange={e => onChange({ paths: parse(e.target.value) })} />
              </Field>
              <Field label="Исключить (шаблоны tar)" hint="Например: */cache/*, *.log, node_modules">
                <textarea className="textarea" rows={4} value={lines(src.excludes)} onChange={e => onChange({ excludes: parse(e.target.value) })} />
              </Field>
            </div>
          )}
          {src.type === 'configs' && (
            <>
              <div className="small muted">Отмеченные пути попадут в архив, если существуют на сервере — лишние можно не снимать.</div>
              <div className="grid g3" style={{ gap: 6 }}>
                {configPresets.map(p => {
                  const on = src.paths?.includes(p)
                  return (
                    <label key={p} className="check small mono">
                      <input type="checkbox" checked={!!on} onChange={() => onChange({ paths: on ? src.paths!.filter(x => x !== p) : [...(src.paths || []), p] })} />{p}
                    </label>
                  )
                })}
              </div>
              <Field label="Свои пути (по одному в строке)">
                <textarea className="textarea" rows={2} value={custom.join('\n')} onChange={e => onChange({ paths: [...(src.paths || []).filter(p => configPresets.includes(p)), ...parse(e.target.value)] })} placeholder="/opt/app/.env" />
              </Field>
            </>
          )}
          <div className="row">
            <Button size="sm" icon={<Search size={13} />} loading={probing} onClick={doProbe}>Проверить на сервере</Button>
            {probe && !probe.ok && <span className="small" style={{ color: 'var(--danger)' }}>{probe.error}</span>}
            {probe?.ok && probe.paths && <span className="small" style={{ color: 'var(--success)' }}>Найдено: {probe.paths.length} из {src.paths?.length}</span>}
          </div>
          {probe?.ok && probe.databases && (
            <div className="card card-pad" style={{ background: 'var(--surface-2)' }}>
              <div className="small muted" style={{ marginBottom: 8 }}>Подключение работает. Отметьте базы (ничего не отмечено — все):</div>
              <div className="grid g3" style={{ gap: 6 }}>
                {probe.databases.map(d => {
                  const on = src.databases?.includes(d)
                  return <label key={d} className="check small mono"><input type="checkbox" checked={!!on} onChange={() => onChange({ databases: on ? src.databases!.filter(x => x !== d) : [...(src.databases || []), d] })} />{d}</label>
                })}
              </div>
            </div>
          )}
          {probe?.ok && probe.paths && src.type === 'configs' && <div className="small faint mono">{probe.paths.join('  ')}</div>}
        </div>
      )}
    </div>
  )
}

const days: [number, string][] = [[1, 'Пн'], [2, 'Вт'], [3, 'Ср'], [4, 'Чт'], [5, 'Пт'], [6, 'Сб'], [0, 'Вс']]

function ScheduleSection({ j, set, sys }: { j: Job; set: (p: Partial<Job>) => void; sys: SysInfo }) {
  const s = j.schedule
  const upd = (p: Partial<Job['schedule']>) => set({ schedule: { ...s, ...p } })
  const [next, setNext] = useState<string[]>([])
  const [err, setErr] = useState('')
  useEffect(() => {
    if (!s.enabled) { setNext([]); setErr(''); return }
    const t = setTimeout(() => api.nextRuns(s, sys.tzOffset, 3).then(r => { setNext(r); setErr('') }).catch(e => { setNext([]); setErr(errText(e)) }), 250)
    return () => clearTimeout(t)
  }, [JSON.stringify(s)])
  const tz = `UTC${sys.tzOffset >= 0 ? '+' : ''}${sys.tzOffset / 3600}`
  const fmt = (iso: string) => {
    // Show the server-local wall time as written in the ISO string.
    const d = new Date(iso.slice(0, 19))
    return `${d.toLocaleDateString('ru-RU', { weekday: 'short', day: 'numeric', month: 'short' })}, ${iso.slice(11, 16)}`
  }
  return (
    <div className="editor-section">
      <h3><span className="num">3</span> Расписание</h3>
      <div className="row wrap" style={{ gap: 14 }}>
        <Toggle on={s.enabled} onChange={v => upd({ enabled: v })} label={s.enabled ? 'По расписанию' : 'Только вручную'} />
        {s.enabled && <Seg value={s.mode} onChange={m => upd({ mode: m })} options={[['daily', 'Каждый день'], ['weekly', 'По дням недели'], ['monthly', 'Раз в месяц'], ['interval', 'Каждые N часов'], ['cron', 'Cron']]} />}
      </div>
      {s.enabled && (
        <div className="row wrap mt16" style={{ gap: 16, alignItems: 'flex-end' }}>
          {s.mode !== 'cron' && (
            <Field label={s.mode === 'interval' ? 'Начиная с' : 'Время'}>
              <input className="input" type="time" value={s.time} onChange={e => upd({ time: e.target.value })} style={{ width: 130 }} />
            </Field>
          )}
          {s.mode === 'weekly' && (
            <Field label="Дни недели">
              <div className="days">
                {days.map(([d, l]) => {
                  const on = s.weekdays?.includes(d)
                  return <button key={d} className={cls(on && 'on')} onClick={() => upd({ weekdays: on ? s.weekdays.filter(x => x !== d) : [...(s.weekdays || []), d] })}>{l}</button>
                })}
              </div>
            </Field>
          )}
          {s.mode === 'monthly' && (
            <Field label="Число месяца">
              <select className="select" value={s.monthDay} onChange={e => upd({ monthDay: +e.target.value })} style={{ width: 100 }}>
                {Array.from({ length: 28 }, (_, i) => <option key={i + 1} value={i + 1}>{i + 1}</option>)}
              </select>
            </Field>
          )}
          {s.mode === 'interval' && (
            <Field label="Интервал">
              <select className="select" value={s.everyHours} onChange={e => upd({ everyHours: +e.target.value })} style={{ width: 150 }}>
                {[1, 2, 3, 4, 6, 8, 12].map(h => <option key={h} value={h}>каждые {h} ч</option>)}
              </select>
            </Field>
          )}
          {s.mode === 'cron' && (
            <Field label="Выражение cron (мин час день месяц день_недели)" className="grow">
              <input className="input mono" value={s.cron} onChange={e => upd({ cron: e.target.value })} placeholder="30 2 * * 1-5" />
            </Field>
          )}
        </div>
      )}
      {s.enabled && (
        <div className="small mt12" style={{ color: err ? 'var(--danger)' : 'var(--text-2)' }}>
          {err || <>Ближайшие запуски: <b>{next.map(fmt).join(' · ')}</b> <span className="faint">(время сервера, {sys.tz} {tz})</span></>}
        </div>
      )}
    </div>
  )
}

function ChunkSection({ serverId, j, set }: { serverId: string; j: Job; set: (p: Partial<Job>) => void }) {
  const { state } = useApp()
  const { toast } = useUI()
  const c = j.chunk
  const upd = (p: Partial<Job['chunk']>) => set({ chunk: { ...c, ...p } })
  const [plan, setPlan] = useState<ChunkPlan | null>(null)
  const [testing, setTesting] = useState(false)
  const seq = useRef(0)
  const key = JSON.stringify([c, j.storages])

  const load = () => {
    const n = ++seq.current
    api.plan(serverId, j).then(p => { if (n === seq.current) setPlan(p) }).catch(() => {})
  }
  useEffect(() => { const t = setTimeout(load, 300); return () => clearTimeout(t) }, [key])

  const speedTest = async () => {
    setTesting(true)
    try {
      for (const sid of j.storages) {
        const name = state.storages.find(s => s.id === sid)?.name || sid
        const r = await api.speedTest(serverId, sid)
        if (r.ok) toast('success', `${name}: ${speed(r.speedBps)} с сервера`)
        else toast('error', `${name}: ${r.error}`)
      }
      load()
    } catch (e) { toast('error', errText(e)) } finally { setTesting(false) }
  }

  const sizeIdx = Math.max(0, SIZES_MB.findIndex(s => s >= c.sizeMB))
  const memShare = plan && plan.memAvailable ? plan.peakMemory / plan.memAvailable : 0

  return (
    <div className="editor-section">
      <h3><span className="num">5</span> Размер кусков <span className="badge accent" style={{ marginLeft: 4 }}><Zap size={11} /> умная настройка</span></h3>
      <div className="small muted" style={{ maxWidth: 680 }}>
        Архив не сохраняется на диск сервера: он режется на куски в памяти и сразу уходит в облако. Пока один кусок загружается, следующий уже заполняется —
        поэтому в памяти одновременно два куска. Если кусок не загрузился, он отправляется заново целиком.
      </div>
      <div className="row wrap mt16" style={{ gap: 14 }}>
        <Seg value={c.mode} onChange={m => upd({ mode: m })} options={[['auto', 'Автоматически'], ['manual', 'Вручную']]} />
        <span className="grow" />
        <Button size="sm" icon={<Gauge size={14} />} loading={testing} disabled={!j.storages.length} onClick={speedTest}>Измерить скорость с сервера</Button>
      </div>

      <div className="grid g3 mt16">
        {c.mode === 'auto' ? (
          <>
            <Field label={<>Можно занять памяти: <b>{c.maxMemoryPercent}%</b> свободной</>} hint="Меньше — безопаснее для сайтов, больше — меньше частей">
              <input type="range" min={5} max={50} step={5} value={c.maxMemoryPercent} onChange={e => upd({ maxMemoryPercent: +e.target.value })} />
            </Field>
            <Field label={<>Загрузка одного куска: <b>~{c.targetSeconds} с</b></>} hint="Сколько данных не жалко переотправить при обрыве">
              <input type="range" min={15} max={300} step={15} value={c.targetSeconds} onChange={e => upd({ targetSeconds: +e.target.value })} />
            </Field>
          </>
        ) : (
          <Field label={<>Размер куска: <b>{bytes(SIZES_MB[sizeIdx] * 1048576)}</b></>} className="" hint="8 МБ — 2 ГБ">
            <input type="range" min={0} max={SIZES_MB.length - 1} value={sizeIdx} onChange={e => upd({ sizeMB: SIZES_MB[+e.target.value] })} />
          </Field>
        )}
        <Field label="Где держать кусок" hint={c.buffer === 'auto' ? 'В памяти; на диске — только если RAM почти нет' : c.buffer === 'disk' ? 'Временные файлы в /var/tmp, удаляются сразу' : 'Только оперативная память'}>
          <Seg value={c.buffer} onChange={b => upd({ buffer: b })} options={[['auto', 'Авто'], ['memory', 'Память'], ['disk', 'Диск']]} />
        </Field>
      </div>

      {plan ? (
        <div className="plan">
          <div>
            <div className="small faint">{c.mode === 'auto' ? 'Рекомендуемый размер' : 'Размер куска'}</div>
            <div className="plan-size">{bytes(plan.size)}</div>
            <div className="plan-metrics">
              <div><div className="k"><MemoryStick size={11} /> Пик памяти</div><div className="v">{bytes(plan.peakMemory)}</div></div>
              <div><div className="k"><HardDrive size={11} /> Диск</div><div className="v">{plan.peakDisk ? bytes(plan.peakDisk) : 'не нужен'}</div></div>
              <div><div className="k"><Timer size={11} /> Один кусок</div><div className="v">~{plan.partSeconds < 90 ? `${Math.max(1, Math.round(plan.partSeconds))} с` : `${(plan.partSeconds / 60).toFixed(1)} мин`}</div></div>
              <div><div className="k"><Archive size={11} /> Частей</div><div className="v">{plan.parts ? `~${plan.parts}` : '—'}</div></div>
            </div>
          </div>
          <div>
            <ul>{plan.reasons.map((r, i) => <li key={i}>{r}</li>)}</ul>
            <div className="mem-viz">
              <div className="row small"><span className="grow muted">Свободная память сервера: {bytes(plan.memAvailable)}</span><span className="faint">бэкап займёт {(memShare * 100).toFixed(memShare < 0.01 ? 1 : 0)}%</span></div>
              <div className="mt8"><Bar value={memShare} warn={memShare > 0.4} /></div>
              <div className="row small faint mt8" style={{ gap: 14 }}>
                <span>Скорость в облако: {plan.speedMeasured ? <b>{speed(plan.speedBps)}</b> : 'не измерена'}</span>
                {plan.expectedSize > 0 && <span>Прошлый архив: {bytes(plan.expectedSize)}</span>}
                {plan.maxFileSize > 0 && <span>Лимит файла в облаке: {bytes(plan.maxFileSize)}</span>}
              </div>
            </div>
            {plan.warnings.length > 0 && <div className="col mt12" style={{ gap: 6 }}>{plan.warnings.map((w, i) => <Alert key={i} tone="warning">{w}</Alert>)}</div>}
            <div className="small faint mt12">Во время бэкапа агент следит за памятью: если её станет мало, следующие куски автоматически уменьшатся; если кусок не проходит по сети, он делится пополам.</div>
          </div>
        </div>
      ) : <div className="mt16"><Bar value={0} indeterminate /></div>}
    </div>
  )
}
