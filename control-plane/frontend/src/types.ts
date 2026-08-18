export type Language = 'en' | 'ru'
export type Section = 'overview' | 'nodes' | 'users' | 'boards' | 'traffic' | 'activity' | 'settings'
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

/** Пользователь как сущность control-plane; размещение по нодам — вложенный список. */
export type FleetUser = {
  id: string
  name: string
  state: ResourceState
  placements: UserPlacement[]
  limits: UserLimits
  subscription?: { id: string; name: string; state: string }
  updatedAt: string
}

export type UserPlacement = {
  nodeId: string
  nodeName: string
  state: ResourceState
  boards: Array<{ id: string; name: string }>
  version: number
}

export type UserLimits = { maxDevices: number; maxPages: number; traffic?: UserTrafficLimit }

export type UserTrafficLimit = {
  limitBytes: number
  usedBytes: number
  period: Lowercase<QuotaPeriod>
  action: Lowercase<QuotaAction>
  enabled: boolean
  exceeded: boolean
  periodStart: string
  periodEnd: string
}

/** Борд вместе со своей нодой: панель показывает весь флот, а не выбранную ноду. */
export type FleetBoard = {
  nodeId: string
  nodeName: string
  nodeState: ResourceState
  id: string
  name: string
  hash: string
  hubSlide?: string
  apiBase?: string
  guestName?: string
  state: ResourceState
  maxLanes: number
  assigned: boolean
  users: number
  version: number
  updatedAt: string
}

export type SubscriptionKey = { id: string; name: string; nodeId: string; userId: string; position: number }
export type Subscription = { id: string; name: string; recoveryClientPublicKey: string; state: ResourceState; keys: SubscriptionKey[]; version: number; createdAt: string; updatedAt: string }
export type IssuedSubscription = { subscription: Subscription; token: string; recoveryClientPrivateKey: string; url?: string }
export type ProvisionedUser = { id: string; name: string; deliveryType: 'subscription' | 'keylinks'; subscriptionId?: string; subscriptionUrl?: string; keys: Array<{ id: string; name: string; nodeId: string; keylink: string }> }

/** NONE — лимит без календарного сброса. */
export type QuotaPeriod = 'DAILY' | 'WEEKLY' | 'MONTHLY' | 'NONE'
/** ALERT — только уведомить, RESET — обнулить счётчик и продолжить, DISABLE — выключить пользователя. */
export type QuotaAction = 'ALERT' | 'RESET' | 'DISABLE'
export type TrafficQuota = { nodeId: string; userTag: string; period: QuotaPeriod; limitBytes: number; action: QuotaAction; enabled: boolean; version: number; updatedAt: string; counterStart?: string }
export type TrafficQuotaUsage = { quota: TrafficQuota; periodStart: string; periodEnd: string; usedBytes: number; exceeded: boolean; exceededAt?: string; enforcedAt?: string }

export const SUBSCRIPTION_PLATFORMS = ['ios', 'android', 'windows', 'macos', 'linux'] as const
export type SubscriptionPlatform = (typeof SUBSCRIPTION_PLATFORMS)[number]

/** Настройки сервиса подписок. Владелец — control-plane, subscribe только забирает их по своему токену. */
export type SubscriptionServiceSettings = {
  enabled: boolean
  serviceName: string
  icon: string
  publicUrl: string
  yandexEditorUrl: string
  recoveryKeyId: string
  apps: Array<{ platform: SubscriptionPlatform; url: string }>
  revision: number
  updatedAt: string
}

/** Проекция состояния subscribe: заполняется, когда сервис приходит за конфигом. */
export type SubscriptionServiceStatus = {
  tokenIssued: boolean
  connected: boolean
  lastSeenAt?: string
  serviceVersion?: string
  appliedRevision?: number
  recoveryWatcherReady?: boolean
  recoveryPublicKey?: string
  startedAt?: string
}

export type SubscriptionService = { settings: SubscriptionServiceSettings; status: SubscriptionServiceStatus }
/** Токен сервиса подписок: секрет возвращается ровно один раз при выпуске. */
export type IssuedServiceToken = { id: string; name: string; secret: string }

export type ApiToken = { id: string; name: string; role: 'SUBSCRIBER' | 'VIEWER' | 'OPERATOR' | 'ADMIN'; createdBy: string; createdAt: string; expiresAt?: string; revokedAt?: string; lastUsedAt?: string }
export type IssuedApiToken = { token: ApiToken; secret: string }
export type NodeCertificate = { serialNumber: string; nodeId: string; fingerprintSha256: string; issuedAt: string; expiresAt: string; revokedAt?: string; revokedReason?: string; lastSeenAt?: string }
/** Применённый TOML без клиентских идентичностей: секреты вырезает бэкенд. */
export type AppliedConfig = {
  nodeId: string
  revision: number
  catalogVersion: number
  configSha256: string
  toml: string
  createdAt: string
}

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
  users: FleetUser[]
  boards: FleetBoard[]
  subscriptions: Subscription[]
  subscriptionService?: SubscriptionService
  tokens: ApiToken[]
  certificates: NodeCertificate[]
  revisions: CatalogRevision[]
}
