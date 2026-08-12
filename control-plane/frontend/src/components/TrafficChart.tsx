import { useMemo, useState } from 'react'
import type { TrafficPoint } from '../model'

export function TrafficChart({ interfacePoints, userPoints }: { interfacePoints: TrafficPoint[]; userPoints: TrafficPoint[] }) {
  const [mode, setMode] = useState<'Interface traffic' | 'User payload'>('Interface traffic')
  const points = mode === 'Interface traffic' ? interfacePoints : userPoints
  const path = useMemo(() => chart(points), [points])
  return <section className="panel traffic-panel">
    <div className="panel-heading"><h2>Traffic</h2><select aria-label="Traffic interval" defaultValue="hour"><option value="hour">Last 1 hour</option></select></div>
    <div className="segments"><button className={mode === 'Interface traffic' ? 'selected' : ''} onClick={() => setMode('Interface traffic')}>Interface traffic</button><button className={mode === 'User payload' ? 'selected' : ''} onClick={() => setMode('User payload')}>User payload</button></div>
    <div className="chart-label">Bytes / 5 min</div>
    <svg className="chart" viewBox="0 0 640 250" role="img" aria-label="Traffic over the last hour">
      {[35, 90, 145, 200].map(y => <line key={y} x1="48" x2="610" y1={y} y2={y} className="grid-line" />)}
      <path d={path.rx} className="line rx"/><path d={path.tx} className="line tx"/>
      {path.empty ? <text x="320" y="125" textAnchor="middle" className="empty-chart">No traffic samples</text> : null}
    </svg>
    <div className="legend"><span><i className="cyan"/>In</span><span><i className="green"/>Out</span></div>
  </section>
}

function chart(points: TrafficPoint[]) {
  if (points.length === 0) return { rx: '', tx: '', empty: true }
  const grouped = new Map<string, { rx: number; tx: number }>()
  for (const point of points) {
    const value = grouped.get(point.bucket) ?? { rx: 0, tx: 0 }
    value.rx += point.rxBytes; value.tx += point.txBytes; grouped.set(point.bucket, value)
  }
  const values = [...grouped.values()]
  const max = Math.max(1, ...values.flatMap(value => [value.rx, value.tx]))
  const line = (key: 'rx' | 'tx') => values.map((value, index) => {
    const x = 48 + (index * 562) / Math.max(1, values.length - 1)
    const y = 220 - (value[key] / max) * 190
    return `${index === 0 ? 'M' : 'L'}${x.toFixed(1)} ${y.toFixed(1)}`
  }).join(' ')
  return { rx: line('rx'), tx: line('tx'), empty: false }
}
