/**
 * Зеркало DTO хаба. Ручное, а не сгенерированное: контрактов немного, а
 * генератор поверх springdoc пришлось бы держать в сборке ради того же объёма.
 */

export interface Page<T> {
  items: T[]
  offset: number
  limit: number
  total: number
}

export type ResourceState = 'enabled' | 'disabled' | 'revoked'

// --- аутентификация ---------------------------------------------------------

export interface PanelAuthStatus {
  setupRequired: boolean
}

export interface PanelSessionResponse {
  token: string
  expiresAt: string
  user: { username: string; role: string }
}

// --- ноды -------------------------------------------------------------------

export interface CoreSettings {
  idleTimeout: string
  allowPrivateEgress: boolean
  window: number
  maxFramePayload: number
  streamWindow: number
  maxStreamWindow: number
  ackTimeout: string
  coalesceTarget: number
  streamIdleTimeout: string
  grpcListen: string
  httpListen: string | null
  observabilityEnabled: boolean
  logLevel: string
}

export interface Node {
  id: string
  name: string
  state: ResourceState
  settings: CoreSettings
  version: number
  updatedAt: string
}

/**
 * Наблюдаемое состояние агента. Ноды и сервис подписок приходят одним списком —
 * различает их `kind`.
 */
export interface Agent {
  id: string
  kind: 'node' | 'subscription-service'
  name: string
  online: boolean
  appliedRevision: number
  desiredRevision: number
  appliedSha256: string | null
  desiredSha256: string | null
  agentVersion: string | null
  coreVersion: string | null
  applyError: string | null
  bootId: string | null
  lastReportAt: string | null
  activeSessions: number
  activeLanes: number
  coreReporting: boolean
  coreSnapshotAt: string | null
}

// --- доски ------------------------------------------------------------------

export interface Board {
  nodeId: string
  id: string
  name: string
  hash: string
  hubSlide: string | null
  apiBase: string | null
  guestName: string | null
  state: ResourceState
  maxLanes: number
  version: number
  updatedAt: string
}

// --- пользователи -----------------------------------------------------------

export interface UserQuota {
  limitBytes: number
  usedBytes: number
  exceeded: boolean
  enabled: boolean
  periodEnd: string
}

export interface User {
  id: string
  name: string
  description: string
  identityFingerprint: string
  hubIssuedKey: boolean
  state: ResourceState
  maxSessions: number
  maxLanes: number
  nodeIds: string[]
  /** null — квота не задана, то есть ограничения нет. */
  quota: UserQuota | null
  lastSeenAt: string | null
  activated: boolean
  version: number
  updatedAt: string
}

/** Пустой `boardIds` означает «все включённые доски ноды». */
export interface Grant {
  nodeId: string
  boardIds: string[]
}

export interface NodeKeylink {
  nodeId: string
  nodeName: string
  keylink: string | null
}

export type QuotaPeriod = 'DAILY' | 'WEEKLY' | 'MONTHLY' | 'NONE'
export type QuotaAction = 'ALERT' | 'RESET' | 'DISABLE'

export interface TrafficQuota {
  userId: string
  period: QuotaPeriod
  limitBytes: number
  action: QuotaAction
  enabled: boolean
  version: number
  updatedAt: string
  counterStart: string | null
}

export interface TrafficQuotaUsage {
  quota: TrafficQuota
  periodStart: string
  periodEnd: string
  usedBytes: number
  exceeded: boolean
}

// --- трафик -----------------------------------------------------------------

export type TrafficKind = 'interface' | 'user'

export interface TrafficTotal {
  subject: string
  rxBytes: number
  txBytes: number
}

export interface TrafficPoint {
  bucket: string
  subject: string
  rxBytes: number
  txBytes: number
}

// --- конфигурация и история -------------------------------------------------

export interface AppliedConfig {
  nodeId: string
  revision: number
  configSha256: string
  toml: string
  updatedAt: string
}

export interface RuntimeSnapshot {
  nodeId: string
  snapshot: Record<string, unknown>
  observedAt: string
}

export interface RuntimeEvent {
  id: number
  type: string
  payload: Record<string, unknown>
  occurredAt: string
  receivedAt: string
}

// --- журнал -----------------------------------------------------------------

export interface AuditEvent {
  id: string
  nodeId: string | null
  actor: string
  action: string
  resourceType: string
  resourceId: string
  resourceVersion: number
  details: Record<string, unknown>
  occurredAt: string
}

// --- доступ -----------------------------------------------------------------

export type AccessRole = 'SUBSCRIBER' | 'VIEWER' | 'OPERATOR' | 'ADMIN'

export interface ApiToken {
  id: string
  name: string
  role: AccessRole
  createdBy: string
  createdAt: string
  expiresAt: string | null
  revokedAt: string | null
  lastUsedAt: string | null
}

export interface IssuedApiToken {
  token: ApiToken
  secret: string
}

export interface IssuedEnrollmentToken {
  nodeSecret: string
  expiresAt: string
}

// --- подписки ---------------------------------------------------------------

export interface Subscription {
  id: string
  name: string
  userId: string
  recoveryClientPublicKey: string
  state: ResourceState
  version: number
  createdAt: string
  updatedAt: string
}

export interface IssuedSubscription {
  subscription: Subscription
  token: string
  recoveryClientPrivateKey: string
  /** null — доставка подписками выключена. */
  url: string | null
}

export interface SubscriptionApp {
  platform: string
  url: string
}

export interface SubscriptionServiceSettings {
  enabled: boolean
  serviceName: string
  icon: string
  publicUrl: string
  yandexEditorUrl: string
  recoveryKeyId: string
  apps: SubscriptionApp[]
  revision: number
  updatedAt: string
}

export interface SubscriptionServiceStatus {
  tokenIssued: boolean
  connected: boolean
  lastSeenAt: string | null
  serviceVersion: string | null
  appliedRevision: number | null
  recoveryWatcherReady: boolean | null
  recoveryPublicKey: string | null
  startedAt: string | null
}

export interface SubscriptionService {
  settings: SubscriptionServiceSettings
  status: SubscriptionServiceStatus
}
