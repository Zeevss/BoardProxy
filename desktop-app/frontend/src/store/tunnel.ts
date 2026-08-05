import { create } from "zustand";
import type {
  LogEntry,
  LogLevel,
  TrafficPoint,
  TransportDebugPoint,
  TunnelStatus,
} from "@/types";
import { uid } from "@/lib/utils";
import { backend, type Metrics } from "@/lib/backend";
import { useProfilesStore } from "@/store/profiles";
import { useSettingsStore } from "@/store/settings";
import { useStreamsStore } from "@/store/streams";

/**
 * Стор туннеля. Управляет подключением через core-бэкенд (Wails) и принимает
 * статус/метрики/логи из событий tunnel:status / tunnel:metrics / tunnel:log.
 */
interface TunnelState {
  status: TunnelStatus;
  connectedAt: number | null;
  /** Текущий RTT (мс). */
  latency: number;
  totalUp: number;
  totalDown: number;
  traffic: TrafficPoint[];
  debug: TransportDebugPoint[];
  logs: LogEntry[];
  logFilter: LogLevel | null;

  connect: () => Promise<void>;
  disconnect: () => Promise<void>;
  toggle: () => void;
  /** Перезапуск с текущими настройками (смена режима TUN на лету). */
  reconnect: () => Promise<void>;
  /** Включить/выключить системный прокси на лету, если подключено. */
  applySystemProxy: (enabled: boolean) => void;
  clearLogs: () => void;
  setLogFilter: (level: LogLevel | null) => void;
  pushLog: (level: LogLevel, msg: string) => void;
}

const MAX_TRAFFIC_POINTS = 60;
const MAX_DEBUG_POINTS = 180;
const MAX_LOGS = 500;

/** Признак активного (или устанавливаемого) подключения. */
function isActiveStatus(s: TunnelStatus): boolean {
  return (
    s === "connected" ||
    s === "connecting" ||
    s === "reconnecting" ||
    s === "stopping"
  );
}

/** Собирает конфиг подключения из активного профиля и настроек. */
function buildConfig() {
  const profiles = useProfilesStore.getState();
  const profile = profiles.getById(profiles.activeId);
  const settings = useSettingsStore.getState();
  if (!profile || !profile.key.trim()) return null;
  return {
    link: profile.key.trim(),
    listen: `${settings.listenAddr}:${settings.port}`,
    systemProxy: settings.systemProxy,
    tunMode: settings.tunMode,
    bypassList: settings.bypassList,
    maxLanes: settings.maxLanes,
  };
}

/** Приводит уровень из slog ("INFO"/"DEBUG"/...) к LogLevel. */
function normalizeLevel(level: string): LogLevel {
  const l = level.toLowerCase();
  if (l === "debug" || l === "info" || l === "warn" || l === "error") return l;
  if (l.startsWith("warn")) return "warn";
  if (l.startsWith("err")) return "error";
  return "info";
}

export const useTunnelStore = create<TunnelState>((set, get) => ({
  status: "disconnected",
  connectedAt: null,
  latency: 0,
  totalUp: 0,
  totalDown: 0,
  traffic: [],
  debug: [],
  logs: [
    { id: uid("l_"), ts: Date.now(), level: "info", msg: "Готов к подключению. Выберите профиль." },
  ],
  logFilter: null,

  connect: async () => {
    if (isActiveStatus(get().status)) return;
    const cfg = buildConfig();
    if (!cfg) {
      get().pushLog("error", "Профиль не выбран или без ключа подключения.");
      return;
    }
    set({ status: "connecting", totalUp: 0, totalDown: 0, traffic: [], debug: [] });
    get().pushLog("info", "Запуск BoardProxy...");
    try {
      await backend.connect(cfg);
    } catch (e) {
      set({ status: "error" });
      get().pushLog("error", e instanceof Error ? e.message : String(e));
    }
  },

  reconnect: async () => {
    if (!isActiveStatus(get().status)) return;
    const cfg = buildConfig();
    if (!cfg) return;
    set({ status: "connecting", traffic: [], debug: [] });
    get().pushLog("info", "Смена режима — переподключение…");
    try {
      await backend.reconnect(cfg);
    } catch (e) {
      set({ status: "error" });
      get().pushLog("error", e instanceof Error ? e.message : String(e));
    }
  },

  applySystemProxy: (enabled) => {
    if (isActiveStatus(get().status)) backend.setSystemProxyEnabled(enabled);
  },

  disconnect: async () => {
    set({ status: "stopping" });
    get().pushLog("info", "Остановка туннеля...");
    try {
      await backend.disconnect();
    } catch (e) {
      set({ status: "error" });
      get().pushLog("error", e instanceof Error ? e.message : String(e));
    }
  },

  toggle: () => {
    const s = get().status;
    if (
      s === "connected" ||
      s === "connecting" ||
      s === "reconnecting" ||
      s === "stopping"
    ) void get().disconnect();
    else get().connect();
  },

  clearLogs: () => set({ logs: [] }),

  setLogFilter: (logFilter) => set({ logFilter }),

  pushLog: (level, msg) =>
    set((s) => ({
      logs: [...s.logs, { id: uid("l_"), ts: Date.now(), level, msg }].slice(-MAX_LOGS),
    })),
}));

/** Применяет снапшот метрик к стору туннеля. */
function applyMetrics(m: Metrics) {
  const ts = Date.now();
  const point: TrafficPoint = { ts, up: m.rateUp, down: m.rateDown };
  const debugPoint: TransportDebugPoint = {
    ts,
    rateUp: m.rateUp,
    rateDown: m.rateDown,
    rateConfirmedTx: m.rateConfirmedTx ?? 0,
    backlogFrames: m.backlogFrames ?? 0,
    backlogBytes: m.backlogBytes ?? 0,
    blockedWriters: m.blockedWriters ?? 0,
    lanes: (m.lanes ?? []).map((lane) => ({
      id: lane.id,
      congestionWindow: lane.congestionWindow,
      inflight: lane.inflight,
      peerWindow: lane.peerWindow,
      effectiveWindow: lane.effectiveWindow,
      targetPayload: lane.targetPayload,
      rttMs: lane.rttMs,
      baseRttMs: lane.baseRttMs,
      draining: lane.draining,
    })),
  };
  useTunnelStore.setState((s) => ({
    latency: m.rttMs,
    totalUp: m.totalUp,
    totalDown: m.totalDown,
    traffic: [...s.traffic, point].slice(-MAX_TRAFFIC_POINTS),
    debug: [...s.debug, debugPoint].slice(-MAX_DEBUG_POINTS),
  }));
  useStreamsStore.getState().applyMetrics(m.streams);
}

// --- Подписка на события бэкенда (один раз на загрузку модуля) ---
backend.onStatus((status, error) => {
  const store = useTunnelStore.getState();
  if (status === "connected") {
    useTunnelStore.setState({ status, connectedAt: Date.now() });
    store.pushLog("info", "BoardProxy подключён. Локальный прокси готов.");
  } else if (status === "reconnecting") {
    useTunnelStore.setState({ status });
    store.pushLog("warn", "Соединение потеряно. Выполняется переподключение…");
  } else if (status === "stopping") {
    useTunnelStore.setState({ status });
    store.pushLog("info", "Остановка BoardProxy…");
  } else if (status === "disconnected") {
    useTunnelStore.setState({ status, connectedAt: null, latency: 0 });
    useStreamsStore.getState().reset();
    store.pushLog("info", "Туннель остановлен.");
  } else if (status === "error") {
    useTunnelStore.setState({ status, connectedAt: null });
    useStreamsStore.getState().reset();
    if (error) store.pushLog("error", error);
  } else {
    useTunnelStore.setState({ status });
  }
});

backend.onMetrics(applyMetrics);

backend.onLog((level, msg) => {
  useTunnelStore.getState().pushLog(normalizeLevel(level), msg);
});
