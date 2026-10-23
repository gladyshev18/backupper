import { useState } from 'react'
import { Bell, Eye, EyeOff, KeyRound, Languages, Palette, Send, Wand2 } from 'lucide-react'
import { useApp } from '../App'
import { api } from '../api'
import type { Settings } from '../types'
import { Alert, Button, Field, Seg, Toggle, useUI } from '../components/ui'
import { getLang, t } from '../i18n'

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
  // Appearance applies at once, without the Save button.
  const applyNow = (p: Partial<Settings>) => { set(p); api.saveSettings({ ...state.settings, ...p }).then(reload) }

  const save = () => run('save', async () => {
    const errs = await api.saveSettings(s)
    await reload()
    const failed = Object.keys(errs)
    if (failed.length) toast('error', t('Не удалось отправить на серверы: {list}. Отправится при следующем подключении.', { list: failed.map(id => state.servers.find(x => x.id === id)?.name || id).join(', ') }))
  }, t('Настройки сохранены и отправлены на серверы'))

  const botLink = <a href="#" onClick={e => { e.preventDefault(); api.openURL('https://t.me/BotFather') }}>@BotFather</a>
  const idLink = <a href="#" onClick={e => { e.preventDefault(); api.openURL('https://t.me/userinfobot') }}>@userinfobot</a>

  return (
    <div className="page" style={{ maxWidth: 860 }}>
      <div className="page-head">
        <div>
          <h1>{t('Настройки')}</h1>
          <div className="desc">{t('Общие для всех серверов: передаются агентам при подключении')}</div>
        </div>
        <div className="actions"><Button variant="primary" loading={busy === 'save'} onClick={save}>{t('Сохранить')}</Button></div>
      </div>

      <div className="editor-section">
        <h3><KeyRound size={17} /> {t('Пароль шифрования архивов')}</h3>
        <div className="small muted" style={{ marginBottom: 12 }}>{t('Используется задачами с включённым шифрованием (формат age, совместим с утилитой age -d). Хранится на этом ПК (зашифрован учётной записью Windows) и на серверах в /etc/backupper (доступ только root).')}</div>
        <Field>
          <div className="input-group">
            <input className="input mono" type={show ? 'text' : 'password'} value={s.encryptionPassword} onChange={e => set({ encryptionPassword: e.target.value })} placeholder={t('Не задан')} />
            <Button icon={show ? <EyeOff size={16} /> : <Eye size={16} />} onClick={() => setShow(!show)} title={t('Показать')} />
            <Button icon={<Wand2 size={16} />} onClick={() => { set({ encryptionPassword: genPassword() }); setShow(true) }}>{t('Сгенерировать')}</Button>
          </div>
        </Field>
        <div className="mt12"><Alert tone="warning">{t('Без этого пароля зашифрованные копии восстановить невозможно. Сохраните его в менеджере паролей.')}</Alert></div>
        {pwChanged && <div className="mt8"><Alert tone="danger">{t('Вы меняете пароль. Старые копии останутся зашифрованы прежним паролем — скачивать их с расшифровкой придётся, временно вернув старый пароль.')}</Alert></div>}
      </div>

      <div className="editor-section">
        <h3><Bell size={17} /> {t('Уведомления в Telegram')}</h3>
        <div className="small muted" style={{ marginBottom: 12 }}>{t('Агенты сами отправляют сообщения — уведомление придёт, даже если этот компьютер выключен.')}</div>
        <Toggle on={s.telegram.enabled} onChange={v => tg({ enabled: v })} label={t('Включить уведомления')} />
        {s.telegram.enabled && (
          <>
            <div className="grid g2 mt16">
              <Field label={t('Токен бота')} hint={<>{t('Создайте бота у')} {botLink} {t('командой /newbot')}</>}>
                <input className="input mono" type="password" value={s.telegram.botToken} onChange={e => tg({ botToken: e.target.value })} placeholder="123456:ABC…" />
              </Field>
              <Field label="Chat ID" hint={<>{t('Напишите боту /start, затем узнайте свой ID у')} {idLink}</>}>
                <input className="input mono" value={s.telegram.chatId} onChange={e => tg({ chatId: e.target.value })} placeholder="123456789" />
              </Field>
            </div>
            <div className="mt12">
              <Button icon={<Send size={15} />} loading={busy === 'tg'} onClick={() => run('tg', () => api.testTelegram(s.telegram), t('Тестовое сообщение отправлено'))}>{t('Отправить тестовое сообщение')}</Button>
            </div>
          </>
        )}
      </div>

      <div className="editor-section">
        <h3><Palette size={17} /> {t('Оформление')}</h3>
        <div className="col" style={{ gap: 14 }}>
          <Field label={<span className="row" style={{ gap: 6 }}><Languages size={14} /> {t('Язык интерфейса')}</span>}>
            <Seg value={getLang()} onChange={l => applyNow({ language: l })} options={[['ru', 'Русский'], ['en', 'English']]} />
          </Field>
          <Field label={t('Тема')}>
            <Seg value={s.theme} onChange={th => applyNow({ theme: th })} options={[['system', t('Как в Windows')], ['light', t('Светлая')], ['dark', t('Тёмная')]]} />
          </Field>
        </div>
      </div>
    </div>
  )
}
