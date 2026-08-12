import { useState, type FormEvent } from 'react'
import type { ControlApi } from '../api/controlApi'
import type { Catalog, NodeSummary } from '../model'
import type { Section } from './AppShell'

type ResourceKind = 'User' | 'Board'

export function ResourceView({ section, nodes, catalog, nodeId, api, onChanged }: {
  section: Section
  nodes: NodeSummary[]
  catalog?: Catalog
  nodeId?: string
  api: ControlApi
  onChanged: () => void
}) {
  const [editor, setEditor] = useState<ResourceKind>()
  const [busy, setBusy] = useState<string>()
  const [error, setError] = useState<string>()
  const rows = section === 'Users' ? catalog?.users : section === 'Boards' ? catalog?.boards : undefined

  const mutate = async (key: string, action: () => Promise<unknown>) => {
    setBusy(key); setError(undefined)
    try { await action(); await onChanged() } catch (cause) { setError(cause instanceof Error ? cause.message : 'Mutation failed') } finally { setBusy(undefined) }
  }

  const toggleNode = (node: NodeSummary) => mutate(node.nodeId, () => api.mutate(`/api/v1/nodes/${node.nodeId}`, 'PATCH', {
    state: node.state === 'enabled' ? 'disabled' : 'enabled',
  }, node.version))

  const toggle = (row: NonNullable<typeof rows>[number]) => nodeId && catalog && mutate(row.id, () => api.mutate(
    `/api/v1/nodes/${nodeId}/${section.toLowerCase()}/${row.id}`, 'PUT', resourceBody(row, row.state === 'enabled' ? 'disabled' : 'enabled'), catalog.version,
  ))

  const remove = (row: NonNullable<typeof rows>[number]) => {
    if (!nodeId || !catalog || !window.confirm(`Remove ${row.name}? Assigned resources must be detached first.`)) return
    void mutate(row.id, () => api.mutate(`/api/v1/nodes/${nodeId}/${section.toLowerCase()}/${row.id}`, 'DELETE', undefined, catalog.version))
  }

  return <div className="resource-view"><div className="page-heading"><h1>{section}</h1><p>Manage BoardProxy {section.toLowerCase()} through versioned desired state.</p></div>
    {error ? <div className="inline-alert critical">{error}</div> : null}
    <section className="panel"><div className="panel-heading"><h2>{section}</h2>{(section === 'Users' || section === 'Boards') ? <button className="primary-button" onClick={() => setEditor(section.slice(0, -1) as ResourceKind)}>Add {section.slice(0, -1)}</button> : null}</div>
      {section === 'Nodes' ? <table><thead><tr><th>Node</th><th>State</th><th>Boards</th><th>Users</th><th>Version</th><th/></tr></thead><tbody>{nodes.map(node => <tr key={node.nodeId}><td>{node.name}<small>{node.nodeId}</small></td><td><span className={node.state === 'enabled' ? 'status ok' : 'status warn'}>● {node.state}</span></td><td>{node.boards}</td><td>{node.users}</td><td>{node.version}</td><td className="actions"><button disabled={busy === node.nodeId} onClick={() => void toggleNode(node)}>{node.state === 'enabled' ? 'Disable' : 'Enable'}</button></td></tr>)}</tbody></table> : null}
      {rows ? <table><thead><tr><th>Name</th><th>ID</th><th>State</th><th>Limits</th><th>Version</th><th/></tr></thead><tbody>{rows.map(row => <tr key={row.id}><td>{row.name}</td><td className="mono">{row.id}</td><td><span className={row.state === 'enabled' ? 'status ok' : 'status warn'}>● {row.state}</span></td><td>{'maxSessions' in row ? `${row.maxSessions} sessions · ${row.maxLanes} lanes` : `${row.maxLanes} lanes`}</td><td>{row.version}</td><td className="actions"><button disabled={busy === row.id} onClick={() => void toggle(row)}>{row.state === 'enabled' ? 'Disable' : 'Enable'}</button><button className="danger-link" disabled={busy === row.id} onClick={() => remove(row)}>Remove</button></td></tr>)}</tbody></table> : null}
      {section === 'Traffic' ? <div className="empty-state"><strong>Traffic policies</strong><span>Hourly rollups, retention, per-user quotas and interface/user series are active. Select Overview for live charts.</span></div> : null}
      {section === 'Access' ? <div className="empty-state"><strong>Protected access surface</strong><span>API-token and node-certificate endpoints are available to administrators. Secrets are shown only once when issued.</span></div> : null}
    </section>
    {editor && nodeId && catalog ? <ResourceEditor kind={editor} nodeId={nodeId} version={catalog.version} api={api} onClose={() => setEditor(undefined)} onSaved={async () => { setEditor(undefined); await onChanged() }}/>: null}
  </div>
}

function ResourceEditor({ kind, nodeId, version, api, onClose, onSaved }: { kind: ResourceKind; nodeId: string; version: number; api: ControlApi; onClose: () => void; onSaved: () => Promise<void> }) {
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault(); setBusy(true); setError(undefined)
    const data = new FormData(event.currentTarget)
    const id = String(data.get('id') ?? '').trim()
    const common = { name: String(data.get('name') ?? '').trim(), state: 'enabled', maxLanes: Number(data.get('maxLanes')) }
    const body = kind === 'Board'
      ? { ...common, hash: String(data.get('hash') ?? '').trim() }
      : { ...common, privateKey: String(data.get('privateKey') ?? '').trim(), maxSessions: Number(data.get('maxSessions')) }
    try {
      await api.mutate(`/api/v1/nodes/${nodeId}/${kind.toLowerCase()}s/${id}`, 'PUT', body, version)
      await onSaved()
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Mutation failed'); setBusy(false) }
  }
  return <div className="modal-backdrop" role="presentation" onMouseDown={event => { if (event.target === event.currentTarget) onClose() }}><form className="modal" onSubmit={event => void submit(event)}><div className="panel-heading"><h2>Add {kind}</h2><button type="button" className="icon-button" aria-label="Close" onClick={onClose}>×</button></div>{error ? <div className="inline-alert critical">{error}</div> : null}<label>ID<input name="id" required pattern="[A-Za-z0-9._:]+(-[A-Za-z0-9._:]+)*"/></label><label>Name<input name="name" required/></label>{kind === 'Board' ? <label>Board hash<input name="hash" required/></label> : <><label>Private key<input name="privateKey" type="password" required autoComplete="new-password"/></label><label>Maximum sessions<input name="maxSessions" type="number" min="0" defaultValue="0" required/></label></>}<label>Maximum lanes<input name="maxLanes" type="number" min="1" defaultValue="1" required/></label><div className="modal-actions"><button type="button" onClick={onClose}>Cancel</button><button className="primary-button" disabled={busy}>{busy ? 'Saving…' : `Add ${kind}`}</button></div></form></div>
}

function resourceBody(row: NonNullable<Catalog['users'] | Catalog['boards']>[number], state: string) {
  if ('maxSessions' in row) return { name: row.name, publicKey: row.publicKey, state, maxSessions: row.maxSessions, maxLanes: row.maxLanes }
  return { name: row.name, hash: row.hash, hubSlide: row.hubSlide, apiBase: row.apiBase, guestName: row.guestName, state, maxLanes: row.maxLanes }
}
