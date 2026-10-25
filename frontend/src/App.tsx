import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { Cloud, Globe, Info, LayoutDashboard, Plus, Server as ServerIcon, Settings as SettingsIcon, ShieldCheck } from 'lucide-react'
import { api } from './api'
import type { AppState, JobView, ServerStatus } from './types'
import { UIProvider, usePoll } from './components/ui'
import { cls } from './util'
import { defaultLang, setLang, t } from './i18n'
import Overview from './pages/Overview'
import ServerPage from './pages/Server'
import Clouds from './pages/Clouds'
import SettingsPage from './pages/Settings'
import ServerForm from './pages/ServerForm'
import About from './pages/About'
import SitePage from './pages/Site'
import JobEditor from './pages/JobEditor'

export type Route = { page: 'overview' } | { page: 'clouds' } | { page: 'settings' } | { page: 'about' } | { page: 'server'; id: string }
  | { page: 'site'; server: string; job: string }

interface AppCtx {
  state: AppState
  reload: () => Promise<void>
  statuses: Record<string, ServerStatus>
  setStatus: (s: ServerStatus) => void
  refreshAll: () => Promise<void>
  go: (r: Route) => void
  addServer: () => void
  addSite: () => void
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
  const [addingSite, setAddingSite] = useState(false)
  // Last known site jobs per server: a site stays in the menu while its server is unreachable.
  const siteJobs = useRef<Record<string, JobView[]>>({})

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
  // Set before rendering so every t() below uses the chosen language.
  setLang(state.settings.language || defaultLang())

  const sites = state.servers.flatMap(s => {
    const st = statuses[s.id]
    if (st?.info) siteJobs.current[s.id] = st.info.jobs.filter(j => j.kind === 'site')
    return (siteJobs.current[s.id] || []).map(j => ({ j, s, st }))
  }).sort((a, b) => a.j.name.localeCompare(b.j.name))

  const ctx: AppCtx = { state, reload, statuses, setStatus, refreshAll, go: setRoute, addServer: () => setAdding(true), addSite: () => setAddingSite(true) }

  return (
    <UIProvider>
      <Ctx.Provider value={ctx}>
        <div className="app">
          <aside className="sidebar">
            <div className="brand">
              <div className="brand-logo"><ShieldCheck size={19} /></div>
              <div>
                <div className="brand-name">Backupper</div>
                <div className="brand-ver">{t('версия {v}', { v: state.version })}</div>
              </div>
            </div>
            <button className={cls('nav-item', route.page === 'overview' && 'active')} onClick={() => setRoute({ page: 'overview' })}>
              <LayoutDashboard size={17} /><span className="grow">{t('Обзор')}</span>
            </button>
            <button className={cls('nav-item', route.page === 'clouds' && 'active')} onClick={() => setRoute({ page: 'clouds' })}>
              <Cloud size={17} /><span className="grow">{t('Облака')}</span>
              <span className="faint small">{state.storages.length || ''}</span>
            </button>
            <button className={cls('nav-item', route.page === 'settings' && 'active')} onClick={() => setRoute({ page: 'settings' })}>
              <SettingsIcon size={17} /><span className="grow">{t('Настройки')}</span>
            </button>

            <div className="nav-section">
              {t('Серверы')}
              <button className="btn ghost sm icon" title={t('Добавить сервер')} onClick={() => setAdding(true)}><Plus size={15} /></button>
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
            {state.servers.length === 0 && <div className="faint small" style={{ padding: '4px 10px' }}>{t('Пока нет серверов')}</div>}

            <div className="nav-section">
              {t('Сайты на хостинге')}
              <button className="btn ghost sm icon" title={t('Добавить сайт')} onClick={() => setAddingSite(true)}><Plus size={15} /></button>
            </div>
            {sites.map(({ j, s, st }) => {
              const running = st?.active?.some(r => r.jobId === j.id && r.status === 'running')
              const last = j.lastRun?.status
              const dot = !st ? '' : !st.online ? 'off' : running ? 'wait' : last === 'failed' ? 'off' : last ? 'on' : ''
              return (
                <button key={s.id + j.id} className={cls('nav-item', route.page === 'site' && route.job === j.id && 'active')} onClick={() => setRoute({ page: 'site', server: s.id, job: j.id })}>
                  <Globe size={17} />
                  <span className="grow">
                    {j.name}
                    <span className="sub">{t('через {server}', { server: s.name })}</span>
                  </span>
                  <span className={cls('dot', dot)} title={st && !st.online ? st.error || '' : ''} />
                </button>
              )
            })}
            {sites.length === 0 && <div className="faint small" style={{ padding: '4px 10px' }}>{t('Пока нет сайтов')}</div>}
            <div className="sidebar-foot">
              <button className={cls('nav-item', route.page === 'about' && 'active')} style={{ marginBottom: 8 }} onClick={() => setRoute({ page: 'about' })}>
                <Info size={17} /><span className="grow">{t('О программе')}</span>
              </button>
              <button className="btn" style={{ width: '100%' }} onClick={() => setAdding(true)}><Plus size={16} /> {t('Добавить сервер')}</button>
            </div>
          </aside>
          <main className="main">
            {route.page === 'overview' && <Overview />}
            {route.page === 'clouds' && <Clouds />}
            {route.page === 'settings' && <SettingsPage />}
            {route.page === 'about' && <About />}
            {route.page === 'server' && state.servers.some(s => s.id === route.id) && <ServerPage key={route.id} id={route.id} />}
            {route.page === 'site' && state.servers.some(s => s.id === route.server) && <SitePage key={route.server + route.job} server={route.server} job={route.job} />}
          </main>
        </div>
        {addingSite && <JobEditor site serverId="" job={null} onClose={() => setAddingSite(false)}
          onSaved={(server, job) => { setAddingSite(false); setRoute({ page: 'site', server, job }) }} />}
        {adding && <ServerForm onClose={() => setAdding(false)} onSaved={id => { setAdding(false); setRoute({ page: 'server', id }) }} />}
      </Ctx.Provider>
    </UIProvider>
  )
}
