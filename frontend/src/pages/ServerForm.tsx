import { useState } from 'react'
import { FolderOpen, KeyRound, Server as ServerIcon } from 'lucide-react'
import { api } from '../api'
import { useApp } from '../App'
import type { Server } from '../types'
import { Alert, Button, Field, Modal, useUI } from '../components/ui'
import { t } from '../i18n'

export default function ServerForm({ server, onClose, onSaved }: { server?: Server; onClose: () => void; onSaved: (id: string) => void }) {
  const { state, reload } = useApp()
  const { run, busy } = useUI()
  const [s, setS] = useState<Partial<Server>>(server ? { ...server, passphrase: '' } : { name: '', host: '', port: 22, user: 'root', keyPath: '', agentPort: 7171 })
  const set = (p: Partial<Server>) => setS(v => ({ ...v, ...p }))
  const isPub = !!s.keyPath?.toLowerCase().endsWith('.pub')

  const save = () => run('save', async () => {
    const id = await api.saveServer(isPub ? { ...s, keyPath: s.keyPath!.slice(0, -4) } : s)
    await reload()
    onSaved(id)
  })

  const keyHint = isPub
    ? <span style={{ color: 'var(--warning)' }}>{t('Это публичный ключ (.pub) — он лежит на сервере. Нужен приватный: файл с тем же именем без .pub. При сохранении путь будет исправлен.')}</span>
    : s.keyPath ? undefined
      : state.keys.length ? t('Если не указать — будут перепробованы {keys} и ключи из ssh-agent', { keys: state.keys.map(k => k.split('\\').pop()).join(', ') })
        : t('Ключи в %USERPROFILE%\\.ssh не найдены — выберите файл')

  return (
    <Modal title={server ? t('Настройки сервера') : t('Новый сервер')} icon={<ServerIcon size={20} />} onClose={onClose} footer={<>
      <Button onClick={onClose}>{t('Отмена')}</Button>
      <Button variant="primary" loading={busy === 'save'} onClick={save} disabled={!s.host}>{server ? t('Сохранить') : t('Добавить')}</Button>
    </>}>
      <div className="col" style={{ gap: 14 }}>
        <div className="grid g2">
          <Field label={t('Название')} hint={t('Будет папкой в облаке')}>
            <input className="input" value={s.name} onChange={e => set({ name: e.target.value })} placeholder={t('Например, Боевой')} autoFocus />
          </Field>
          <Field label={t('Адрес (IP или домен)')}>
            <input className="input" value={s.host} onChange={e => set({ host: e.target.value })} placeholder="203.0.113.10" />
          </Field>
        </div>
        <div className="grid g2">
          <Field label={t('Пользователь SSH')} hint={t('root или пользователь с sudo без пароля')}>
            <input className="input" value={s.user} onChange={e => set({ user: e.target.value })} />
          </Field>
          <Field label={t('Порт SSH')}>
            <input className="input" type="number" value={s.port} onChange={e => set({ port: +e.target.value })} />
          </Field>
        </div>
        <Field label={t('Приватный ключ')} hint={keyHint}>
          <div className="input-group">
            <input className="input mono" value={s.keyPath} onChange={e => set({ keyPath: e.target.value })} placeholder="C:\Users\...\.ssh\id_ed25519" />
            <Button icon={<FolderOpen size={16} />} onClick={async () => { const p = await api.pickKeyFile(); if (p) set({ keyPath: p }) }} title={t('Выбрать файл')} />
          </div>
        </Field>
        <Field label={t('Пароль ключа')} hint={server ? t('Оставьте пустым, чтобы не менять') : t('Только если ключ защищён паролем')}>
          <input className="input" type="password" value={s.passphrase} onChange={e => set({ passphrase: e.target.value })} />
        </Field>
        {server?.fingerprint && (
          <Alert icon={<KeyRound size={17} />}>
            {t('Отпечаток ключа сервера:')} <span className="mono selectable">{server.fingerprint}</span>
            <div className="mt8"><Button size="sm" onClick={() => run('reset', async () => { await api.resetHostKey(server.id); await reload() }, t('Ключ сброшен — будет запомнен при следующем подключении'))}>{t('Сбросить (если сервер переустановлен)')}</Button></div>
          </Alert>
        )}
      </div>
    </Modal>
  )
}
