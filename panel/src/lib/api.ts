// Типизированная обёртка над управляющим API core (проксируется на /api).
// Все запросы идут с credentials:"include" — авторизация держится на сессионной
// HttpOnly-cookie, которую ставит POST /api/login. На 401 бросаем Unauthorized —
// глобальный обработчик уводит на экран логина.

const BASE = "/api";

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
  serving_boards: string[] | null;
  hubs_up: number;
  per_board: BoardStat[] | null;
}

// ---- Методы ----

export const api = {
  login: (password: string) =>
    request<void>("/login", { method: "POST", body: JSON.stringify({ password }) }),
  logout: () => request<void>("/logout", { method: "POST" }),

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
  createBoard: (id: string, name: string) =>
    request<BoardInfo>("/boards", { method: "POST", body: JSON.stringify({ id, name }) }),
  updateBoard: (id: string, patch: { name?: string; status?: string }) =>
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
