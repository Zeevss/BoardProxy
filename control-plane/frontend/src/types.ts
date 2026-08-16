export type Language = 'en' | 'ru'
export type Section = 'overview' | 'nodes' | 'subscriptions' | 'users' | 'boards' | 'traffic' | 'activity' | 'access'
export type ResourceState = 'enabled' | 'disabled' | 'revoked'

export type PanelUser = { username: string; role: 'ADMIN' }
export type PanelSession = { token: string; expiresAt: string; user: PanelUser }
export type PanelAuthStatus = { setupRequired: boolean }

export type NodeSummary = {
  nodeId: string
  name: string
  state: ResourceState
  boards: number
  users: number
  version: number
  updatedAt: string
}

export type NodeStatus = {
  nodeId: string
  connected: boolean
  bootId?: string
  agentVersion?: string
  coreVersion?: string
  coreRunning: boolean
  coreReady: boolean
  desiredRevision: number
  appliedRevision: number
  configSha256?: string
  lastError?: string
  lastSeen?: string
  fencingToken: number
  version: number
}

export type RuntimeUser = { userTag: string; enabled: boolean; activeSessions: number; rxBytesAtSnapshot: number; txBytesAtSnapshot: number }
export type RuntimeBoard = { boardTag: string; enabled: boolean; state: string; error: string }
export type RuntimeSession = { bundleId: string; userTag: string; boardTag: string; openedAt: string }
export type RuntimeProjection = {
  nodeId: string
  coreBootId?: string
  lastSequence: number
  runtimeRevision: number
  gapDetected: boolean
  lastEventAt?: string
  capturedAt?: string
  sessionDetailsComplete: boolean
  users: RuntimeUser[]
  boards: RuntimeBoard[]
  sessions: RuntimeSession[]
  version: number
}
export type RuntimeEvent = {
  eventId: string
  coreBootId: string
  sequence: number
  runtimeRevision: number
  type: string
  payload: Record<string, unknown>
  occurredAt: string
  receivedAt: string
}

export type TrafficPoint = { bucket: string; subject: string; rxBytes: number; txBytes: number }
export type TrafficTotal = { subject: string; rxBytes: number; txBytes: number }

export type CoreSettings = {
  idleTimeout: string
  allowPrivateEgress: boolean
  transport: { window: number; maxFramePayload: number; streamWindow: number; maxStreamWindow: number; ackTimeout: string; coalesceTarget: number; streamIdleTimeout: string }
  management: { grpcListen: string; httpListen?: string }
  observability: { enabled: boolean; logLevel: string }
}
export type BoardResource = { id: string; name: string; hash: string; hubSlide?: string; apiBase?: string; guestName?: string; state: ResourceState; maxLanes: number; version: number; updatedAt: string }
export type UserResource = { id: string; name: string; publicKey?: string; credentialType: string; state: ResourceState; maxSessions: number; maxLanes: number; version: number; updatedAt: string }
export type Catalog = {
  node: { id: string; name: string; state: ResourceState; core: CoreSettings; version: number; updatedAt: string }
  boards: BoardResource[]
  users: UserResource[]
  assignment: { boardIds: string[]; users: Array<{ userId: string; boardIds: string[] }>; version: number; updatedAt: string }
  version: number
  updatedAt: string
}

export type CatalogMutation = {
  catalog: Catalog
  desiredRevision: number
  configSha256: string
  configChanged: boolean
}

export type SubscriptionKey = { id: string; name: string; nodeId: string; userId: string; position: number }
export type Subscription = { id: string; name: string; recoveryClientPublicKey: string; state: ResourceState; keys: SubscriptionKey[]; version: number; createdAt: string; updatedAt: string }
export type IssuedSubscription = { subscription: Subscription; token: string; recoveryClientPrivateKey: string }
export type ProvisionedUser = { id: string; name: string; deliveryType: 'subscription' | 'keylinks'; subscriptionId?: string; subscriptionUrl?: string; keys: Array<{ id: string; name: string; nodeId: string; keylink: string }> }

export type TrafficQuota = { nodeId: string; userTag: string; period: 'DAILY' | 'MONTHLY'; limitBytes: number; action: 'ALERT' | 'DISABLE'; enabled: boolean; version: number; updatedAt: string }
export type TrafficQuotaUsage = { quota: TrafficQuota; periodStart: string; periodEnd: string; usedBytes: number; exceeded: boolean; exceededAt?: string; enforcedAt?: string }

export type ApiToken = { id: string; name: string; role: 'VIEWER' | 'OPERATOR' | 'ADMIN'; createdBy: string; createdAt: string; expiresAt?: string; revokedAt?: string; lastUsedAt?: string }
export type IssuedApiToken = { token: ApiToken; secret: string }
export type NodeCertificate = { serialNumber: string; nodeId: string; fingerprintSha256: string; issuedAt: string; expiresAt: string; revokedAt?: string; revokedReason?: string; lastSeenAt?: string }
export type CatalogRevision = { nodeId: string; catalogVersion: number; createdAt: string }

export type DashboardData = {
  nodes: NodeSummary[]
  statuses: Record<string, NodeStatus | undefined>
  runtimes: Record<string, RuntimeProjection | undefined>
  catalog?: Catalog
  interfaceTraffic: TrafficPoint[]
  userTraffic: TrafficPoint[]
  interfaceTotals: TrafficTotal[]
  userTotals: TrafficTotal[]
  events: RuntimeEvent[]
  quotas: TrafficQuotaUsage[]
  subscriptions: Subscription[]
  tokens: ApiToken[]
  certificates: NodeCertificate[]
  revisions: CatalogRevision[]
}
