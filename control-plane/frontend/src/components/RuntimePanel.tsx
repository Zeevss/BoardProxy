import type { RuntimeProjection } from '../model'

export function RuntimePanel({ runtime }: { runtime?: RuntimeProjection }) {
  return <section className="panel runtime-panel">
    <h2>Runtime state</h2>
    <h3>User sessions</h3>
    <div className="table-scroll"><table><thead><tr><th>User</th><th>Session ID</th><th>Board</th><th>State</th><th>Started</th></tr></thead><tbody>
      {runtime?.sessions.length ? runtime.sessions.slice(0, 5).map(session => <tr key={session.bundleId}><td>{session.userTag}</td><td className="mono">{short(session.bundleId)}</td><td>{session.boardTag}</td><td><span className="status ok">● Active</span></td><td>{time(session.openedAt)}</td></tr>) : <tr><td colSpan={5} className="empty-row">No active session details</td></tr>}
    </tbody></table></div>
    <h3>Board lifecycle</h3>
    <div className="table-scroll"><table><thead><tr><th>Board</th><th>State</th><th>Error</th></tr></thead><tbody>
      {runtime?.boards.length ? runtime.boards.slice(0, 5).map(board => <tr key={board.boardTag}><td>{board.boardTag}</td><td><span className={stateClass(board.state)}>● {board.state}</span></td><td>{board.error || '—'}</td></tr>) : <tr><td colSpan={3} className="empty-row">No board runtime state</td></tr>}
    </tbody></table></div>
    {runtime && !runtime.sessionDetailsComplete ? <div className="inline-alert">△ Some sessions have incomplete details</div> : null}
    {runtime?.gapDetected ? <div className="inline-alert critical">△ Runtime event gap detected</div> : null}
  </section>
}

function short(value: string) { return value.length > 12 ? `${value.slice(0, 9)}…` : value }
function time(value: string) { return new Date(value).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }) }
function stateClass(state: string) { return ['ready', 'running', 'active'].includes(state.toLowerCase()) ? 'status ok' : 'status warn' }
