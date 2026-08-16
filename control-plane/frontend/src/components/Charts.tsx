import { useMemo } from 'react'
import type { TrafficPoint } from '../types'

type Series = { name: string; color: string; values: number[] }

export function TrafficChart({ points, mode = 'interface', height = 210 }: { points: TrafficPoint[]; mode?: 'interface' | 'user'; height?: number }) {
  const { series, buckets } = useMemo(() => chartData(points, mode), [mode, points])
  const max = Math.max(1, ...series.flatMap(item => item.values))
  return <div className="chart-wrap">
    <svg className="chart" viewBox={`0 0 720 ${height}`} role="img" aria-label="Traffic over time">
      {[0, .25, .5, .75, 1].map(value => <line key={value} x1="0" x2="720" y1={height * value} y2={height * value} className="chart-grid" />)}
      {series.length ? series.map(item => <g key={item.name}><path d={area(item.values, 720, height, max)} fill={item.color} opacity=".12"/><path d={line(item.values, 720, height, max)} fill="none" stroke={item.color} strokeWidth="2" strokeLinejoin="round"/></g>) : <text x="360" y={height / 2} textAnchor="middle" className="chart-empty">No traffic samples</text>}
    </svg>
    <div className="chart-axis"><span>{formatTick(buckets[0])}</span><span>{formatTick(buckets[Math.floor(buckets.length / 2)])}</span><span>{buckets.length ? 'now' : ''}</span></div>
    <div className="chart-legend">{series.map(item => <span key={item.name}><i style={{ background: item.color }}/>{item.name}</span>)}</div>
  </div>
}

export function MiniSpark({ values, color = '#4fd1a5' }: { values: number[]; color?: string }) {
  const max = Math.max(1, ...values)
  return <svg className="spark" viewBox="0 0 86 24"><path d={line(values, 86, 24, max)} fill="none" stroke={color} strokeWidth="1.6"/></svg>
}

export function Progress({ value, color = '#4fd1a5' }: { value: number; color?: string }) {
  return <div className="progress"><span style={{ width: `${Math.min(100, Math.max(0, value))}%`, background: color }}/></div>
}

// Shared with the small unit test below the component surface.
// eslint-disable-next-line react-refresh/only-export-components
export function chartData(points: TrafficPoint[], mode: 'interface' | 'user') {
  const bucketSet = new Set(points.map(point => point.bucket))
  const buckets = [...bucketSet].sort()
  const index = new Map(buckets.map((bucket, position) => [bucket, position]))
  if (mode === 'interface') {
    const rx = new Array(buckets.length).fill(0) as number[]
    const tx = new Array(buckets.length).fill(0) as number[]
    for (const point of points) { const position = index.get(point.bucket); if (position !== undefined) { rx[position] += point.rxBytes; tx[position] += point.txBytes } }
    return { buckets, series: buckets.length ? [{ name: 'Received', color: '#4fd1a5', values: rx }, { name: 'Sent', color: '#8b7bf7', values: tx }] satisfies Series[] : [] }
  }
  const subjects = [...new Set(points.map(point => point.subject))].slice(0, 5)
  const palette = ['#4fd1a5', '#5aa9e6', '#8b7bf7', '#f0b429', '#f2635f']
  const series = subjects.map((subject, position) => {
    const values = new Array(buckets.length).fill(0) as number[]
    for (const point of points) if (point.subject === subject) { const bucket = index.get(point.bucket); if (bucket !== undefined) values[bucket] += point.rxBytes + point.txBytes }
    return { name: subject, color: palette[position], values }
  })
  return { buckets, series }
}

function line(values: number[], width: number, height: number, max: number) {
  if (!values.length) return ''
  const step = values.length === 1 ? 0 : width / (values.length - 1)
  return values.map((value, index) => `${index ? 'L' : 'M'}${(index * step).toFixed(1)} ${(height - value / max * height).toFixed(1)}`).join(' ')
}
function area(values: number[], width: number, height: number, max: number) { return values.length ? `${line(values, width, height, max)} L${width} ${height} L0 ${height} Z` : '' }
function formatTick(value?: string) { return value ? new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' }).format(new Date(value)) : '' }
