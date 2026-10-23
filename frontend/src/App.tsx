import { createContext, useCallback, useContext, useEffect, useState } from 'react'
import { Cloud, Info, LayoutDashboard, Plus, Server as ServerIcon, Settings as SettingsIcon, ShieldCheck } from 'lucide-react'
import { api } from './api'
import type { AppState, ServerStatus } from './types'
import { UIProvider, usePoll } from './components/ui'
import { cls } from './util'
import Overview from './pages/Overview'
import ServerPage from './pages/Server'
import Clouds from './pages/Clouds'
import SettingsPage from './pages/Settings'
import ServerForm from './pages/ServerForm'
import About from './pages/About'

export type Route = { page: 'overview' } | { page: 'clouds' } | { page: 'settings' } | { page: 'about' } | { page: 'server'; id: string }

interface AppCtx {
  state: AppState
  reload: () => Promise<void>
  statuses: Record<string, ServerStatus>
  setStatus: (s: ServerStatus) => void
  refreshAll: () => Promise<void>
  go: (r: Route) => void
  addServer: () => void
}

const Ctx = createContext<AppCtx>(null as any)
export const useApp = () => useContext(Ctx)

function applyTheme(theme: string) {
  const dark = theme === 'dark' || (theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.dataset.theme = dark ? 'dark' : 'light'
}

export default function App() {
  const [state, setState] = useState<AppState | null>(null)
  const [route, setRoute] = useState<Route>({ page: 'overview' })
  const [statuses, setStatuses] = useState<Record<string, ServerStatus>>({})
  const [adding, setAdding] = useState(false)

  const reload = useCallback(async () => setState(await api.state()), [])
  useEffect(() => { reload() }, [reload])

  useEffect(() => {
    if (!state) return
    applyTheme(state.settings.theme)
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const h = () => applyTheme(state.settings.theme)
    mq.addEventListener('change', h)
    return () => mq.removeEventListener('change', h)
  }, [state?.settings.theme])

  const setStatus = useCallback((s: ServerStatus) => setStatuses(p => ({ ...p, [s.id]: s })), [])
  const refreshAll = useCallback(async () => {
    const list = await api.overview()
    setStatuses(Object.fromEntries(list.map(s => [s.id, s])))
  }, [])
  usePoll(() => { if (state?.servers.length) refreshAll() }, 30000, [state?.servers.length])

  if (!state) return null

  const ctx: AppCtx = { state, reload, statuses, setStatus, refreshAll, go: setRoute, addServer: () => setAdding(true) }

  return (
    <UIProvider>
      <Ctx.Provider value={ctx}>
        <div className="app">
          <aside className="sidebar">
            <div className="brand">
              <div className="brand-logo"><ShieldCheck size={19} /></div>
              <div>
                <div className="brand-name">Backupper</div>
                <div className="brand-ver">версия {state.version}</div>
              </div>
            </div>
            <button className={cls('nav-item', route.page === 'overview' && 'active')} onClick={() => setRoute({ page: 'overview' })}>
              <LayoutDashboard size={17} /><span className="grow">Обзор</span>
            </button>
            <button className={cls('nav-item', route.page === 'clouds' && 'active')} onClick={() => setRoute({ page: 'clouds' })}>
              <Cloud size={17} /><span className="grow">Облака</span>
              <span className="faint small">{state.storages.length || ''}</span>
            </button>
            <button className={cls('nav-item', route.page === 'settings' && 'active')} onClick={() => setRoute({ page: 'settings' })}>
              <SettingsIcon size={17} /><span className="grow">Настройки</span>
            </button>

            <div className="nav-section">
              Серверы
              <button className="btn ghost sm icon" title="Добавить сервер" onClick={() => setAdding(true)}><Plus size={15} /></button>
            </div>
            {state.servers.map(s => {
              const st = statuses[s.id]
              const running = st?.active?.some(r => r.status === 'running')
              return (
                <button key={s.id} className={cls('nav-item', route.page === 'server' && route.id === s.id && 'active')} onClick={() => setRoute({ page: 'server', id: s.id })}>
                  <ServerIcon size={17} />
                  <span className="grow">
                    {s.name}
                    <span className="sub">{s.host}</span>
                  </span>
                  <span className={cls('dot', !st ? '' : running ? 'wait' : st.online ? 'on' : 'off')} title={st?.error || ''} />
                </button>
              )
            })}
            {state.servers.length === 0 && <div className="faint small" style={{ padding: '4px 10px' }}>Пока нет серверов</div>}
            <div className="sidebar-foot">
              <button className={cls('nav-item', route.page === 'about' && 'active')} style={{ marginBottom: 8 }} onClick={() => setRoute({ page: 'about' })}>
                <Info size={17} /><span className="grow">О программе</span>
              </button>
              <button className="btn" style={{ width: '100%' }} onClick={() => setAdding(true)}><Plus size={16} /> Добавить сервер</button>
            </div>
          </aside>
          <main className="main">
            {route.page === 'overview' && <Overview />}
            {route.page === 'clouds' && <Clouds />}
            {route.page === 'settings' && <SettingsPage />}
            {route.page === 'about' && <About />}
            {route.page === 'server' && state.servers.some(s => s.id === route.id) && <ServerPage key={route.id} id={route.id} />}
          </main>
        </div>
        {adding && <ServerForm onClose={() => setAdding(false)} onSaved={id => { setAdding(false); setRoute({ page: 'server', id }) }} />}
      </Ctx.Provider>
    </UIProvider>
  )
}
