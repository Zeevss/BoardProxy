import type { NodeStatus, NodeSummary } from '../model'
import type { ReactNode } from 'react'

export function StatusRail({ node, status }: { node?: NodeSummary; status?: NodeStatus }) {
  const drift = (status?.desiredRevision ?? 0) !== (status?.appliedRevision ?? 0)
  return <section className="status-rail" aria-label="Node health">
    <Metric label="Node"><strong>{node?.name ?? 'No node selected'}</strong><span className={status?.connected ? 'status ok' : 'status bad'}>● {status?.connected ? 'Online' : 'Offline'}</span><small>{status?.coreReady ? '● Core ready' : 'Core not ready'}</small></Metric>
    <Metric label="Desired revision"><strong>{status?.desiredRevision ?? '—'}</strong></Metric>
    <Metric label="Applied revision"><strong>{status?.appliedRevision ?? '—'}</strong></Metric>
    <Metric label="Drift"><span className={drift ? 'status warn emphasis' : 'status ok'}>{drift ? '△ Out of sync' : '✓ Converged'}</span><small>{drift ? 'Revisions differ' : 'Desired state applied'}</small></Metric>
    <Metric label="Last seen"><strong>{relative(status?.lastSeen)}</strong><small>{status?.lastSeen ? new Date(status.lastSeen).toLocaleString() : 'No heartbeat'}</small></Metric>
  </section>
}

function Metric({ label, children }: { label: string; children: ReactNode }) { return <div className="metric"><span className="metric-label">{label}</span>{children}</div> }
function relative(value?: string) {
  if (!value) return 'Never'
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 1000))
  return seconds < 60 ? `${seconds}s ago` : `${Math.floor(seconds / 60)}m ago`
}
