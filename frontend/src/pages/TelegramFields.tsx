import { useState, type ReactNode } from 'react'
import { AlertTriangle, CheckCircle2, ExternalLink, FolderOpen, Search, ServerCog, Stethoscope, XCircle } from 'lucide-react'
import { api } from '../api'
import type { Storage, TelegramChat, TelegramServer, TgServerCheck } from '../types'
import { Alert, Bar, Button, Field, useUI } from '../components/ui'
import { bytes, speed } from '../util'
import { t } from '../i18n'

/** Telegram storage: own Bot API server (check and install over SSH), bot and channel. */
export default function TelegramFields({ s, set, editing, onInstalled }: {
  s: Storage; set: (p: Partial<Storage>) => void; editing: boolean; onInstalled: (saved: Storage) => void
}) {
  const { run, busy, confirm } = useUI()
  const ts: TelegramServer = s.tgServer || { host: '' }
  const setTs = (p: Partial<TelegramServer>) => set({ tgServer: { ...ts, ...p } })
  const [check, setCheck] = useState<TgServerCheck | null>(null)
  const [log, setLog] = useState('')
  const [chats, setChats] = useState<TelegramChat[] | null>(null)
  const secretHint = editing ? t('Оставьте пустым, чтобы не менять') : undefined
  const link = (url: string, text: string) => <a href="#" onClick={e => { e.preventDefault(); api.openURL(url) }}>{text} <ExternalLink size={11} /></a>

  const doCheck = () => run('tgcheck', async () => { setCheck(null); setCheck(await api.checkTelegramServer(s)) })
  const doInstall = async () => {
    const ok = await confirm(<>
      {t('На сервер {host} будет установлено:', { host: ts.host })}
      <ul className="steps">
        <li>{t('Docker (или podman, если он уже есть) и контейнер telegram-bot-api из образа aiogram — служба backupper-tg-bot-api, слушает только 127.0.0.1;')}</li>
        <li>{t('пользователь backupper-tg без shell: агенты входят под ним по ключу, который создаст панель, и могут только обращаться к Bot API и забирать скачанные файлы;')}</li>
        <li>{t('ежечасная очистка забытых скачанных файлов.')}</li>
      </ul>
      {t('Бот будет отключён от облачного Bot API (logOut) и дальше работает только через ваш сервер.')}
    </>, t('Установить'))
    if (!ok) return
    setLog('')
    await run('tginstall', async () => {
      const r = await api.installTelegramServer(s)
      setLog(r.log)
      if (!r.ok) throw new Error(r.error)
      onInstalled(r.storage)
    }, t('Bot API установлен, облако сохранено'))
  }
  const findChats = () => run('tgchats', async () => { setChats(null); setChats(await api.telegramChats(s)) })

  return (
    <div className="col mt16" style={{ gap: 14 }}>
      <Alert>
        <ol className="steps">
          <li><span>{t('Создайте бота у')} {link('https://t.me/BotFather', '@BotFather')} {t('(команда /newbot) — отдельного, не того, что шлёт уведомления.')}</span></li>
          <li><span>{t('Получите api_id и api_hash на')} {link('https://my.telegram.org/apps', 'my.telegram.org')} → API development tools.</span></li>
          <li>{t('Укажите свой сервер для Bot API, нажмите «Проверить сервер», затем «Установить Bot API».')}</li>
          <li>{t('Добавьте бота администратором канала с правами «Публикация», «Редактирование» и «Удаление сообщений», затем нажмите «Найти канал».')}</li>
        </ol>
      </Alert>

      <div className="tg-step"><ServerCog size={15} /> {t('Сервер Bot API')}</div>
      <div className="grid g3">
        <Field label={t('Адрес сервера')}>
          <input className="input mono" value={ts.host} onChange={e => setTs({ host: e.target.value.trim(), hostKey: '' })} placeholder="203.0.113.10" />
        </Field>
        <Field label={t('SSH-порт')}>
          <input className="input" type="number" value={ts.port || 22} onChange={e => setTs({ port: +e.target.value || 22 })} />
        </Field>
        <Field label={t('Пользователь')} hint={t('root или пользователь с sudo без пароля')}>
          <input className="input" value={ts.adminUser ?? 'root'} onChange={e => setTs({ adminUser: e.target.value.trim() })} />
        </Field>
      </div>
      <div className="grid g2">
        <Field label={t('Приватный ключ')} hint={t('Пусто — ключи из ~/.ssh и ssh-agent')}>
          <div className="input-group">
            <input className="input mono" value={ts.adminKeyPath || ''} onChange={e => setTs({ adminKeyPath: e.target.value })} placeholder="C:\Users\...\.ssh\id_ed25519" />
            <Button icon={<FolderOpen size={16} />} onClick={async () => { const p = await api.pickKeyFile(); if (p) setTs({ adminKeyPath: p }) }} title={t('Выбрать файл')} />
          </div>
        </Field>
        <Field label={t('Пароль ключа')} hint={secretHint || t('Только если ключ защищён паролем')}>
          <input className="input" type="password" value={ts.adminPassphrase || ''} onChange={e => setTs({ adminPassphrase: e.target.value })} />
        </Field>
      </div>
      <div className="row wrap" style={{ gap: 10 }}>
        <Button icon={<Stethoscope size={15} />} loading={busy === 'tgcheck'} disabled={!ts.host} onClick={doCheck}>{t('Проверить сервер')}</Button>
        <span className="small faint">{t('Только чтение: доступ, ресурсы, связь с Telegram и скорость скачивания с него.')}</span>
      </div>
      {busy === 'tgcheck' && <Bar value={0} indeterminate />}
      {check && <CheckView c={check} />}

      <div className="tg-step"><CheckCircle2 size={15} /> {t('Бот')}</div>
      <div className="grid g3">
        <Field label={t('Токен бота')} hint={secretHint}>
          <input className="input mono" type="password" value={s.token} onChange={e => set({ token: e.target.value.trim() })} placeholder={editing ? '••••••••' : '123456:AA…'} />
        </Field>
        <Field label="api_id">
          <input className="input mono" value={ts.apiId || ''} onChange={e => setTs({ apiId: e.target.value.trim() })} />
        </Field>
        <Field label="api_hash" hint={secretHint}>
          <input className="input mono" type="password" value={ts.apiHash || ''} onChange={e => setTs({ apiHash: e.target.value.trim() })} />
        </Field>
      </div>
      <div className="row wrap" style={{ gap: 10 }}>
        <Button variant={ts.installed ? undefined : 'primary'} icon={<ServerCog size={15} />} loading={busy === 'tginstall'} disabled={!ts.host} onClick={doInstall}>
          {ts.installed ? t('Переустановить Bot API') : t('Установить Bot API')}
        </Button>
        {ts.installed && <span className="small faint">{t('Установлен {date}', { date: new Date(ts.installed).toLocaleString() })}</span>}
      </div>
      {busy === 'tginstall' && <><Bar value={0} indeterminate /><div className="small faint">{t('Установка занимает несколько минут: ставится Docker и скачивается образ.')}</div></>}
      {log && <div className="log" style={{ whiteSpace: 'pre-wrap' }}>{log}</div>}

      <div className="tg-step"><Search size={15} /> {t('Канал')}</div>
      <Field label={t('ID канала')} hint={t('-100… для приватного канала или @имя публичного')}>
        <div className="input-group">
          <input className="input mono" value={s.chatId || ''} onChange={e => set({ chatId: e.target.value.trim() })} placeholder="-1001234567890" />
          <Button icon={<Search size={15} />} loading={busy === 'tgchats'} disabled={!ts.installed} onClick={findChats}>{t('Найти канал')}</Button>
        </div>
      </Field>
      {chats && (chats.length === 0
        ? <Alert tone="warning">{t('Бот пока не видит каналов. Добавьте его администратором (или снимите и добавьте снова) и повторите поиск.')}</Alert>
        : <div className="row wrap" style={{ gap: 8 }}>
          {chats.map(c => <button key={c.id} className="chip" style={{ cursor: 'pointer' }} onClick={() => set({ chatId: String(c.id) })}>{c.title} · <span className="mono">{c.id}</span></button>)}
        </div>)}

      <Alert>
        {t('Копии в Telegram всегда шифруются. Части до 2000 МБ отправляются альбомами с подписью «часть N из M» и хэштегами #сервер #задача #dГГГГММДД; manifest.json приходит последним — без него копия неполная. Оглавление каждого сервера закреплено в канале.')}{' '}
        {t('Старые копии удаляются, только если Telegram разрешает боту удалять сообщения старше 48 часов: это проверяется само через двое суток, и если нельзя — ротация для этого облака отключается.')}
      </Alert>
    </div>
  )
}

function CheckView({ c }: { c: TgServerCheck }) {
  const line = (state: 'ok' | 'warn' | 'bad', text: ReactNode) => (
    <div className="row" style={{ gap: 8 }}>
      {state === 'ok' ? <CheckCircle2 size={15} color="var(--success)" /> : state === 'warn' ? <AlertTriangle size={15} color="var(--warning)" /> : <XCircle size={15} color="var(--danger)" />}
      <span>{text}</span>
    </div>
  )
  if (!c.os && c.error) return <Alert tone="danger"><span className="selectable">{c.error}</span></Alert>
  return (
    <div className="card card-pad col" style={{ gap: 6 }}>
      {line(c.systemd ? 'ok' : 'bad', `${c.os} · ${c.arch}${c.systemd ? ' · systemd' : ''}`)}
      {line(c.root || c.sudo ? 'ok' : 'bad', c.root ? t('вход под root') : c.sudo ? t('sudo без пароля') : t('нет прав root/sudo'))}
      {line(c.telegramOk ? 'ok' : 'bad', c.telegramOk ? t('api.telegram.org доступен, TLS за {ms} мс', { ms: c.telegramMs }) : t('api.telegram.org недоступен'))}
      {c.speedBps > 0 && line(c.speedBps >= 2 * 1024 * 1024 ? 'ok' : 'warn', t('скачивание с серверов Telegram: {speed}', { speed: speed(c.speedBps) }))}
      {line('ok', t('память {total}, свободно {free}; диск /var/lib: свободно {disk}', { total: bytes(c.memTotal), free: bytes(c.memAvailable), disk: bytes(c.diskFree) }))}
      {line('ok', c.runtime ? t('контейнеры: {rt}', { rt: c.runtime }) : t('Docker нет — будет установлен'))}
      {c.agentVersion && line('ok', t('агент Backupper {v} уже стоит', { v: c.agentVersion }))}
      {c.installed && line(c.running ? 'ok' : 'warn', c.running ? t('Bot API уже установлен и работает') : t('Bot API установлен, но служба не запущена'))}
      {c.warnings?.map(w => <div key={w}>{line('warn', w)}</div>)}
      {c.error && line('bad', <span className="selectable">{c.error}</span>)}
      {c.fingerprint && <div className="small faint">{t('Отпечаток ключа сервера:')} <span className="mono selectable">{c.fingerprint}</span></div>}
    </div>
  )
}
