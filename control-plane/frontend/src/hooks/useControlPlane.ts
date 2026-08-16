import { startTransition, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ApiError, ControlApi } from '../api/controlApi'
import type { DashboardData } from '../types'

const EMPTY: DashboardData = {
  nodes: [], statuses: {}, runtimes: {}, interfaceTraffic: [], userTraffic: [], interfaceTotals: [], userTotals: [],
  events: [], quotas: [], subscriptions: [], tokens: [], certificates: [], revisions: [],
}

export function useControlPlane(token: string, selectedNode: string | undefined, hours: number) {
  const api = useMemo(() => new ControlApi(token), [token])
  const [data, setData] = useState<DashboardData>(EMPTY)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string>()
  const [unauthorized, setUnauthorized] = useState(false)
  const [streamConnected, setStreamConnected] = useState(false)
  const refreshTimer = useRef<number | undefined>(undefined)

  const refresh = useCallback(async (signal?: AbortSignal) => {
    setLoading(true)
    try {
      const next = await api.dashboard(selectedNode, hours, signal)
      startTransition(() => setData(next))
      setError(undefined)
      setUnauthorized(false)
    } catch (cause) {
      if (!signal?.aborted) {
        setUnauthorized(cause instanceof ApiError && cause.status === 401)
        setError(cause instanceof Error ? cause.message : 'Control plane request failed')
      }
    } finally {
      if (!signal?.aborted) setLoading(false)
    }
  }, [api, hours, selectedNode])

  useEffect(() => {
    const controller = new AbortController()
    // The async refresh updates state only after the network request resolves.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void refresh(controller.signal)
    return () => controller.abort()
  }, [refresh])

  useEffect(() => {
    const controller = new AbortController()
    const listen = async () => {
      let delay = 500
      while (!controller.signal.aborted) {
        try {
          await api.events(() => {
            window.clearTimeout(refreshTimer.current)
            refreshTimer.current = window.setTimeout(() => void refresh(), 250)
          }, controller.signal, () => setStreamConnected(true))
          delay = 500
        } catch { /* retry below */ }
        if (controller.signal.aborted) return
        setStreamConnected(false)
        await new Promise(resolve => window.setTimeout(resolve, delay))
        delay = Math.min(delay * 2, 15_000)
      }
    }
    void listen()
    return () => {
      controller.abort()
      setStreamConnected(false)
      window.clearTimeout(refreshTimer.current)
    }
  }, [api, refresh])

  return { api, data, loading, error, unauthorized, streamConnected, refresh }
}
