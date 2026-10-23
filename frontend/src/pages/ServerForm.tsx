import { useState } from 'react'
import { FolderOpen, KeyRound, Server as ServerIcon } from 'lucide-react'
import { api } from '../api'
import { useApp } from '../App'
import type { Server } from '../types'
import { Alert, Button, Field, Modal, useUI } from '../components/ui'

export default function ServerForm({ server, onClose, onSaved }: { server?: Server; onClose: () => void; onSaved: (id: string) => void }) {
  const { state, reload } = useApp()
  const { run, busy } = useUI()
  const [s, setS] = useState<Partial<Server>>(server ? { ...server, passphrase: '' } : { name: '', host: '', port: 22, user: 'root', keyPath: '', agentPort: 7171 })
  const set = (p: Partial<Server>) => setS(v => ({ ...v, ...p }))

  const save = () => run('save', async () => {
    const id = await api.saveServer(s)
    await reload()
    onSaved(id)
  })

  return (
    <Modal title={server ? 'Настройки сервера' : 'Новый сервер'} icon={<ServerIcon size={20} />} onClose={onClose} footer={<>
      <Button onClick={onClose}>Отмена</Button>
      <Button variant="primary" loading={busy === 'save'} onClick={save} disabled={!s.host}>{server ? 'Сохранить' : 'Добавить'}</Button>
    </>}>
      <div className="col" style={{ gap: 14 }}>
        <div className="grid g2">
          <Field label="Название" hint="Будет папкой в облаке">
            <input className="input" value={s.name} onChange={e => set({ name: e.target.value })} placeholder="Например, Боевой" autoFocus />
          </Field>
          <Field label="Адрес (IP или домен)">
            <input className="input" value={s.host} onChange={e => set({ host: e.target.value })} placeholder="203.0.113.10" />
          </Field>
        </div>
        <div className="grid g2">
          <Field label="Пользователь SSH" hint="root или пользователь с sudo без пароля">
            <input className="input" value={s.user} onChange={e => set({ user: e.target.value })} />
          </Field>
          <Field label="Порт SSH">
            <input className="input" type="number" value={s.port} onChange={e => set({ port: +e.target.value })} />
          </Field>
        </div>
        <Field label="Приватный ключ" hint={s.keyPath ? undefined : state.keys.length ? `Если не указать — будут перепробованы ${state.keys.map(k => k.split('\\').pop()).join(', ')} и ключи из ssh-agent` : 'Ключи в %USERPROFILE%\\.ssh не найдены — выберите файл'}>
          <div className="input-group">
            <input className="input mono" value={s.keyPath} onChange={e => set({ keyPath: e.target.value })} placeholder="C:\Users\...\.ssh\id_ed25519" />
            <Button icon={<FolderOpen size={16} />} onClick={async () => { const p = await api.pickKeyFile(); if (p) set({ keyPath: p }) }} title="Выбрать файл" />
          </div>
        </Field>
        <Field label="Пароль ключа" hint={server ? 'Оставьте пустым, чтобы не менять' : 'Только если ключ защищён паролем'}>
          <input className="input" type="password" value={s.passphrase} onChange={e => set({ passphrase: e.target.value })} />
        </Field>
        {server?.fingerprint && (
          <Alert icon={<KeyRound size={17} />}>
            Отпечаток ключа сервера: <span className="mono selectable">{server.fingerprint}</span>
            <div className="mt8"><Button size="sm" onClick={() => run('reset', async () => { await api.resetHostKey(server.id); await reload() }, 'Ключ сброшен — будет запомнен при следующем подключении')}>Сбросить (если сервер переустановлен)</Button></div>
          </Alert>
        )}
      </div>
    </Modal>
  )
}
