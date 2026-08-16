import type { DashboardData, Language, NodeSummary } from '../types'
import { PageHeader } from '../components/AppShell'
import { Panel, Badge, Empty } from '../components/UI'
import { Progress, TrafficChart } from '../components/Charts'
import { ago, bytes, eventKind, message, rate } from '../lib/format'

export function Overview({ language, data, selected }: { language: Language; data: DashboardData; selected?: NodeSummary }) {
  const statuses = Object.values(data.statuses).filter(Boolean)
  const runtimes = Object.values(data.runtimes).filter(Boolean)
  const online = statuses.filter(status => status?.connected).length
  const sessions = runtimes.reduce((sum, runtime) => sum + (runtime?.users.reduce((count, user) => count + user.activeSessions, 0) ?? 0), 0)
  const drifted = statuses.filter(status => status && status.desiredRevision !== status.appliedRevision)
  const latestBucket = data.interfaceTraffic.at(-1)
  const throughput = rate((latestBucket?.rxBytes ?? 0) + (latestBucket?.txBytes ?? 0))
  const alerts = data.quotas.filter(quota => quota.exceeded)
  const topUsers = [...data.userTotals].sort((a, b) => b.rxBytes + b.txBytes - a.rxBytes - a.txBytes).slice(0, 5)
  const topMax = Math.max(1, ...topUsers.map(user => user.rxBytes + user.txBytes))
  return <>
    <PageHeader language={language} section="overview" action={<RangeLabel selected={selected}/>} />
    <div className="kpi-grid">
      <Metric label="Nodes online" value={`${online}/${data.nodes.length}`} note="Now"/>
      <Metric label="Active sessions" value={String(sessions)} note="Runtime projections"/>
      <Metric label="Selected throughput" value={throughput} note={selected?.name ?? 'No node selected'}/>
      <Metric label="Config drift" value={String(drifted.length)} note={drifted.map(status => status?.nodeId).join(', ') || 'Converged'} tone={drifted.length ? 'warn' : 'ok'}/>
      <Metric label="Quota alerts" value={String(alerts.length)} note={alerts.map(alert => alert.quota.userTag).join(', ') || 'No alerts'} tone={alerts.length ? 'warn' : 'ok'}/>
    </div>
    <div className="overview-main">
      <Panel title="Interface traffic" meta={`${selected?.nodeId ?? '—'} · separate rx / tx`}><TrafficChart points={data.interfaceTraffic}/></Panel>
      <Panel title="Active sessions" meta={`${selected?.nodeId ?? '—'} · live projection`}><div className="session-summary"><strong>{data.runtimes[selected?.nodeId ?? '']?.sessions.length ?? 0}</strong><span>complete session details</span><dl><div><dt>Runtime revision</dt><dd>{data.runtimes[selected?.nodeId ?? '']?.runtimeRevision ?? '—'}</dd></div><div><dt>Sequence</dt><dd>{data.runtimes[selected?.nodeId ?? '']?.lastSequence ?? '—'}</dd></div><div><dt>Projection</dt><dd>{data.runtimes[selected?.nodeId ?? '']?.gapDetected ? <Badge tone="bad">gap</Badge> : <Badge tone="ok">healthy</Badge>}</dd></div></dl></div></Panel>
    </div>
    <div className="overview-secondary">
      <Panel title="User payload" meta="Per-user payload · separate series"><TrafficChart points={data.userTraffic} mode="user" height={150}/></Panel>
      <Panel title="Top users" meta="Selected range">{topUsers.length ? <div className="ranking">{topUsers.map(user => { const total = user.rxBytes + user.txBytes; return <div key={user.subject}><span>{user.subject}</span><code>{bytes(total)}</code><Progress value={total / topMax * 100}/></div> })}</div> : <Empty title="No user traffic"/>}</Panel>
    </div>
    <Panel title="Node health" action={<span className="accent-link">All nodes</span>} className="node-health-panel">{data.nodes.length ? <div className="health-grid">{data.nodes.map(node => { const status = data.statuses[node.nodeId]; const runtime = data.runtimes[node.nodeId]; const drift = status && status.desiredRevision !== status.appliedRevision; return <div className="health-item" key={node.nodeId}><div><span className={`dot ${status?.connected ? drift ? 'warn' : 'ok' : 'bad'}`}/><strong>{node.name}</strong><small>{node.nodeId}</small></div><code>{runtime?.sessions.length ?? 0} sess</code><code>{status ? `${status.appliedRevision}/${status.desiredRevision}` : '—'}</code><span>{ago(status?.lastSeen)}</span></div> })}</div> : <Empty/>}</Panel>
    <Panel title="Recent activity" meta="Selected node runtime events">{data.events.length ? <div className="event-list">{data.events.slice(0, 8).map(event => <div key={event.eventId}><time>{new Date(event.occurredAt).toLocaleTimeString()}</time><span className={`event-chip ${eventKind(event.type)}`}>{event.type}</span><p>{message(event.payload)}</p><code>{selected?.nodeId}</code></div>)}</div> : <Empty title="No runtime events"/>}</Panel>
  </>
}

function Metric({ label, value, note, tone }: { label: string; value: string; note: string; tone?: 'ok' | 'warn' }) { return <div className="metric"><span>{label}</span><strong className={tone}>{value}</strong><small className={tone}>{note}</small></div> }
function RangeLabel({ selected }: { selected?: NodeSummary }) { return <span className="scope-label"><span className="dot ok"/>{selected ? selected.name : 'First available node'}</span> }
