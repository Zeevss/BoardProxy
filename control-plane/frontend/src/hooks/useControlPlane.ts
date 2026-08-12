import { startTransition, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ControlApi } from '../api/controlApi'
import type { DashboardData } from '../model'

const EMPTY: DashboardData = { nodes: [], interfaceTraffic: [], userTraffic: [], events: [] }

export function useControlPlane(token: string, selectedNode: string | undefined) {
  const api = useMemo(() => new ControlApi(token), [token])
  const [data, setData] = useState<DashboardData>(EMPTY)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string>()
  const [streamConnected, setStreamConnected] = useState(false)
  const refreshTimer = useRef<number | undefined>(undefined)

  const refresh = useCallback(async (signal?: AbortSignal) => {
    try {
      const next = await api.dashboard(selectedNode, signal)
      startTransition(() => setData(next))
      setError(undefined)
    } catch (cause) {
      if (!signal?.aborted) setError(cause instanceof Error ? cause.message : 'Control plane request failed')
    } finally {
      if (!signal?.aborted) setLoading(false)
    }
  }, [api, selectedNode])

  useEffect(() => {
    const controller = new AbortController()
    // The async refresh only updates state after the network request resolves.
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
        } catch { /* reconnect below; dashboard errors are reported separately */ }
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

  return { api, data, loading, error, streamConnected, refresh }
}
