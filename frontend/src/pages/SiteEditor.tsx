import { useState } from 'react'
import { ChevronDown, ChevronRight, Search, ShieldCheck } from 'lucide-react'
import { api, errText } from '../api'
import type { ProbeResult, SiteSource, Source } from '../types'
import { Alert, Bar, Button, Field, Seg, Toggle } from '../components/ui'
import { bytes } from '../util'
import { t } from '../i18n'

const cmsExcludes: Record<string, string> = {
  bitrix: 'bitrix/cache, managed_cache, stack_cache, html_pages, backup, upload/tmp, upload/resize_cache',
  wordpress: 'wp-content/cache, upgrade, папки плагинов резервного копирования',
}

// Settings of a site on shared hosting: FTP access, site address, what to save.
export default function SiteEditor({ serverId, src, onChange }: { serverId: string; src: Source; onChange: (p: Partial<Source>) => void }) {
  const s = src.site!
  const set = (p: Partial<SiteSource>) => onChange({ site: { ...s, ...p } })
  const [probe, setProbe] = useState<ProbeResult | null>(null)
  const [probing, setProbing] = useState(false)
  const [manualDB, setManualDB] = useState(!!s.dbName)
  const doProbe = async () => {
    setProbing(true); setProbe(null)
    try { setProbe(await api.probe(serverId, src)) } catch (e) { setProbe({ ok: false, error: errText(e) }) } finally { setProbing(false) }
  }
  const lines = (a?: string[]) => (a || []).join('\n')
  const parse = (v: string) => v.split('\n').map(x => x.trim()).filter(Boolean)
  const info = probe?.site
  const cmsName = (c: string) => c === 'bitrix' ? t('1С-Битрикс') : c === 'wordpress' ? 'WordPress' : t('не определена')

  return (
    <>
      <Alert icon={<ShieldCheck size={17} />}>
        {t('Для сайтов на обычном хостинге без SSH. Агент загружает по FTP одноразовый PHP-скрипт со случайным именем, получает через него базу и файлы порциями (запросы подписаны, ответы зашифрованы) и сразу удаляет скрипт. Нужен PHP 7.1+.')}
      </Alert>
      <div className="row wrap" style={{ gap: 14 }}>
        <Field label={t('Протокол')}>
          <Seg value={s.protocol} onChange={v => set({ protocol: v })} options={[['ftp', 'FTP'], ['ftps', 'FTPS'], ['sftp', 'SFTP']]} />
        </Field>
        <Field label={t('Сервер FTP')} className="grow">
          <input className="input" value={s.host} onChange={e => set({ host: e.target.value })} placeholder="ftp.hosting.ru" />
        </Field>
        <Field label={t('Порт')} hint={s.protocol === 'ftps' ? t('990 — неявный TLS') : undefined}>
          <input className="input" type="number" style={{ width: 100 }} value={s.port || ''} onChange={e => set({ port: +e.target.value })} placeholder={s.protocol === 'sftp' ? '22' : '21'} />
        </Field>
      </div>
      <div className="grid g2">
        <Field label={t('Логин FTP')}>
          <input className="input" value={s.user} onChange={e => set({ user: e.target.value })} />
        </Field>
        <Field label={t('Пароль FTP')}>
          <input className="input" type="password" value={s.password || ''} onChange={e => set({ password: e.target.value })} />
        </Field>
      </div>
      <div className="grid g2">
        <Field label={t('Папка сайта на FTP')} hint={t('Где лежит index.php сайта')}>
          <input className="input mono" value={s.root} onChange={e => set({ root: e.target.value })} placeholder="/public_html" />
        </Field>
        <Field label={t('Адрес сайта')} hint={t('Эта же папка, открытая в браузере')}>
          <input className="input mono" value={s.url} onChange={e => set({ url: e.target.value })} placeholder="https://site.ru" />
        </Field>
      </div>
      <div className="row wrap" style={{ gap: 24 }}>
        <Toggle on={s.db} onChange={v => set({ db: v })} label={t('База данных')} />
        <Toggle on={s.files} onChange={v => set({ files: v })} label={t('Файлы сайта')} />
        <label className="check small"><input type="checkbox" checked={!!s.insecure} onChange={e => set({ insecure: e.target.checked })} /> {t('Не проверять сертификат (FTPS / HTTPS)')}</label>
      </div>
      <div className="grid g2">
        <Field label="CMS" hint={t('Определяет, где взять доступ к базе и что не архивировать')}>
          <select className="select" value={s.cms} onChange={e => set({ cms: e.target.value as SiteSource['cms'] })}>
            <option value="auto">{t('Определить автоматически')}</option>
            <option value="bitrix">{t('1С-Битрикс')}</option>
            <option value="wordpress">WordPress</option>
            <option value="none">{t('Другая / без CMS')}</option>
          </select>
        </Field>
        <Field label={t('Исключить (шаблоны, по одному в строке)')} hint={t('Например: *.log, upload/tmp')}>
          <textarea className="textarea" rows={2} value={lines(src.excludes)} onChange={e => onChange({ excludes: parse(e.target.value) })} />
        </Field>
      </div>
      {s.files && (
        <Toggle on={s.cmsExcludes} onChange={v => set({ cmsExcludes: v })}
          label={<>{t('Не архивировать кэш и резервные копии CMS')} <span className="faint small">({cmsExcludes[s.cms] ? t(cmsExcludes[s.cms]) : t('для Битрикса и WordPress')})</span></>} />
      )}
      {s.db && (
        <div>
          <button className="btn ghost sm" onClick={() => setManualDB(!manualDB)}>
            {manualDB ? <ChevronDown size={14} /> : <ChevronRight size={14} />} {t('Указать доступ к базе вручную')}
          </button>
          {manualDB && (
            <div className="grid g2 mt8">
              <Field label={t('Хост базы')}><input className="input" value={s.dbHost || ''} onChange={e => set({ dbHost: e.target.value })} placeholder="localhost" /></Field>
              <Field label={t('Имя базы')} hint={t('Пусто — взять из настроек CMS')}><input className="input" value={s.dbName || ''} onChange={e => set({ dbName: e.target.value })} /></Field>
              <Field label={t('Пользователь')}><input className="input" value={s.dbUser || ''} onChange={e => set({ dbUser: e.target.value })} /></Field>
              <Field label={t('Пароль')}><input className="input" type="password" value={s.dbPassword || ''} onChange={e => set({ dbPassword: e.target.value })} /></Field>
            </div>
          )}
        </div>
      )}
      <div className="row">
        <Button size="sm" icon={<Search size={13} />} loading={probing} onClick={doProbe} disabled={!s.host || !s.url}>{t('Проверить хостинг')}</Button>
        <span className="small faint">{t('Скрипт загрузится, ответит и сразу удалится')}</span>
      </div>
      {probing && <Bar value={0} indeterminate />}
      {probe && !probe.ok && <Alert tone="danger"><span className="selectable">{probe.error}</span></Alert>}
      {info && (
        <div className="card card-pad" style={{ background: 'var(--surface-2)' }}>
          <div className="kv">
            <span className="k">PHP</span><span>{info.php} ({info.sapi})</span>
            <span className="k">CMS</span><span>{cmsName(info.cms)}</span>
            <span className="k">{t('Лимиты')}</span><span>{t('время {time}, память {mem}', { time: info.timeLimit ? t('{s} с', { s: info.timeLimit }) : t('без ограничения'), mem: info.memoryLimit > 0 ? bytes(info.memoryLimit) : t('без ограничения') })}</span>
            {info.diskFree > 0 && <><span className="k">{t('Свободно на хостинге')}</span><span>{bytes(info.diskFree)}</span></>}
            {info.db && <><span className="k">{t('База')}</span><span>{info.db.error
              ? <span style={{ color: 'var(--danger)' }}>{info.db.name}: {info.db.error}</span>
              : <>{info.db.name} · {t('{n} табл.', { n: info.db.tables })} · {bytes(info.db.size)} · MySQL {info.db.server} <span className="faint">({t('из {file}', { file: info.db.from })})</span></>}</span></>}
            <span className="k">{t('В папке')}</span><span className="mono small">{info.top.slice(0, 14).join('  ')}{info.top.length > 14 ? '  …' : ''}</span>
          </div>
          {info.warnings?.map((w, i) => <div key={i} className="mt8"><Alert tone="warning">{w}</Alert></div>)}
          {!info.warnings?.length && <div className="mt8"><Alert tone="success">{t('Хостинг подходит, скрипт уже удалён.')}</Alert></div>}
        </div>
      )}
    </>
  )
}
