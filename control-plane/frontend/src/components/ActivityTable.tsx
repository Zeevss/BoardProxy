import type { RuntimeEvent } from '../model'

export function ActivityTable({ events }: { events: RuntimeEvent[] }) {
  return <section className="panel activity-panel"><h2>Recent activity</h2><div className="table-scroll"><table><thead><tr><th>Time (UTC)</th><th>Event</th><th>Resource</th><th>Status</th></tr></thead><tbody>
    {events.length ? events.slice(0, 8).map(event => <tr key={event.eventId}><td>{new Date(event.occurredAt).toISOString().replace('T', ' ').slice(0, 19)}</td><td>{label(event.type)}</td><td>{resource(event)}</td><td><span className={event.type.includes('reset') ? 'status warn' : 'status ok'}>{event.type.includes('reset') ? '△ Warning' : '✓ Observed'}</span></td></tr>) : <tr><td colSpan={4} className="empty-row">No recent runtime events</td></tr>}
  </tbody></table></div></section>
}

function label(type: string) { return type.split('.').map(word => word[0]?.toUpperCase() + word.slice(1)).join(' ') }
function resource(event: RuntimeEvent) {
  const payload = event.payload
  return String(payload.userTag ?? payload.boardTag ?? payload.tag ?? 'runtime')
}
