import { useQuery } from '@tanstack/react-query'
import { api } from './client'
import { keys } from './keys'
import type { TrafficKind, TrafficPoint, TrafficTotal } from './types'

const MINUTE = 60_000

/**
 * Окно запроса. Хаб отвергает диапазон длиннее месяца, поэтому задаётся в днях.
 *
 * Правый край округляется до минуты, и та же пара попадает в ключ кэша. Иначе
 * `Date.now()` давал бы новый диапазон на каждый рендер: ключ при этом оставался
 * бы прежним, и в нём лежали бы данные за окно, которое уже не запрашивают.
 */
function window(days: number): { from: string; to: string } {
  const to = new Date(Math.floor(Date.now() / MINUTE) * MINUTE)
  const from = new Date(to.getTime() - days * 86_400_000)
  return { from: from.toISOString(), to: to.toISOString() }
}

export function useTrafficTotals(kind: TrafficKind, days: number, nodeId?: string) {
  const range = window(days)
  return useQuery({
    queryKey: [...keys.traffic.all, 'totals', kind, nodeId ?? '', range.from, range.to] as const,
    queryFn: () => api.get<TrafficTotal[]>('/traffic/totals', { query: { kind, nodeId, ...range } }),
    // Дельты трафика приезжают отчётами агентов, а не мгновенно: перечитывать
    // их чаще, чем раз в минуту, значит гонять запросы вхолостую.
    staleTime: 60_000,
  })
}

export function useTrafficByNode(kind: TrafficKind, days: number) {
  const range = window(days)
  return useQuery({
    queryKey: [...keys.traffic.all, 'by-node', kind, range.from, range.to] as const,
    queryFn: () => api.get<TrafficTotal[]>('/traffic/by-node', { query: { kind, ...range } }),
    staleTime: 60_000,
  })
}

export function useTrafficSeries(kind: TrafficKind, days: number, bucketSeconds: number, nodeId?: string) {
  const range = window(days)
  return useQuery({
    queryKey: [
      ...keys.traffic.all,
      'series',
      kind,
      bucketSeconds,
      nodeId ?? '',
      range.from,
      range.to,
    ] as const,
    queryFn: () =>
      api.get<TrafficPoint[]>('/traffic/series', {
        query: { kind, nodeId, bucketSeconds, ...range },
      }),
    staleTime: 60_000,
  })
}
