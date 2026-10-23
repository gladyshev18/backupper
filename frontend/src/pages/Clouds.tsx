import { useState } from 'react'
import { Cloud, ExternalLink, Pencil, Plug, Plus, Trash2 } from 'lucide-react'
import { useApp } from '../App'
import { api } from '../api'
import type { Storage, StorageKind, StorageTest } from '../types'
import { Alert, Bar, Button, Empty, Field, Modal, Seg, useUI } from '../components/ui'
import { bytes, cls, kindLabels } from '../util'

function KindLogo({ kind }: { kind: StorageKind }) {
  if (kind === 'yandex' || kind === 'yandex_webdav') return <div className="kind-logo yandex">Я</div>
  if (kind === 'mailru') return <div className="kind-logo mailru">@</div>
  return <div className="kind-logo webdav"><Cloud size={18} /></div>
}

export default function Clouds() {
  const { state, reload } = useApp()
  const { run, busy, confirm } = useUI()
  const [edit, setEdit] = useState<Storage | 'new' | null>(null)

  const del = async (s: Storage) => {
    if (!await confirm(<>Удалить облако <b>{s.name}</b> из панели? Задачи, которые выгружают в него, начнут завершаться ошибкой. Файлы в самом облаке не удаляются.</>, 'Удалить', true)) return
    await run('d' + s.id, async () => { await api.deleteStorage(s.id); await reload() })
  }

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <h1>Облака</h1>
          <div className="desc">Куда агенты выгружают копии. Настройки автоматически передаются на все серверы.</div>
        </div>
        <div className="actions"><Button variant="primary" icon={<Plus size={16} />} onClick={() => setEdit('new')}>Подключить облако</Button></div>
      </div>
      {state.storages.length === 0 ? (
        <div className="card">
          <Empty icon={<Cloud size={26} />} title="Облака не подключены" action={<Button variant="primary" icon={<Plus size={16} />} onClick={() => setEdit('new')}>Подключить облако</Button>}>
            Поддерживаются Яндекс Диск, Облако Mail.ru и любой WebDAV-сервер.
          </Empty>
        </div>
      ) : (
        <div className="grid g3">
          {state.storages.map(s => (
            <div key={s.id} className="card card-pad">
              <div className="row">
                <KindLogo kind={s.kind} />
                <div className="grow">
                  <div style={{ fontWeight: 600 }} className="ellipsis">{s.name}</div>
                  <div className="small faint">{kindLabels[s.kind]}</div>
                </div>
              </div>
              <div className="small muted mt12">Папка: <span className="mono">{s.root}</span>{s.login && <> · {s.login}</>}</div>
              <div className="row mt16" style={{ gap: 6 }}>
                <Button size="sm" icon={<Pencil size={13} />} onClick={() => setEdit(s)}>Изменить</Button>
                <span className="grow" />
                <Button size="sm" variant="danger" icon={<Trash2 size={13} />} loading={busy === 'd' + s.id} onClick={() => del(s)} />
              </div>
            </div>
          ))}
        </div>
      )}
      {edit && <StorageForm storage={edit === 'new' ? null : edit} onClose={() => setEdit(null)} />}
    </div>
  )
}

function StorageForm({ storage, onClose }: { storage: Storage | null; onClose: () => void }) {
  const { state, reload } = useApp()
  const { run, busy } = useUI()
  const [s, setS] = useState<Storage>(storage ? { ...storage, password: '', token: '' } : { id: '', name: 'Яндекс Диск', kind: 'yandex', root: '/Backupper', login: '', password: '', token: '', url: '' })
  const set = (p: Partial<Storage>) => setS(v => ({ ...v, ...p }))
  const [test, setTest] = useState<StorageTest | null>(null)
  const [clientId, setClientId] = useState(state.settings.yandexClientId || '')
  const family = s.kind === 'yandex_webdav' ? 'yandex' : s.kind
  const defaultNames = ['', 'Яндекс Диск', 'Облако Mail.ru', 'WebDAV']
  const pickKind = (k: StorageKind) => set({ kind: k, name: defaultNames.includes(s.name) ? (k === 'mailru' ? 'Облако Mail.ru' : k === 'webdav' ? 'WebDAV' : 'Яндекс Диск') : s.name })

  const doTest = () => run('test', async () => { setTest(null); setTest(await api.testStorage(s)) })
  const save = () => run('save', async () => {
    if (clientId !== state.settings.yandexClientId) await api.saveSettings({ ...state.settings, yandexClientId: clientId })
    await api.saveStorage(s)
    await reload()
    onClose()
  }, 'Облако сохранено и отправлено на серверы')
  const getToken = () => api.openURL(`https://oauth.yandex.ru/authorize?response_type=token&client_id=${encodeURIComponent(clientId.trim())}`)
  const link = (url: string, text: string) => <a href="#" onClick={e => { e.preventDefault(); api.openURL(url) }}>{text} <ExternalLink size={11} /></a>
  const secretHint = storage ? 'Оставьте пустым, чтобы не менять' : undefined

  return (
    <Modal wide title={storage ? 'Облако' : 'Подключить облако'} onClose={onClose} footer={<>
      <Button icon={<Plug size={15} />} loading={busy === 'test'} onClick={doTest} className="" >Проверить подключение</Button>
      <span className="grow" />
      <Button onClick={onClose}>Отмена</Button>
      <Button variant="primary" loading={busy === 'save'} onClick={save}>Сохранить</Button>
    </>}>
      <div className="kind-tiles">
        {([['yandex', 'Яндекс Диск', 'Быстрый REST API'], ['mailru', 'Облако Mail.ru', 'WebDAV, пароль приложения'], ['webdav', 'WebDAV', 'Nextcloud, свой сервер']] as const).map(([k, t, d]) => (
          <button key={k} className={cls('kind-tile', family === k && 'on')} onClick={() => pickKind(k)} disabled={!!storage && family !== k}>
            <KindLogo kind={k} />
            <div><div style={{ fontWeight: 600 }}>{t}</div><div className="small faint">{d}</div></div>
          </button>
        ))}
      </div>

      <div className="grid g2 mt16">
        <Field label="Название"><input className="input" value={s.name} onChange={e => set({ name: e.target.value })} /></Field>
        <Field label="Папка в облаке" hint="Внутри: <сервер>/<задача>/<дата>"><input className="input mono" value={s.root} onChange={e => set({ root: e.target.value })} /></Field>
      </div>

      {family === 'yandex' && (
        <div className="mt16">
          <Seg value={s.kind} onChange={k => set({ kind: k })} options={[['yandex', 'OAuth-токен (рекомендуется)'], ['yandex_webdav', 'Пароль приложения (WebDAV)']]} />
          {s.kind === 'yandex' ? (
            <div className="col mt12" style={{ gap: 12 }}>
              <Alert>
                <ol className="steps">
                  <li><span>Создайте приложение на {link('https://oauth.yandex.ru/client/new', 'oauth.yandex.ru')}: платформа «Веб-сервисы», Redirect URI <span className="mono selectable">https://oauth.yandex.ru/verification_code</span>, доступы «Яндекс Диск REST API»: запись, чтение, информация о Диске.</span></li>
                  <li>Вставьте ClientID ниже и нажмите «Получить токен» — откроется браузер.</li>
                  <li>Скопируйте токен со страницы Яндекса в поле «OAuth-токен».</li>
                </ol>
              </Alert>
              <div className="grid g2">
                <Field label="ClientID приложения">
                  <div className="input-group">
                    <input className="input mono" value={clientId} onChange={e => setClientId(e.target.value)} />
                    <Button disabled={!clientId.trim()} onClick={getToken}>Получить токен</Button>
                  </div>
                </Field>
                <Field label="OAuth-токен" hint={secretHint}>
                  <input className="input mono" type="password" value={s.token} onChange={e => set({ token: e.target.value })} placeholder={storage ? '••••••••' : 'y0_…'} />
                </Field>
              </div>
            </div>
          ) : (
            <div className="col mt12" style={{ gap: 12 }}>
              <Alert tone="warning">Яндекс сильно ограничивает скорость загрузки по WebDAV для больших файлов. Подходит для небольших бэкапов; для больших используйте OAuth-токен.</Alert>
              <div className="grid g2">
                <Field label="Логин Яндекса"><input className="input" value={s.login} onChange={e => set({ login: e.target.value })} placeholder="ivan.petrov" /></Field>
                <Field label="Пароль приложения" hint={secretHint || <>Создайте на {link('https://id.yandex.ru/security/app-passwords', 'id.yandex.ru')} → «Файлы»</>}>
                  <input className="input" type="password" value={s.password} onChange={e => set({ password: e.target.value })} />
                </Field>
              </div>
            </div>
          )}
        </div>
      )}

      {s.kind === 'mailru' && (
        <div className="col mt16" style={{ gap: 12 }}>
          <Alert>
            Нужен <b>пароль для внешнего приложения</b>, а не обычный пароль от почты: {link('https://account.mail.ru/user/2-step-auth/passwords/', 'Настройки → Безопасность → Пароли для внешних приложений')}, доступ «Облако (WebDAV)».
            Mail.ru принимает файлы не больше 2 ГБ на бесплатном тарифе — размер куска подстроится автоматически.
          </Alert>
          <div className="grid g2">
            <Field label="Почта"><input className="input" value={s.login} onChange={e => set({ login: e.target.value })} placeholder="ivan@mail.ru" /></Field>
            <Field label="Пароль приложения" hint={secretHint}><input className="input" type="password" value={s.password} onChange={e => set({ password: e.target.value })} /></Field>
          </div>
        </div>
      )}

      {s.kind === 'webdav' && (
        <div className="col mt16" style={{ gap: 12 }}>
          <Field label="Адрес WebDAV" hint="Например: https://cloud.example.com/remote.php/dav/files/ivan"><input className="input mono" value={s.url} onChange={e => set({ url: e.target.value })} /></Field>
          <div className="grid g2">
            <Field label="Логин"><input className="input" value={s.login} onChange={e => set({ login: e.target.value })} /></Field>
            <Field label="Пароль" hint={secretHint}><input className="input" type="password" value={s.password} onChange={e => set({ password: e.target.value })} /></Field>
          </div>
        </div>
      )}

      {busy === 'test' && <div className="mt16"><Bar value={0} indeterminate /></div>}
      {test && (
        <div className="mt16">
          {test.ok ? (
            <Alert tone="success">
              Подключение работает, папка {s.root} доступна.
              {test.total > 0 && <> Занято {bytes(test.used)} из {bytes(test.total)} (свободно {bytes(test.total - test.used)}).</>}
              {test.maxFile > 0 && <> Максимальный размер файла: {bytes(test.maxFile)}.</>}
              <div className="small faint mt8">Это проверка с вашего компьютера. Скорость с сервера измеряется в редакторе задачи.</div>
            </Alert>
          ) : <Alert tone="danger"><span className="selectable">{test.error}</span></Alert>}
        </div>
      )}
    </Modal>
  )
}
