import { useEffect, useState } from 'react'
import { AppShell } from './components/AppShell'
import { Icon } from './components/Icon'
import { PanelAuthApi } from './api/controlApi'
import { useControlPlane } from './hooks/useControlPlane'
import { t } from './i18n'
import { Overview } from './screens/Overview'
import { Nodes } from './screens/Nodes'
import { Subscriptions } from './screens/Subscriptions'
import { Users } from './screens/Users'
import { Boards } from './screens/Boards'
import { Traffic } from './screens/Traffic'
import { Activity } from './screens/Activity'
import { Access } from './screens/Access'
import { PanelAuthScreen } from './screens/PanelAuthScreen'
import { GettingStarted } from './screens/GettingStarted'
import type { Language, PanelUser, Section } from './types'

const TOKEN_KEY = 'boardproxy-control-panel-session-v1'
const LANG_KEY = 'boardproxy-control-language-v1'
const authApi = new PanelAuthApi()

type AuthState =
  | { kind: 'checking' }
  | { kind: 'setup'; error?: string }
  | { kind: 'login'; error?: string }
  | { kind: 'ready'; token: string; user: PanelUser }

export function App() {
  const [auth, setAuth] = useState<AuthState>({ kind: 'checking' })
  const [authRevision, setAuthRevision] = useState(0)

  useEffect(() => {
    const controller = new AbortController()
    const storedToken = sessionStorage.getItem(TOKEN_KEY)
    const statusPromise = authApi.status(controller.signal)
    const userPromise = storedToken
      ? authApi.me(storedToken, controller.signal).catch(() => undefined)
      : Promise.resolve(undefined)
    void Promise.all([statusPromise, userPromise]).then(([status, user]) => {
      if (controller.signal.aborted) return
      if (storedToken && user) {
        setAuth({ kind: 'ready', token: storedToken, user })
        return
      }
      sessionStorage.removeItem(TOKEN_KEY)
      setAuth({ kind: status.setupRequired ? 'setup' : 'login' })
    }).catch(cause => {
      if (!controller.signal.aborted) {
        setAuth({ kind: 'login', error: cause instanceof Error ? cause.message : 'Control plane недоступен' })
      }
    })
    return () => controller.abort()
  }, [authRevision])

  if (auth.kind === 'checking') return <AuthLoading/>
  if (auth.kind === 'setup' || auth.kind === 'login') {
    return <PanelAuthScreen
      mode={auth.kind}
      initialError={auth.error}
      onRetry={() => { setAuth({ kind: 'checking' }); setAuthRevision(value => value + 1) }}
      onAuthenticated={session => {
        sessionStorage.setItem(TOKEN_KEY, session.token)
        setAuth({ kind: 'ready', token: session.token, user: session.user })
      }}
    />
  }
  return <AuthenticatedApp
    token={auth.token}
    user={auth.user}
    onLogout={() => {
      void authApi.logout(auth.token).catch(() => undefined)
      sessionStorage.removeItem(TOKEN_KEY)
      setAuth({ kind: 'login' })
    }}
  />
}

function AuthenticatedApp({ token, user, onLogout }: { token: string; user: PanelUser; onLogout: () => void }) {
  const [language, setLanguage] = useState<Language>(() => localStorage.getItem(LANG_KEY) === 'en' ? 'en' : 'ru')
  const [section, setSection] = useState<Section>('overview')
  const [selectedNode, setSelectedNode] = useState<string>()
  const [search, setSearch] = useState('')
  const [hours] = useState(24)
  const { api, data, loading, error, unauthorized, streamConnected, refresh } = useControlPlane(token, selectedNode, hours)
  const selected = data.nodes.find(node => node.nodeId === selectedNode) ?? data.nodes[0]
  const common = { language, data, api, onChanged: refresh }
  return <AppShell username={user.username} language={language} section={section} nodes={data.nodes} selectedNode={selected?.nodeId} search={search} streamConnected={streamConnected} onLanguage={next => { localStorage.setItem(LANG_KEY, next); setLanguage(next) }} onSection={setSection} onNode={setSelectedNode} onSearch={setSearch} onRefresh={() => void refresh()} onLogout={onLogout}>
    {loading ? <div className="loading-line" aria-label={t(language).loading}/> : null}
    {error ? <div className="global-error"><Icon name="warning" size={17}/><span>{error}</span>{unauthorized ? <button type="button" onClick={onLogout}>{t(language).logout}</button> : <button type="button" onClick={() => void refresh()}>{t(language).refresh}</button>}</div> : null}
    {section === 'overview' && !loading && data.nodes.length === 0 ? <GettingStarted onCreateNode={() => setSection('nodes')}/> : null}
    {section === 'overview' && (loading || data.nodes.length > 0) ? <Overview language={language} data={data} selected={selected}/> : null}
    {section === 'nodes' ? <Nodes {...common} search={search}/> : null}
    {section === 'subscriptions' ? <Subscriptions {...common} search={search}/> : null}
    {section === 'users' ? <Users {...common} search={search}/> : null}
    {section === 'boards' ? <Boards {...common} search={search}/> : null}
    {section === 'traffic' ? <Traffic {...common}/> : null}
    {section === 'activity' ? <Activity language={language} data={data} search={search}/> : null}
    {section === 'access' ? <Access {...common}/> : null}
  </AppShell>
}

function AuthLoading() {
  return <main className="login-screen"><div className="login-panel auth-loading"><div className="brand login-brand"><span className="brand-mark"><span/></span><div><strong>BoardProxy</strong><small>Control plane</small></div></div><div className="auth-spinner"/><p>Проверяем состояние панели…</p></div></main>
}
