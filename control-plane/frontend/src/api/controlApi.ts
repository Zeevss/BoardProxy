import type { Catalog, DashboardData, NodeStatus, NodeSummary, RuntimeEvent, RuntimeProjection, TrafficPoint } from '../model'

export class ControlApi {
  constructor(private readonly token: string) {}

  async dashboard(nodeId?: string, signal?: AbortSignal): Promise<DashboardData> {
    const nodesPage = await this.get<{ items: NodeSummary[] }>('/api/v1/nodes?limit=200', signal)
    const selected = nodeId ?? nodesPage.items[0]?.nodeId
    if (!selected) return { nodes: [], interfaceTraffic: [], userTraffic: [], events: [] }
    const to = new Date()
    const from = new Date(to.getTime() - 60 * 60 * 1000)
    const query = `from=${encodeURIComponent(from.toISOString())}&to=${encodeURIComponent(to.toISOString())}`
    const [status, runtime, interfaceTraffic, userTraffic, events, catalog] = await Promise.all([
      this.optional<NodeStatus>(`/api/v1/nodes/${selected}/status`, signal),
      this.optional<RuntimeProjection>(`/api/v1/nodes/${selected}/runtime`, signal),
      this.get<TrafficPoint[]>(`/api/v1/nodes/${selected}/traffic/series?kind=interface&bucketSeconds=300&${query}`, signal),
      this.get<TrafficPoint[]>(`/api/v1/nodes/${selected}/traffic/series?kind=user&bucketSeconds=300&${query}`, signal),
      this.get<RuntimeEvent[]>(`/api/v1/nodes/${selected}/runtime/events?limit=20`, signal),
      this.optional<Catalog>(`/api/v1/catalogs/${selected}`, signal),
    ])
    return { nodes: nodesPage.items, status, runtime, interfaceTraffic, userTraffic, events, catalog }
  }

  async get<T>(path: string, signal?: AbortSignal): Promise<T> {
    const response = await fetch(path, { headers: this.headers(), signal })
    if (!response.ok) throw new Error(await problem(response))
    return response.json() as Promise<T>
  }

  async mutate<T>(path: string, method: string, body: unknown, version?: number): Promise<T> {
    const response = await fetch(path, {
      method,
      headers: { ...this.headers(), 'Content-Type': 'application/json', ...(version ? { 'If-Match': `"${version}"` } : {}) },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
    if (!response.ok) throw new Error(await problem(response))
    return response.status === 204 ? (undefined as T) : (response.json() as Promise<T>)
  }

  async events(onEvent: () => void, signal: AbortSignal, onOpen: () => void): Promise<void> {
    const response = await fetch('/api/v1/events', { headers: this.headers(), signal })
    if (!response.ok || !response.body) throw new Error(await problem(response))
    onOpen()
    const reader = response.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    while (!signal.aborted) {
      const { value, done } = await reader.read()
      if (done) return
      buffer += decoder.decode(value, { stream: true })
      const frames = buffer.split('\n\n')
      buffer = frames.pop() ?? ''
      for (const frame of frames) if (frame.includes('data:')) onEvent()
    }
  }

  private async optional<T>(path: string, signal?: AbortSignal): Promise<T | undefined> {
    const response = await fetch(path, { headers: this.headers(), signal })
    if (response.status === 404) return undefined
    if (!response.ok) throw new Error(await problem(response))
    return response.json() as Promise<T>
  }

  private headers() { return { Authorization: `Bearer ${this.token}` } }
}

async function problem(response: Response): Promise<string> {
  const body = await response.json().catch(() => null) as { detail?: string; title?: string } | null
  return body?.detail ?? body?.title ?? `Request failed (${response.status})`
}
