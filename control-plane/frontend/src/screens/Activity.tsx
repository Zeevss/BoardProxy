import { useDeferredValue, useState } from 'react'
import type { DashboardData, Language } from '../types'
import { PageHeader } from '../components/AppShell'
import { Panel, Empty } from '../components/UI'
import { eventKind, message } from '../lib/format'

const filters = ['all', 'catalog', 'runtime', 'status', 'security'] as const
export function Activity({ language, data, search }: { language: Language; data: DashboardData; search: string }) {
  const [filter, setFilter] = useState<(typeof filters)[number]>('all')
  const query = useDeferredValue(search.trim().toLowerCase())
  const rows = data.events.filter(event => (filter === 'all' || eventKind(event.type) === filter) && (!query || `${event.type} ${message(event.payload)}`.toLowerCase().includes(query)))
  return <><PageHeader language={language} section="activity"/><div className="filter-bar">{filters.map(item => <button type="button" key={item} className={filter === item ? 'active' : ''} onClick={() => setFilter(item)}>{item}</button>)}</div><Panel>{rows.length ? <div className="table-scroll"><table><thead><tr><th>Time</th><th>Type</th><th>Event</th><th>Revision</th><th>Sequence</th></tr></thead><tbody>{rows.map(event => <tr key={event.eventId}><td className="mono">{new Date(event.occurredAt).toLocaleString()}</td><td><span className={`event-chip ${eventKind(event.type)}`}>{event.type}</span></td><td>{message(event.payload)}</td><td className="mono">{event.runtimeRevision}</td><td className="mono">{event.sequence}</td></tr>)}</tbody></table></div> : <Empty title="No matching events"/>}</Panel></>
}
