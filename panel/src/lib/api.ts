// Типизированная обёртка над управляющим API core (проксируется на /api).
// Все запросы идут с credentials:"include" — авторизация держится на сессионной
// HttpOnly-cookie, которую ставит POST /api/login. На 401 бросаем Unauthorized —
// глобальный обработчик уводит на экран логина.

const BASE = "/api/node";

export class Unauthorized extends Error {
  constructor() {
    super("unauthorized");
    this.name = "Unauthorized";
  }
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(BASE + path, {
    credentials: "include",
    headers: init?.body ? { "Content-Type": "application/json" } : undefined,
    ...init,
  });
  if (resp.status === 401) throw new Unauthorized();
  if (!resp.ok) {
    let msg = `HTTP ${resp.status}`;
    try {
      const body = await resp.json();
      if (body?.error) msg = body.error;
    } catch {
      /* тело не JSON — оставляем статус */
    }
    throw new ApiError(resp.status, msg);
  }
  if (resp.status === 204) return undefined as T;
  const ct = resp.headers.get("Content-Type") ?? "";
  if (ct.includes("application/json")) return (await resp.json()) as T;
  return undefined as T;
}

async function panelRequest<T>(path: string, init?: RequestInit): Promise<T> {
  const resp = await fetch("/api" + path, {
    credentials: "include",
    headers: init?.body ? { "Content-Type": "application/json" } : undefined,
    ...init,
  });
  if (resp.status === 401) throw new Unauthorized();
  if (!resp.ok) {
    let message = `HTTP ${resp.status}`;
    try {
      const body = await resp.json();
      if (body?.error) message = body.error;
    } catch { /* keep status */ }
    throw new ApiError(resp.status, message);
  }
  if (resp.status === 204) return undefined as T;
  return (await resp.json()) as T;
}

export interface NodeInfo {
  id: string;
  name: string;
  host: string;
  port: number;
  tls: boolean;
  key_hint: string;
  created_at: string;
  selected: boolean;
}

export interface NodeStatus {
  online: boolean;
  latency_ms: number;
  checked_at: string;
  error?: string;
}

// ---- Типы ответов (совпадают с internal/mgmt) ----

export interface ClientInfo {
  id: number;
  name: string;
  status: "active" | "disabled";
  public_key: string;
  created_at: string;
  last_seen?: string | null;
  rx_bytes: number;
  tx_bytes: number;
}

export interface CreateClientResponse {
  id: number;
  name: string;
  keylink: string;
}

export interface BoardInfo {
  id: string;
  name: string;
  hub_slide: string;
  status: "active" | "disabled";
  max_lanes: number;
  created_at: string;
}

export interface StreamInfo {
  id: number;
  target: string;
  written: number;
  received: number;
  started_at: string;
}

export interface ConnectionInfo {
  bundle_id?: string;
  lane_id?: number;
  epoch?: number;
  page: string;
  lanes?: LaneInfo[];
  written: number;
  received: number;
  rtt_ms: number;
  streams: StreamInfo[];
}

export interface LaneInfo {
  id: number;
  page: string;
  rtt_ms: number;
}

export interface LogEntry {
  ts: string;
  level: string;
  msg: string;
}

export interface BoardStat {
  id: string;
  name: string;
  clients_online: number;
  free_pages: number;
  rx_bytes: number;
  tx_bytes: number;
  page_cleanup_runs: number;
  page_cleanup_deleted: number;
  page_cleanup_failures: number;
  page_cleanup_quarantined: number;
}

export interface UserStat {
  id: number;
  name: string;
  status: "active" | "disabled";
  online: boolean;
  last_seen?: string | null;
  connections: number;
  lanes: number;
  streams: number;
  rx_bytes: number;
  tx_bytes: number;
  active_rx_bytes: number;
  active_tx_bytes: number;
}

export interface NetworkStat {
  available: boolean;
  scope: string;
  interfaces: string[] | null;
  started_at: string;
  sampled_at: string;
  rx_bytes: number;
  tx_bytes: number;
  rx_bytes_since_start: number;
  tx_bytes_since_start: number;
  rx_bytes_per_second: number;
  tx_bytes_per_second: number;
}

export interface ReconnectRoleStat {
  role: string;
  board: string;
  disconnects_total: number;
  reconnects_total: number;
  reconnect_attempts_failed: number;
  circuit_open_total: number;
  snapshot_objects_total: number;
  snapshot_bytes_total: number;
  reconnects_last_minute: number;
  snapshot_bytes_last_minute: number;
  last_disconnect_at?: string | null;
  last_disconnect_reason?: string;
  last_connected_for_ms: number;
  last_reconnect_at?: string | null;
  last_downtime_ms: number;
  last_snapshot_objects: number;
  last_snapshot_bytes: number;
}

export interface TransportStat {
  started_at: string;
  disconnects_total: number;
  reconnects_total: number;
  reconnect_attempts_failed: number;
  circuit_open_total: number;
  snapshot_objects_total: number;
  snapshot_bytes_total: number;
  reconnects_last_minute: number;
  reconnects_last_five_minutes: number;
  snapshot_bytes_last_minute: number;
  snapshot_bytes_last_five_minutes: number;
  last_disconnect_at?: string | null;
  last_disconnect_reason?: string;
  last_connected_for_ms: number;
  last_reconnect_at?: string | null;
  last_downtime_ms: number;
  last_snapshot_objects: number;
  last_snapshot_bytes: number;
  per_role: ReconnectRoleStat[] | null;
}

export interface ServerStats {
  clients: number;
  clients_active: number;
  clients_online: number;
  boards: number;
  boards_active: number;
  free_pages: number;
  rx_bytes: number;
  tx_bytes: number;
  online_users: number;
  active_connections: number;
  active_lanes: number;
  active_streams: number;
  page_cleanup_runs: number;
  page_cleanup_deleted: number;
  page_cleanup_failures: number;
  page_cleanup_quarantined: number;
  serving_boards: string[] | null;
  hubs_up: number;
  per_board: BoardStat[] | null;
  users: UserStat[] | null;
  network: NetworkStat;
  transport: TransportStat;
}

// ---- Методы ----

export const api = {
  login: (password: string) =>
	panelRequest<void>("/login", { method: "POST", body: JSON.stringify({ password }) }),
  logout: () => panelRequest<void>("/logout", { method: "POST" }),
  session: () => panelRequest<void>("/session"),

  listNodes: () => panelRequest<NodeInfo[]>("/nodes"),
  createNode: (node: { name: string; host: string; port: number; tls: boolean; access_key: string }) =>
    panelRequest<NodeInfo>("/nodes", { method: "POST", body: JSON.stringify(node) }),
  selectNode: (id: string) => panelRequest<void>(`/nodes/${id}/select`, { method: "POST" }),
  deleteNode: (id: string) => panelRequest<void>(`/nodes/${id}`, { method: "DELETE" }),
  nodeStatus: (id: string) => panelRequest<NodeStatus>(`/nodes/${id}/status`),

  stats: () => request<ServerStats>("/stats"),
  logs: (limit = 500) => request<LogEntry[]>(`/logs?limit=${limit}`),

  listClients: () => request<ClientInfo[]>("/clients"),
  createClient: (name: string) =>
    request<CreateClientResponse>("/clients", { method: "POST", body: JSON.stringify({ name }) }),
  updateClient: (id: number, patch: { name?: string; status?: string }) =>
    request<ClientInfo>(`/clients/${id}`, { method: "PATCH", body: JSON.stringify(patch) }),
  deleteClient: (id: number) => request<void>(`/clients/${id}`, { method: "DELETE" }),
  clientConnections: (id: number) => request<ConnectionInfo[]>(`/clients/${id}/connections`),

  listBoards: () => request<BoardInfo[]>("/boards"),
  createBoard: (id: string, name: string, maxLanes: number) =>
	request<BoardInfo>("/boards", { method: "POST", body: JSON.stringify({ id, name, max_lanes: maxLanes }) }),
  updateBoard: (id: string, patch: { name?: string; status?: string; max_lanes?: number }) =>
    request<BoardInfo>(`/boards/${id}`, { method: "PATCH", body: JSON.stringify(patch) }),
  deleteBoard: (id: string) => request<void>(`/boards/${id}`, { method: "DELETE" }),

  restart: () => request<void>("/restart", { method: "POST" }),

  // Импорт бэкапа — multipart с полем backup. Возвращает промис успеха/ошибки.
  async importBackup(file: File): Promise<void> {
    const form = new FormData();
    form.append("backup", file);
    const resp = await fetch(BASE + "/backup", {
      method: "POST",
      credentials: "include",
      body: form,
    });
    if (resp.status === 401) throw new Unauthorized();
    if (!resp.ok && resp.status !== 202) {
      let msg = `HTTP ${resp.status}`;
      try {
        const body = await resp.json();
        if (body?.error) msg = body.error;
      } catch {
        /* пусто */
      }
      throw new ApiError(resp.status, msg);
    }
  },

  // Экспорт бэкапа — прямая ссылка на скачивание (браузер шлёт cookie сам).
  backupURL: () => BASE + "/backup",
};
