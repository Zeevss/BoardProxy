export type NodeSummary = {
  nodeId: string
  name: string
  state: string
  boards: number
  users: number
  version: number
  updatedAt: string
}

export type NodeStatus = {
  nodeId: string
  connected: boolean
  coreReady: boolean
  coreRunning: boolean
  desiredRevision: number
  appliedRevision: number
  lastSeen?: string
  lastError?: string
}

export type RuntimeUser = {
  userTag: string
  enabled: boolean
  activeSessions: number
  rxBytesAtSnapshot: number
  txBytesAtSnapshot: number
}

export type RuntimeBoard = { boardTag: string; enabled: boolean; state: string; error: string }
export type RuntimeSession = { bundleId: string; userTag: string; boardTag: string; openedAt: string }

export type RuntimeProjection = {
  nodeId: string
  coreBootId?: string
  lastSequence: number
  runtimeRevision: number
  gapDetected: boolean
  sessionDetailsComplete: boolean
  users: RuntimeUser[]
  boards: RuntimeBoard[]
  sessions: RuntimeSession[]
  version: number
}

export type RuntimeEvent = {
  eventId: string
  type: string
  payload: Record<string, unknown>
  occurredAt: string
}

export type TrafficPoint = {
  bucket: string
  subject: string
  rxBytes: number
  txBytes: number
}

export type Catalog = {
  node: { id: string; name: string; state: string; version: number }
  boards: Array<{ id: string; name: string; hash: string; hubSlide?: string; apiBase?: string; guestName?: string; state: string; maxLanes: number; version: number }>
  users: Array<{
    id: string
    name: string
    publicKey?: string
    credentialType?: string
    state: string
    maxSessions: number
    maxLanes: number
    version: number
  }>
  assignment: { boardIds: string[]; users: Array<{ userId: string; boardIds: string[] }> }
  version: number
}

export type DashboardData = {
  nodes: NodeSummary[]
  status?: NodeStatus
  runtime?: RuntimeProjection
  interfaceTraffic: TrafficPoint[]
  userTraffic: TrafficPoint[]
  events: RuntimeEvent[]
  catalog?: Catalog
}
