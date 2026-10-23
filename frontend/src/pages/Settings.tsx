import { useState } from 'react'
import { Bell, Eye, EyeOff, KeyRound, Palette, Send, Wand2 } from 'lucide-react'
import { useApp } from '../App'
import { api } from '../api'
import type { Settings } from '../types'
import { Alert, Button, Field, Seg, Toggle, useUI } from '../components/ui'

function genPassword() {
  const a = new Uint8Array(18)
  crypto.getRandomValues(a)
  return btoa(String.fromCharCode(...a)).replace(/[+/=]/g, '').slice(0, 24)
}

export default function SettingsPage() {
  const { state, reload } = useApp()
  const { run, busy, toast } = useUI()
  const [s, setS] = useState<Settings>(state.settings)
  const [show, setShow] = useState(false)
  const set = (p: Partial<Settings>) => setS(v => ({ ...v, ...p }))
  const tg = (p: Partial<Settings['telegram']>) => set({ telegram: { ...s.telegram, ...p } })
  const pwChanged = state.settings.encryptionPassword && s.encryptionPassword !== state.settings.encryptionPassword

  const save = () => run('save', async () => {
    const errs = await api.saveSettings(s)
    await reload()
    const failed = Object.keys(errs)
    if (failed.length) toast('error', `Не удалось отправить на серверы: ${failed.map(id => state.servers.find(x => x.id === id)?.name || id).join(', ')}. Отправится при следующем подключении.`)
  }, 'Настройки сохранены и отправлены на серверы')

  return (
    <div className="page" style={{ maxWidth: 860 }}>
      <div className="page-head">
        <div>
          <h1>Настройки</h1>
          <div className="desc">Общие для всех серверов: передаются агентам при подключении</div>
        </div>
        <div className="actions"><Button variant="primary" loading={busy === 'save'} onClick={save}>Сохранить</Button></div>
      </div>

      <div className="editor-section">
        <h3><KeyRound size={17} /> Пароль шифрования архивов</h3>
        <div className="small muted" style={{ marginBottom: 12 }}>Используется задачами с включённым шифрованием (формат age, совместим с утилитой <span className="mono">age -d</span>). Хранится на этом ПК (зашифрован учётной записью Windows) и на серверах в /etc/backupper (доступ только root).</div>
        <Field>
          <div className="input-group">
            <input className="input mono" type={show ? 'text' : 'password'} value={s.encryptionPassword} onChange={e => set({ encryptionPassword: e.target.value })} placeholder="Не задан" />
            <Button icon={show ? <EyeOff size={16} /> : <Eye size={16} />} onClick={() => setShow(!show)} title="Показать" />
            <Button icon={<Wand2 size={16} />} onClick={() => { set({ encryptionPassword: genPassword() }); setShow(true) }}>Сгенерировать</Button>
          </div>
        </Field>
        <div className="mt12"><Alert tone="warning">Без этого пароля зашифрованные копии восстановить невозможно. Сохраните его в менеджере паролей.</Alert></div>
        {pwChanged && <div className="mt8"><Alert tone="danger">Вы меняете пароль. Старые копии останутся зашифрованы прежним паролем — скачивать их с расшифровкой придётся, временно вернув старый пароль.</Alert></div>}
      </div>

      <div className="editor-section">
        <h3><Bell size={17} /> Уведомления в Telegram</h3>
        <div className="small muted" style={{ marginBottom: 12 }}>Агенты сами отправляют сообщения — уведомление придёт, даже если этот компьютер выключен.</div>
        <Toggle on={s.telegram.enabled} onChange={v => tg({ enabled: v })} label="Включить уведомления" />
        {s.telegram.enabled && (
          <>
            <div className="grid g2 mt16">
              <Field label="Токен бота" hint={<>Создайте бота у <a href="#" onClick={e => { e.preventDefault(); api.openURL('https://t.me/BotFather') }}>@BotFather</a> командой /newbot</>}>
                <input className="input mono" type="password" value={s.telegram.botToken} onChange={e => tg({ botToken: e.target.value })} placeholder="123456:ABC…" />
              </Field>
              <Field label="Chat ID" hint={<>Напишите боту /start, затем узнайте свой ID у <a href="#" onClick={e => { e.preventDefault(); api.openURL('https://t.me/userinfobot') }}>@userinfobot</a></>}>
                <input className="input mono" value={s.telegram.chatId} onChange={e => tg({ chatId: e.target.value })} placeholder="123456789" />
              </Field>
            </div>
            <div className="mt12">
              <Button icon={<Send size={15} />} loading={busy === 'tg'} onClick={() => run('tg', () => api.testTelegram(s.telegram), 'Тестовое сообщение отправлено')}>Отправить тестовое сообщение</Button>
            </div>
          </>
        )}
      </div>

      <div className="editor-section">
        <h3><Palette size={17} /> Оформление</h3>
        <Seg value={s.theme} onChange={t => { set({ theme: t }); api.saveSettings({ ...s, theme: t }).then(reload) }} options={[['system', 'Как в Windows'], ['light', 'Светлая'], ['dark', 'Тёмная']]} />
      </div>
    </div>
  )
}
