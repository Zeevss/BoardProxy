import type { NodeSummary } from '../model'

export function Topbar({ nodes, selected, onSelect, streamConnected }: {
  nodes: NodeSummary[]
  selected?: string
  onSelect: (id: string) => void
  streamConnected: boolean
}) {
  return <header className="topbar">
    <label className="node-select"><span>Selected node</span><select value={selected ?? ''} onChange={event => onSelect(event.target.value)}>
      {nodes.length === 0 ? <option value="">No nodes</option> : null}
      {nodes.map(node => <option key={node.nodeId} value={node.nodeId}>{node.name}</option>)}
    </select></label>
    <div className="top-status"><span className="status ok">● Live</span><span className="separator"/><span>Event stream</span><span className={streamConnected ? 'status ok' : 'status warn'}>◉ {streamConnected ? 'Connected' : 'Reconnecting'}</span></div>
  </header>
}
