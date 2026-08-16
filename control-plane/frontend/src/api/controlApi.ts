import type {
  ApiToken, Catalog, CatalogMutation, CatalogRevision, DashboardData, IssuedApiToken, IssuedSubscription,
  NodeCertificate, NodeStatus, NodeSummary, ProvisionedUser, RuntimeEvent, RuntimeProjection,
  PanelAuthStatus, PanelSession, PanelUser, Subscription, TrafficPoint, TrafficQuota, TrafficQuotaUsage, TrafficTotal,
} from '../types'

type Page<T> = { items: T[]; offset: number; limit: number; total: number }

export class PanelAuthApi {
  status(signal?: AbortSignal) { return publicRequest<PanelAuthStatus>('/api/v1/auth/status', undefined, signal) }
  setup(username: string, password: string) { return publicRequest<PanelSession>('/api/v1/auth/setup', { username, password }) }
  login(username: string, password: string) { return publicRequest<PanelSession>('/api/v1/auth/login', { username, password }) }
  me(token: string, signal?: AbortSignal) { return authenticatedRequest<PanelUser>('/api/v1/auth/me', token, undefined, signal) }
  logout(token: string) { return authenticatedRequest<void>('/api/v1/auth/logout', token, {}, undefined, 'POST') }
}

export class ControlApi {
  constructor(private readonly token: string) {}

  async dashboard(nodeId: string | undefined, hours: number, signal?: AbortSignal): Promise<DashboardData> {
    const nodesPage = await this.get<Page<NodeSummary>>('/api/v1/nodes?limit=200', signal)
    const selected = nodeId && nodesPage.items.some(node => node.nodeId === nodeId) ? nodeId : nodesPage.items[0]?.nodeId
    const [statusEntries, runtimeEntries, subscriptions, tokens] = await Promise.all([
      Promise.all(nodesPage.items.map(async node => [node.nodeId, await this.optional<NodeStatus>(`/api/v1/nodes/${encodeURIComponent(node.nodeId)}/status`, signal)] as const)),
      Promise.all(nodesPage.items.map(async node => [node.nodeId, await this.optional<RuntimeProjection>(`/api/v1/nodes/${encodeURIComponent(node.nodeId)}/runtime`, signal)] as const)),
      this.optional<Subscription[]>('/api/v1/subscriptions', signal, [403]),
      this.optional<ApiToken[]>('/api/v1/access/tokens', signal, [403]),
    ])
    const base: DashboardData = {
      nodes: nodesPage.items,
      statuses: Object.fromEntries(statusEntries),
      runtimes: Object.fromEntries(runtimeEntries),
      interfaceTraffic: [], userTraffic: [], interfaceTotals: [], userTotals: [], events: [], quotas: [],
      subscriptions: subscriptions ?? [], tokens: tokens ?? [], certificates: [], revisions: [],
    }
    if (!selected) return base
    const to = new Date()
    const from = new Date(to.getTime() - hours * 60 * 60 * 1000)
    const range = `from=${encodeURIComponent(from.toISOString())}&to=${encodeURIComponent(to.toISOString())}`
    const prefix = `/api/v1/nodes/${encodeURIComponent(selected)}`
    const [catalog, interfaceTraffic, userTraffic, interfaceTotals, userTotals, events, quotas, certificates, revisions] = await Promise.all([
      this.optional<Catalog>(`/api/v1/catalogs/${encodeURIComponent(selected)}`, signal),
      this.get<TrafficPoint[]>(`${prefix}/traffic/series?kind=interface&bucketSeconds=${bucket(hours)}&${range}`, signal),
      this.get<TrafficPoint[]>(`${prefix}/traffic/series?kind=user&bucketSeconds=${bucket(hours)}&${range}`, signal),
      this.get<TrafficTotal[]>(`${prefix}/traffic/interfaces?${range}`, signal),
      this.get<TrafficTotal[]>(`${prefix}/traffic/users?${range}`, signal),
      this.get<RuntimeEvent[]>(`${prefix}/runtime/events?limit=100`, signal),
      this.get<TrafficQuotaUsage[]>(`${prefix}/traffic/quotas`, signal),
      this.get<NodeCertificate[]>(`${prefix}/certificates`, signal),
      this.get<{ items: CatalogRevision[] }>(`${prefix}/revisions?limit=50`, signal),
    ])
    return { ...base, catalog, interfaceTraffic, userTraffic, interfaceTotals, userTotals, events, quotas, certificates, revisions: revisions.items }
  }

  async get<T>(path: string, signal?: AbortSignal): Promise<T> {
    const response = await fetch(path, { headers: this.headers(), signal })
    if (!response.ok) throw new ApiError(response.status, await problem(response))
    return response.json() as Promise<T>
  }

  async mutate<T>(path: string, method: string, body?: unknown, version?: number): Promise<T> {
    const response = await fetch(path, {
      method,
      headers: { ...this.headers(), ...(body === undefined ? {} : { 'Content-Type': 'application/json' }), ...(version === undefined ? {} : { 'If-Match': `"${version}"` }) },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
    if (!response.ok) throw new ApiError(response.status, await problem(response))
    return response.status === 204 ? (undefined as T) : (response.json() as Promise<T>)
  }

  updateNode(nodeId: string, version: number, patch: { name?: string; state?: string }) { return this.mutate(`/api/v1/nodes/${id(nodeId)}`, 'PATCH', patch, version) }
  createCatalog(body: unknown) { return this.mutate<CatalogMutation>('/api/v1/catalogs', 'POST', body) }
  issueEnrollment(nodeId: string, hubUrl: string, ttlSeconds: number) { return this.mutate<{ nodeSecret: string; expiresAt: string }>(`/api/v1/nodes/${id(nodeId)}/enrollment-tokens`, 'POST', { hubUrl, ttlSeconds }) }
  putUser(nodeId: string, userId: string, version: number, body: Record<string, unknown>) { return this.mutate<CatalogMutation>(`/api/v1/nodes/${id(nodeId)}/users/${id(userId)}`, 'PUT', body, version) }
  removeUser(nodeId: string, userId: string, version: number) { return this.mutate<CatalogMutation>(`/api/v1/nodes/${id(nodeId)}/users/${id(userId)}`, 'DELETE', undefined, version) }
  putBoard(nodeId: string, boardId: string, version: number, body: Record<string, unknown>) { return this.mutate<CatalogMutation>(`/api/v1/nodes/${id(nodeId)}/boards/${id(boardId)}`, 'PUT', body, version) }
  removeBoard(nodeId: string, boardId: string, version: number) { return this.mutate<CatalogMutation>(`/api/v1/nodes/${id(nodeId)}/boards/${id(boardId)}`, 'DELETE', undefined, version) }
  replaceAssignment(nodeId: string, version: number, body: unknown) { return this.mutate<CatalogMutation>(`/api/v1/nodes/${id(nodeId)}/assignment`, 'PUT', body, version) }
  replaceCatalog(nodeId: string, version: number, body: unknown) { return this.mutate(`/api/v1/catalogs/${id(nodeId)}`, 'PUT', body, version) }
  rollback(nodeId: string, revision: number, version: number) { return this.mutate(`/api/v1/nodes/${id(nodeId)}/revisions/${revision}/rollback`, 'POST', undefined, version) }
  rebuildRuntime(nodeId: string) { return this.mutate(`/api/v1/nodes/${id(nodeId)}/runtime/rebuild`, 'POST') }
  provisionUser(body: unknown) { return this.mutate<ProvisionedUser>('/api/v1/users', 'POST', body) }
  createSubscription(body: unknown) { return this.mutate<IssuedSubscription>('/api/v1/subscriptions', 'POST', body) }
  replaceSubscription(subscription: Subscription, state: string) { return this.mutate<Subscription>(`/api/v1/subscriptions/${id(subscription.id)}`, 'PUT', { name: subscription.name, state, keys: subscription.keys.map(({ id: keyId, name, nodeId, userId }) => ({ id: keyId, name, nodeId, userId })) }, subscription.version) }
  putQuota(nodeId: string, userTag: string, body: unknown, version?: number) { return this.mutate<TrafficQuota>(`/api/v1/nodes/${id(nodeId)}/traffic/quotas/${id(userTag)}`, 'PUT', body, version) }
  deleteQuota(nodeId: string, userTag: string, version: number) { return this.mutate(`/api/v1/nodes/${id(nodeId)}/traffic/quotas/${id(userTag)}`, 'DELETE', undefined, version) }
  issueToken(body: unknown) { return this.mutate<IssuedApiToken>('/api/v1/access/tokens', 'POST', body) }
  revokeToken(tokenId: string) { return this.mutate(`/api/v1/access/tokens/${id(tokenId)}`, 'DELETE') }
  revokeCertificate(nodeId: string, serial: string, reason: string) { return this.mutate(`/api/v1/nodes/${id(nodeId)}/certificates/${id(serial)}`, 'DELETE', { reason }) }

  async events(onEvent: () => void, signal: AbortSignal, onOpen: () => void): Promise<void> {
    const response = await fetch('/api/v1/events', { headers: this.headers(), signal })
    if (!response.ok || !response.body) throw new ApiError(response.status, await problem(response))
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
      for (const frame of frames) if (frame.split('\n').some(line => line.startsWith('data:'))) onEvent()
    }
  }

  private async optional<T>(path: string, signal?: AbortSignal, ignored = [404]): Promise<T | undefined> {
    const response = await fetch(path, { headers: this.headers(), signal })
    if (ignored.includes(response.status)) return undefined
    if (!response.ok) throw new ApiError(response.status, await problem(response))
    return response.json() as Promise<T>
  }

  private headers() { return { Authorization: `Bearer ${this.token}` } }
}

export class ApiError extends Error { constructor(public readonly status: number, message: string) { super(message) } }
function id(value: string) { return encodeURIComponent(value) }
function bucket(hours: number) { return hours <= 1 ? 60 : hours <= 24 ? 300 : hours <= 168 ? 3600 : 10_800 }
async function problem(response: Response): Promise<string> {
  const body = await response.json().catch(() => null) as { detail?: string; title?: string } | null
  return body?.detail ?? body?.title ?? `Request failed (${response.status})`
}

async function publicRequest<T>(path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const response = await fetch(path, {
    method: body === undefined ? 'GET' : 'POST',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal,
  })
  if (!response.ok) throw new ApiError(response.status, await problem(response))
  return response.json() as Promise<T>
}

async function authenticatedRequest<T>(path: string, token: string, body?: unknown, signal?: AbortSignal, method = 'GET'): Promise<T> {
  const response = await fetch(path, {
    method,
    headers: {
      Authorization: `Bearer ${token}`,
      ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
    },
    body: body === undefined || method === 'GET' ? undefined : JSON.stringify(body),
    signal,
  })
  if (!response.ok) throw new ApiError(response.status, await problem(response))
  return response.status === 204 ? (undefined as T) : (response.json() as Promise<T>)
}
