import { Fragment, useEffect, useRef, useState } from 'react'
import {
  Archive, CalendarRange, ChevronDown, ChevronRight, Database, FolderTree, Gauge, Globe, HardDrive, Lock, MemoryStick, Search,
  Settings2, Sparkles, Timer, Trash2, Zap,
} from 'lucide-react'
import SiteEditor from './SiteEditor'
import { useApp } from '../App'
import { api, cmsPresets, errText } from '../api'
import type { ChunkPlan, CMSPreset, Job, JobView, ProbeResult, Retention, RetentionPreview, Source, SysInfo } from '../types'
import { KEEP_DAILY, KEEP_LAST, KEEP_MONTHLY, KEEP_WEEKLY, KEEP_YEARLY } from '../types'
import { Alert, Bar, Button, Field, Modal, Seg, Toggle, useUI } from '../components/ui'
import {
  ageText, bytes, cls, configPresets, isCalendar, kindLabels, newJob, newSource, policy, seconds, sourceLabels, speed,
} from '../util'
import { getLang, locale, t } from '../i18n'

// Same "round" sizes as the agent's planner (internal/chunk).
const SIZES_MB = [8, 16, 24, 32, 48, 64, 96, 128, 192, 256, 384, 512, 768, 1024, 1536, 2048]

export default function JobEditor({ serverId, sys, notes, job, onClose, onSaved }: {
  serverId: string; sys: SysInfo; notes?: Record<string, string>; job: JobView | null; onClose: () => void; onSaved: () => void
}) {
  const { state, go } = useApp()
  const { run, busy } = useUI()
  const [j, setJ] = useState<Job>(() => job ? { ...structuredClone(job), retention: policy(job) } : newJob(state.storages.slice(0, 1).map(s => s.id)))
  const set = (p: Partial<Job>) => setJ(v => ({ ...v, ...p }))
  const [open, setOpen] = useState<string | null>(job ? null : 'new')
  // Telegram takes encrypted copies only (the agent refuses others too).
  const telegram = state.storages.filter(s => s.kind === 'telegram' && j.storages.includes(s.id))
  const toggleStorage = (id: string, on: boolean) => {
    const next = on ? j.storages.filter(x => x !== id) : [...j.storages, id]
    const tg = !on && state.storages.find(s => s.id === id)?.kind === 'telegram'
    set(tg ? { storages: next, encrypt: true } : { storages: next })
  }

  const save = () => run('save', async () => {
    const { nextRun, lastRun, lastSize, cronError, ...clean } = j as JobView
    clean.keep = isCalendar(clean.retention) ? 0 : clean.retention.last
    await api.saveJob(serverId, clean)
    onSaved()
  }, job ? t('Задача сохранена') : t('Задача создана'))

  const addSource = (type: Source['type']) => {
    const s = newSource(type)
    const same = j.sources.filter(x => x.type === type).length
    if (same) s.name = `${s.name}${same + 1}`
    const name = j.name || (type === 'configs' ? t('Конфиги сервера') : type === 'files' || type === 'site' ? t('Сайт') : t('Базы данных'))
    set({ sources: [...j.sources, s], name })
    setOpen(s.id)
  }
  const updSource = (id: string, p: Partial<Source>) => set({ sources: j.sources.map(s => s.id === id ? { ...s, ...p } : s) })

  return (
    <Modal drawer title={job ? t('Задача «{name}»', { name: job.name }) : t('Новая задача')} icon={<div className="job-icon" style={{ width: 34, height: 34 }}><Archive size={17} /></div>} onClose={onClose}
      footer={<>
        <span className="small faint grow">{t('Задача хранится на сервере и выполняется агентом, даже когда панель закрыта.')}</span>
        <Button onClick={onClose}>{t('Отмена')}</Button>
        <Button variant="primary" loading={busy === 'save'} onClick={save}>{job ? t('Сохранить') : t('Создать задачу')}</Button>
      </>}>

      <div className="editor-section">
        <h3><span className="num">1</span> {t('Название')}</h3>
        <input className="input" value={j.name} onChange={e => set({ name: e.target.value })} placeholder={t('Например: Магазин — база и файлы')} autoFocus={!job} />
        <div className="hint small faint mt8">{t('Используется как имя папки в облаке.')}</div>
      </div>

      <div className="editor-section">
        <h3><span className="num">2</span> {t('Что бэкапить')}</h3>
        <div className="col">
          {j.sources.map(s => (
            <SourceEditor key={s.id} serverId={serverId} src={s} open={open === s.id} onToggle={() => setOpen(open === s.id ? null : s.id)}
              onChange={p => updSource(s.id, p)} onRemove={() => set({ sources: j.sources.filter(x => x.id !== s.id) })} />
          ))}
        </div>
        <div className={cls('add-source', j.sources.length > 0 && 'mt12')}>
          <button onClick={() => addSource('mysql')}><Database size={20} /> MySQL / MariaDB</button>
          <button onClick={() => addSource('postgres')}><Database size={20} /> PostgreSQL</button>
          <button onClick={() => addSource('files')}><FolderTree size={20} /> {t('Файлы сайта')}</button>
          <button onClick={() => addSource('configs')}><Settings2 size={20} /> {t('Конфиги сервера')}</button>
          <button onClick={() => addSource('site')}><Globe size={20} /> {t('Сайт на хостинге (FTP)')}</button>
        </div>
      </div>

      <ScheduleSection j={j} set={set} sys={sys} />

      <div className="editor-section">
        <h3><span className="num">4</span> {t('Куда выгружать и сколько хранить')}</h3>
        {state.storages.length === 0 ? (
          <Alert tone="warning">{t('Нет подключённых облаков.')} <a href="#" onClick={e => { e.preventDefault(); onClose(); go({ page: 'clouds' }) }}>{t('Подключить облако')}</a></Alert>
        ) : (
          <div className="grid g2">
            {state.storages.map(s => {
              const on = j.storages.includes(s.id)
              return (
                <label key={s.id} className={cls('kind-tile', on && 'on')} style={{ flexDirection: 'row', alignItems: 'center' }}>
                  <input type="checkbox" checked={on} style={{ accentColor: 'var(--accent)', width: 16, height: 16 }}
                    onChange={() => toggleStorage(s.id, on)} />
                  <div className="grow">
                    <div style={{ fontWeight: 600 }}>{s.name}</div>
                    <div className="small faint">{t(kindLabels[s.kind])} · {s.root}</div>
                  </div>
                </label>
              )
            })}
          </div>
        )}
        {telegram.map(s => notes?.[s.id] && <div key={s.id} className="mt12"><Alert tone="warning">{s.name}: {notes[s.id]}</Alert></div>)}
        {telegram.length > 0 && !telegram.some(s => notes?.[s.id]) && (
          <div className="small faint mt12">{t('Telegram: старые копии удаляются, только если бот может удалять сообщения старше 48 часов — агент проверит это сам через двое суток после первой выгрузки.')}</div>
        )}
        <RetentionSection j={j} set={set} sys={sys} lastSize={job?.lastSize || 0} />
        <div className="grid g2 mt16">
          <Field label={t('Уведомления в Telegram')} hint={state.settings.telegram.enabled ? undefined : t('Telegram не настроен в «Настройках»')}>
            <select className="select" value={j.notify} onChange={e => set({ notify: e.target.value as Job['notify'] })}>
              <option value="errors">{t('Только об ошибках')}</option>
              <option value="always">{t('О каждом запуске')}</option>
              <option value="never">{t('Не уведомлять')}</option>
            </select>
          </Field>
          <Field label={t('Сжатие (gzip)')} hint={j.compression <= 3 ? t('Быстро, меньше нагрузка на CPU') : j.compression >= 8 ? t('Максимум, сильно грузит CPU') : t('Баланс скорости и размера')}>
            <div className="row"><input type="range" min={1} max={9} value={j.compression} onChange={e => set({ compression: +e.target.value })} /><b style={{ width: 16 }}>{j.compression}</b></div>
          </Field>
        </div>
        <div className="mt16">
          <Toggle on={j.encrypt} onChange={v => set({ encrypt: v || telegram.length > 0 })} label={<span className="row" style={{ gap: 6 }}><Lock size={14} /> {t('Шифровать архивы (age, AES-256 / ChaCha20)')}</span>} />
          {telegram.length > 0 && <div className="small faint mt8">{t('Для Telegram шифрование обязательно.')}</div>}
          {j.encrypt && !state.settings.encryptionPassword && <div className="mt8"><Alert tone="warning">{t('Задайте пароль шифрования в «Настройках», иначе бэкап завершится ошибкой.')}</Alert></div>}
          {j.encrypt && state.settings.encryptionPassword && <div className="small faint mt8">{t('Без пароля из «Настроек» копии восстановить невозможно — сохраните его в надёжном месте.')}</div>}
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
  const Icon = isDB ? Database : src.type === 'configs' ? Settings2 : src.type === 'site' ? Globe : FolderTree
  const summary = src.type === 'site'
    ? `${(src.site?.protocol || 'ftp').toUpperCase()} ${src.site?.host || '—'} · ${src.site?.url || ''} · ${[src.site?.db && t('база'), src.site?.files && t('файлы')].filter(Boolean).join(' + ')}`
    : isDB
      ? (src.databases?.length ? src.databases.join(', ') : t('все базы')) + (src.perDatabase ? ' · ' + t('по файлу на базу') : '')
      : src.type === 'configs' ? t('{n} путей (отсутствующие пропускаются)', { n: src.paths?.length || 0 }) : (src.paths || []).join(', ')
  const lines = (a?: string[]) => (a || []).join('\n')
  const parse = (s: string) => s.split('\n').map(x => x.trim()).filter(Boolean)
  const custom = (src.paths || []).filter(p => !configPresets.includes(p))
  const ext = src.type === 'site' ? t('.sql.gz и .tar.gz') : isDB ? (src.perDatabase ? t('__<база>.sql.gz') : '.sql.gz') : '.tar.gz'

  return (
    <div className="source-card">
      <div className="source-head" style={{ cursor: 'pointer' }} onClick={onToggle}>
        {open ? <ChevronDown size={16} className="faint" /> : <ChevronRight size={16} className="faint" />}
        <div className="job-icon" style={{ width: 30, height: 30, borderRadius: 8 }}><Icon size={15} /></div>
        <div className="grow">
          <div style={{ fontWeight: 600 }}>{t(sourceLabels[src.type])} <span className="faint mono" style={{ fontWeight: 400 }}>· {src.name}</span></div>
          <div className="small faint ellipsis">{summary}</div>
        </div>
        <Button size="sm" variant="danger" icon={<Trash2 size={13} />} onClick={e => { e.stopPropagation(); onRemove() }} title={t('Убрать источник')} />
      </div>
      {open && (
        <div className="source-body">
          <Field label={t('Имя в архиве')} hint={t('Файлы будут называться {name}{ext}', { name: src.name || '…', ext })}>
            <input className="input mono" value={src.name} onChange={e => onChange({ name: e.target.value })} />
          </Field>
          {isDB && (
            <>
              <div className="grid g2">
                <Field label={t('Пользователь')} hint={src.type === 'mysql' ? t('Пусто — root через сокет или ~/.my.cnf') : t('Пусто — от системного пользователя postgres')}>
                  <input className="input" value={src.user} onChange={e => onChange({ user: e.target.value })} />
                </Field>
                <Field label={t('Пароль')} hint={t('Передаётся через временный файл, не виден в списке процессов')}>
                  <input className="input" type="password" value={src.password} onChange={e => onChange({ password: e.target.value })} />
                </Field>
              </div>
              <div className="grid g2">
                <Field label={t('Хост')} hint={t('Пусто — локальный сервер')}>
                  <input className="input" value={src.host} onChange={e => onChange({ host: e.target.value })} placeholder="localhost" />
                </Field>
                <Field label={t('Порт')}>
                  <input className="input" type="number" value={src.port || ''} onChange={e => onChange({ port: +e.target.value })} placeholder={src.type === 'mysql' ? '3306' : '5432'} />
                </Field>
              </div>
              <Field label={t('Базы данных')} hint={t('Пусто — все базы (системные пропускаются). Нажмите «Проверить», чтобы выбрать из списка.')}>
                <input className="input mono" value={(src.databases || []).join(', ')} onChange={e => onChange({ databases: e.target.value.split(',').map(x => x.trim()).filter(Boolean) })} placeholder={t('все базы')} />
              </Field>
              <Toggle on={!!src.perDatabase} onChange={v => onChange({ perDatabase: v })} label={t('Отдельный файл на каждую базу (удобнее восстанавливать одну базу)')} />
              <Field label={t('Дополнительные параметры')} hint={src.type === 'mysql' ? t('Например: --events --ignore-table=shop.sessions') : t('Например: --exclude-table-data=logs')}>
                <input className="input mono" value={src.extraArgs || ''} onChange={e => onChange({ extraArgs: e.target.value })} />
              </Field>
            </>
          )}
          {src.type === 'files' && (
            <>
              <div className="grid g2">
                <Field label={t('Папки и файлы (по одному в строке)')}>
                  <textarea className="textarea" rows={4} value={lines(src.paths)} onChange={e => onChange({ paths: parse(e.target.value) })} />
                </Field>
                <Field label={t('Исключить (шаблоны tar)')} hint={t('Например: */cache/*, *.log, node_modules')}>
                  <textarea className="textarea" rows={4} value={lines(src.excludes)} onChange={e => onChange({ excludes: parse(e.target.value) })} />
                </Field>
              </div>
              <CMSExcludes on={!!src.cmsExcludes} onChange={v => onChange({ cmsExcludes: v })} />
            </>
          )}
          {src.type === 'site' && <SiteEditor serverId={serverId} src={src} onChange={onChange} />}
          {src.type === 'configs' && (
            <>
              <div className="small muted">{t('Отмеченные пути попадут в архив, если существуют на сервере — лишние можно не снимать.')}</div>
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
              <Field label={t('Свои пути (по одному в строке)')}>
                <textarea className="textarea" rows={2} value={custom.join('\n')} onChange={e => onChange({ paths: [...(src.paths || []).filter(p => configPresets.includes(p)), ...parse(e.target.value)] })} placeholder="/opt/app/.env" />
              </Field>
            </>
          )}
          {src.type !== 'site' && <div className="row">
            <Button size="sm" icon={<Search size={13} />} loading={probing} onClick={doProbe}>{t('Проверить на сервере')}</Button>
            {probe && !probe.ok && <span className="small" style={{ color: 'var(--danger)' }}>{probe.error}</span>}
            {probe?.ok && probe.paths && <span className="small" style={{ color: 'var(--success)' }}>{t('Найдено: {n} из {total}', { n: probe.paths.length, total: src.paths?.length || 0 })}</span>}
          </div>}
          {probe?.ok && probe.databases && (
            <div className="card card-pad" style={{ background: 'var(--surface-2)' }}>
              <div className="small muted" style={{ marginBottom: 8 }}>{t('Подключение работает. Отметьте базы (ничего не отмечено — все):')}</div>
              <div className="grid g3" style={{ gap: 6 }}>
                {probe.databases.map(d => {
                  const on = src.databases?.includes(d)
                  return <label key={d} className="check small mono"><input type="checkbox" checked={!!on} onChange={() => onChange({ databases: on ? src.databases!.filter(x => x !== d) : [...(src.databases || []), d] })} />{d}</label>
                })}
              </div>
            </div>
          )}
          {probe?.ok && probe.paths && src.type === 'configs' && <div className="small faint mono">{probe.paths.join('  ')}</div>}
          {probe?.ok && src.type === 'files' && (
            <div className="small muted">
              {probe.cms?.length
                ? <>{t('Найдены сайты:')} {probe.cms.map(c => <span key={c.path} className="chip mono" style={{ marginRight: 6 }}>{t(c.name)} · {c.path}</span>)}</>
                : t('Сайтов известных CMS не найдено — исключаются только шаблоны выше.')}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

/** Toggle for the CMS presets with the list of what they leave out. */
export function CMSExcludes({ on, onChange }: { on: boolean; onChange: (v: boolean) => void }) {
  const [presets, setPresets] = useState<CMSPreset[]>([])
  const [show, setShow] = useState(false)
  useEffect(() => { cmsPresets().then(setPresets) }, [])
  return (
    <div>
      <div className="row wrap" style={{ gap: 10 }}>
        <Toggle on={on} onChange={onChange} label={<><Sparkles size={13} /> {t('Не архивировать кэш и резервные копии CMS')}</>} />
        <button className="btn ghost sm" type="button" onClick={() => setShow(!show)}>
          {show ? <ChevronDown size={14} /> : <ChevronRight size={14} />} {t('Что исключается')}
        </button>
      </div>
      <div className="small faint mt8">
        {t('Агент сам находит сайты в указанных папках (до {n} уровней вглубь) и пропускает то, что CMS пересоздаёт сама, и её собственные бэкапы — они уже сжаты и обычно занимают больше всего места: {list}.', { n: 4, list: presets.map(p => t(p.name)).join(', ') })}
      </div>
      {show && (
        <div className="card card-pad mt8" style={{ background: 'var(--surface-2)' }}>
          <div className="kv">
            {presets.map(p => <Fragment key={p.id}><span className="k">{t(p.name)}</span><span className="mono small">{p.excludes.join('  ')}</span></Fragment>)}
          </div>
        </div>
      )}
    </div>
  )
}

const RET_PRESETS: [string, Retention][] = [
  ['Месяц по неделям, год по месяцам', { last: 0, daily: 0, weekly: 4, monthly: 12, yearly: 0 }],
  ['…и ещё по копии за 3 года', { last: 0, daily: 0, weekly: 4, monthly: 12, yearly: 3 }],
  ['Неделя по дням, месяц по неделям, полгода по месяцам', { last: 0, daily: 7, weekly: 4, monthly: 6, yearly: 0 }],
]

const RET_FIELDS: [keyof Retention, string, string][] = [
  ['last', 'Последних', 'любых, подряд'],
  ['daily', 'По дням', 'одна за день'],
  ['weekly', 'По неделям', 'одна за неделю'],
  ['monthly', 'По месяцам', 'одна за месяц'],
  ['yearly', 'По годам', 'одна за год'],
]

function reasonText(r: number): string {
  const out: string[] = []
  if (r & KEEP_LAST) out.push(t('последняя'))
  if (r & KEEP_DAILY) out.push(t('дневная'))
  if (r & KEEP_WEEKLY) out.push(t('недельная'))
  if (r & KEEP_MONTHLY) out.push(t('месячная'))
  if (r & KEEP_YEARLY) out.push(t('годовая'))
  return out.join(', ')
}

function RetentionSection({ j, set, sys, lastSize }: { j: Job; set: (p: Partial<Job>) => void; sys: SysInfo; lastSize: number }) {
  const r = j.retention
  const [calendar, setCalendar] = useState(isCalendar(r))
  const upd = (p: Partial<Retention>) => set({ retention: { ...r, ...p } })
  const [prev, setPrev] = useState<RetentionPreview | null>(null)
  useEffect(() => {
    const timer = setTimeout(() => api.retentionPreview(j.schedule, sys.tzOffset, r).then(setPrev).catch(() => setPrev(null)), 250)
    return () => clearTimeout(timer)
  }, [JSON.stringify([r, j.schedule])])
  const mode = (m: 'simple' | 'calendar') => {
    setCalendar(m === 'calendar')
    if (m === 'calendar' && !isCalendar(r)) upd(RET_PRESETS[0][1])
    if (m === 'simple') set({ retention: { last: r.last || 7, daily: 0, weekly: 0, monthly: 0, yearly: 0 } })
  }
  const ages = prev?.ages || []
  const count = ages.length
  return (
    <div className="mt16">
      <div className="row wrap" style={{ gap: 14 }}>
        <span style={{ fontWeight: 600 }}><CalendarRange size={14} style={{ verticalAlign: -2 }} /> {t('Сколько хранить')}</span>
        <Seg value={calendar ? 'calendar' : 'simple'} onChange={mode} options={[['simple', t('Последние N')], ['calendar', t('По календарю')]]} />
      </div>
      {!calendar ? (
        <div className="grid g3 mt12">
          <Field label={t('Хранить последних копий')} hint={t('Более старые удаляются после успешного бэкапа. 0 — не удалять.')}>
            <input className="input" type="number" min={0} value={r.last} onChange={e => upd({ last: Math.max(0, +e.target.value) })} />
          </Field>
        </div>
      ) : (
        <>
          <div className="small muted mt8" style={{ maxWidth: 680 }}>
            {t('Из каждого периода остаётся самая новая копия. Копия, подходящая под несколько правил, хранится один раз. Удаляется только то, что не нужно ни одному правилу.')}
          </div>
          <div className="row wrap mt12" style={{ gap: 12, alignItems: 'flex-start' }}>
            {RET_FIELDS.map(([k, label, hint]) => (
              <Field key={k} label={t(label)} hint={t(hint)}>
                <input className="input" type="number" min={0} style={{ width: 110 }} value={r[k]} onChange={e => upd({ [k]: Math.max(0, +e.target.value) })} />
              </Field>
            ))}
          </div>
          <div className="row wrap mt8" style={{ gap: 6 }}>
            {RET_PRESETS.map(([label, p]) => <button key={label} type="button" className="btn ghost sm" onClick={() => upd(p)}>{t(label)}</button>)}
          </div>
        </>
      )}
      {count > 0 && (
        <div className="card card-pad mt12" style={{ background: 'var(--surface-2)' }}>
          <div className="small">
            {t('В облаке будет храниться копий: {n}, самая старая — {age} назад.', { n: count, age: ageText(ages[count - 1]) })}
            {lastSize > 0 && <> {t('При размере копии {size} это около {total}.', { size: bytes(lastSize), total: bytes(lastSize * count) })}</>}
            {prev?.assumed && <span className="faint"> {t('(расчёт для запуска раз в день)')}</span>}
          </div>
          {calendar && (
            <div className="row wrap mt8" style={{ gap: 4 }}>
              {ages.map((a, i) => <span key={i} className="chip" title={reasonText(prev!.reasons![i])}>{ageText(a)}</span>)}
            </div>
          )}
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
    const timer = setTimeout(() => api.nextRuns(s, sys.tzOffset, 3).then(r => { setNext(r); setErr('') }).catch(e => { setNext([]); setErr(errText(e)) }), 250)
    return () => clearTimeout(timer)
  }, [JSON.stringify(s)])
  const tz = `UTC${sys.tzOffset >= 0 ? '+' : ''}${sys.tzOffset / 3600}`
  const fmt = (iso: string) => {
    // Show the server-local wall time as written in the ISO string.
    const d = new Date(iso.slice(0, 19))
    return `${d.toLocaleDateString(locale(), { weekday: 'short', day: 'numeric', month: 'short' })}, ${iso.slice(11, 16)}`
  }
  return (
    <div className="editor-section">
      <h3><span className="num">3</span> {t('Расписание')}</h3>
      <div className="row wrap" style={{ gap: 14 }}>
        <Toggle on={s.enabled} onChange={v => upd({ enabled: v })} label={s.enabled ? t('По расписанию') : t('Только вручную')} />
        {s.enabled && <Seg value={s.mode} onChange={m => upd({ mode: m })} options={[['daily', t('Каждый день')], ['weekly', t('По дням недели')], ['monthly', t('Раз в месяц')], ['interval', t('Каждые N часов')], ['cron', 'Cron']]} />}
      </div>
      {s.enabled && (
        <div className="row wrap mt16" style={{ gap: 16, alignItems: 'flex-end' }}>
          {s.mode !== 'cron' && (
            <Field label={s.mode === 'interval' ? t('Начиная с') : t('Время')}>
              <input className="input" type="time" value={s.time} onChange={e => upd({ time: e.target.value })} style={{ width: 130 }} />
            </Field>
          )}
          {s.mode === 'weekly' && (
            <Field label={t('Дни недели')}>
              <div className="days">
                {days.map(([d, l]) => {
                  const on = s.weekdays?.includes(d)
                  return <button key={d} className={cls(on && 'on')} onClick={() => upd({ weekdays: on ? s.weekdays.filter(x => x !== d) : [...(s.weekdays || []), d] })}>{t(l)}</button>
                })}
              </div>
            </Field>
          )}
          {s.mode === 'monthly' && (
            <Field label={t('Число месяца')}>
              <select className="select" value={s.monthDay} onChange={e => upd({ monthDay: +e.target.value })} style={{ width: 100 }}>
                {Array.from({ length: 28 }, (_, i) => <option key={i + 1} value={i + 1}>{i + 1}</option>)}
              </select>
            </Field>
          )}
          {s.mode === 'interval' && (
            <Field label={t('Интервал')}>
              <select className="select" value={s.everyHours} onChange={e => upd({ everyHours: +e.target.value })} style={{ width: 150 }}>
                {[1, 2, 3, 4, 6, 8, 12].map(h => <option key={h} value={h}>{t('каждые {n} ч', { n: h })}</option>)}
              </select>
            </Field>
          )}
          {s.mode === 'cron' && (
            <Field label={t('Выражение cron (мин час день месяц день_недели)')} className="grow">
              <input className="input mono" value={s.cron} onChange={e => upd({ cron: e.target.value })} placeholder="30 2 * * 1-5" />
            </Field>
          )}
        </div>
      )}
      {s.enabled && (
        <div className="small mt12" style={{ color: err ? 'var(--danger)' : 'var(--text-2)' }}>
          {err || <>{t('Ближайшие запуски:')} <b>{next.map(fmt).join(' · ')}</b> <span className="faint">({t('время сервера, {tz}', { tz: `${sys.tz} ${tz}` })})</span></>}
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
  const key = JSON.stringify([c, j.storages, getLang()])

  const load = () => {
    const n = ++seq.current
    api.plan(serverId, j, getLang()).then(p => { if (n === seq.current) setPlan(p) }).catch(() => {})
  }
  useEffect(() => { const timer = setTimeout(load, 300); return () => clearTimeout(timer) }, [key])

  const speedTest = async () => {
    setTesting(true)
    try {
      for (const sid of j.storages) {
        const name = state.storages.find(s => s.id === sid)?.name || sid
        const r = await api.speedTest(serverId, sid)
        if (r.ok) toast('success', t('{name}: {speed} с сервера', { name, speed: speed(r.speedBps) }))
        if (r.ok) r.notes?.forEach(n => toast('info', `${name}: ${n}`))
        else toast('error', `${name}: ${r.error}`)
      }
      load()
    } catch (e) { toast('error', errText(e)) } finally { setTesting(false) }
  }

  const sizeIdx = Math.max(0, SIZES_MB.findIndex(s => s >= c.sizeMB))
  const memShare = plan && plan.memAvailable ? plan.peakMemory / plan.memAvailable : 0

  return (
    <div className="editor-section">
      <h3><span className="num">5</span> {t('Размер кусков')} <span className="badge accent" style={{ marginLeft: 4 }}><Zap size={11} /> {t('умная настройка')}</span></h3>
      <div className="small muted" style={{ maxWidth: 680 }}>
        {t('Архив не сохраняется на диск сервера: он режется на куски в памяти и сразу уходит в облако. Пока один кусок загружается, следующий уже заполняется — поэтому в памяти одновременно два куска. Если кусок не загрузился, он отправляется заново целиком.')}
      </div>
      <div className="row wrap mt16" style={{ gap: 14 }}>
        <Seg value={c.mode} onChange={m => upd({ mode: m })} options={[['auto', t('Автоматически')], ['manual', t('Вручную')]]} />
        <span className="grow" />
        <Button size="sm" icon={<Gauge size={14} />} loading={testing} disabled={!j.storages.length} onClick={speedTest}>{t('Измерить скорость с сервера')}</Button>
      </div>

      <div className="grid g3 mt16">
        {c.mode === 'auto' ? (
          <>
            <Field label={<>{t('Можно занять памяти:')} <b>{c.maxMemoryPercent}%</b> {t('свободной')}</>} hint={t('Меньше — безопаснее для сайтов, больше — меньше частей')}>
              <input type="range" min={5} max={50} step={5} value={c.maxMemoryPercent} onChange={e => upd({ maxMemoryPercent: +e.target.value })} />
            </Field>
            <Field label={<>{t('Загрузка одного куска:')} <b>~{seconds(c.targetSeconds)}</b></>} hint={t('Сколько данных не жалко переотправить при обрыве')}>
              <input type="range" min={15} max={300} step={15} value={c.targetSeconds} onChange={e => upd({ targetSeconds: +e.target.value })} />
            </Field>
          </>
        ) : (
          <Field label={<>{t('Размер куска:')} <b>{bytes(SIZES_MB[sizeIdx] * 1048576)}</b></>} hint={`${bytes(8 << 20)} — ${bytes(2 << 30)}`}>
            <input type="range" min={0} max={SIZES_MB.length - 1} value={sizeIdx} onChange={e => upd({ sizeMB: SIZES_MB[+e.target.value] })} />
          </Field>
        )}
        <Field label={t('Где держать кусок')} hint={c.buffer === 'auto' ? t('В памяти; на диске — только если RAM почти нет') : c.buffer === 'disk' ? t('Временные файлы в /var/tmp, удаляются сразу') : t('Только оперативная память')}>
          <Seg value={c.buffer} onChange={b => upd({ buffer: b })} options={[['auto', t('Авто')], ['memory', t('Память')], ['disk', t('Диск')]]} />
        </Field>
      </div>

      {plan ? (
        <div className="plan">
          <div>
            <div className="small faint">{c.mode === 'auto' ? t('Рекомендуемый размер') : t('Размер куска')}</div>
            <div className="plan-size">{bytes(plan.size)}</div>
            <div className="plan-metrics">
              <div><div className="k"><MemoryStick size={11} /> {t('Пик памяти')}</div><div className="v">{bytes(plan.peakMemory)}</div></div>
              <div><div className="k"><HardDrive size={11} /> {t('Диск')}</div><div className="v">{plan.peakDisk ? bytes(plan.peakDisk) : t('не нужен')}</div></div>
              <div><div className="k"><Timer size={11} /> {t('Один кусок')}</div><div className="v">~{seconds(plan.partSeconds)}</div></div>
              <div><div className="k"><Archive size={11} /> {t('Частей')}</div><div className="v">{plan.parts ? `~${plan.parts}` : '—'}</div></div>
            </div>
          </div>
          <div>
            <ul>{plan.reasons.map((r, i) => <li key={i}>{r}</li>)}</ul>
            <div className="mem-viz">
              <div className="row small"><span className="grow muted">{t('Свободная память сервера: {mem}', { mem: bytes(plan.memAvailable) })}</span><span className="faint">{t('бэкап займёт {p}%', { p: (memShare * 100).toFixed(memShare < 0.01 ? 1 : 0) })}</span></div>
              <div className="mt8"><Bar value={memShare} warn={memShare > 0.4} /></div>
              <div className="row small faint mt8" style={{ gap: 14 }}>
                <span>{t('Скорость в облако:')} {plan.speedMeasured ? <b>{speed(plan.speedBps)}</b> : t('не измерена')}</span>
                {plan.expectedSize > 0 && <span>{t('Прошлый архив:')} {bytes(plan.expectedSize)}</span>}
                {plan.maxFileSize > 0 && <span>{t('Лимит файла в облаке:')} {bytes(plan.maxFileSize)}</span>}
              </div>
            </div>
            {plan.warnings.length > 0 && <div className="col mt12" style={{ gap: 6 }}>{plan.warnings.map((w, i) => <Alert key={i} tone="warning">{w}</Alert>)}</div>}
            <div className="small faint mt12">{t('Во время бэкапа агент следит за памятью: если её станет мало, следующие куски автоматически уменьшатся; если кусок не проходит по сети, он делится пополам.')}</div>
          </div>
        </div>
      ) : <div className="mt16"><Bar value={0} indeterminate /></div>}
    </div>
  )
}
