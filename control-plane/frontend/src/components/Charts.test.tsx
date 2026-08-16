import { describe, expect, it } from 'vitest'
import { chartData } from './Charts'

describe('chartData', () => {
  it('aggregates interface subjects into rx and tx series', () => {
    const result = chartData([
      { bucket: '2026-01-01T00:00:00Z', subject: 'eth0', rxBytes: 10, txBytes: 4 },
      { bucket: '2026-01-01T00:00:00Z', subject: 'wg0', rxBytes: 6, txBytes: 2 },
    ], 'interface')
    expect(result.series.map(series => series.values)).toEqual([[16], [6]])
  })
})
