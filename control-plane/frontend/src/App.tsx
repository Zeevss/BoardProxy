import { useState } from 'react'
import { ActivityTable } from './components/ActivityTable'
import { AppShell, type Section } from './components/AppShell'
import { ResourceView } from './components/ResourceView'
import { RuntimePanel } from './components/RuntimePanel'
import { StatusRail } from './components/StatusRail'
import { Topbar } from './components/Topbar'
import { TrafficChart } from './components/TrafficChart'
import { useControlPlane } from './hooks/useControlPlane'

const TOKEN_KEY = 'boardproxy-control-token-v1'

export function App() {
  const [token, setToken] = useState(() => sessionStorage.getItem(TOKEN_KEY) ?? '')
  if (!token) return <Login onLogin={value => { sessionStorage.setItem(TOKEN_KEY, value); setToken(value) }} />
  return <AuthenticatedApp token={token} onLogout={() => { sessionStorage.removeItem(TOKEN_KEY); setToken('') }} />
}

function AuthenticatedApp({ token, onLogout }: { token: string; onLogout: () => void }) {
  const [section, setSection] = useState<Section>('Overview')
  const [selectedNode, setSelectedNode] = useState<string>()
  const { api, data, loading, error, streamConnected, refresh } = useControlPlane(token, selectedNode)
  const selected = data.nodes.find(node => node.nodeId === selectedNode) ?? data.nodes[0]
  return <AppShell section={section} onSection={setSection} topbar={<Topbar nodes={data.nodes} selected={selected?.nodeId} onSelect={setSelectedNode} streamConnected={streamConnected} />}>
    {error ? <div className="global-error"><span>{error}</span><button onClick={onLogout}>Change token</button></div> : null}
    {loading ? <div className="loading-bar"/> : null}
    {section === 'Overview' ? <>
      <div className="page-heading"><h1>Fleet overview</h1><p>Real-time health, runtime state and activity across your BoardProxy fleet.</p></div>
      <StatusRail node={selected} status={data.status}/>
      <div className="dashboard-grid"><TrafficChart interfacePoints={data.interfaceTraffic} userPoints={data.userTraffic}/><RuntimePanel runtime={data.runtime}/></div>
      <ActivityTable events={data.events}/>
    </> : <ResourceView section={section} nodes={data.nodes} catalog={data.catalog} nodeId={selected?.nodeId} api={api} onChanged={() => refresh()}/>} 
  </AppShell>
}

function Login({ onLogin }: { onLogin: (token: string) => void }) {
  const [value, setValue] = useState('')
  return <main className="login-screen"><form className="login-panel" onSubmit={event => { event.preventDefault(); if (value.trim()) onLogin(value.trim()) }}><div className="brand login-brand"><span className="brand-mark">◇</span><span><strong>BoardProxy</strong><small>Control Plane</small></span></div><h1>Connect to control plane</h1><p>Enter a bearer token. It is kept only in this browser tab.</p><label>API token<input type="password" autoComplete="off" value={value} onChange={event => setValue(event.target.value)} placeholder="bpat_…"/></label><button className="primary-button" type="submit">Continue</button></form></main>
}
