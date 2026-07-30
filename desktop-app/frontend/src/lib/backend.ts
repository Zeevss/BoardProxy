/**
 * Типизированная обёртка над Go-бэкендом (Wails bindings + события).
 *
 * В окне Wails доступны window.go (биндинги) и window.runtime (события). В чистом
 * браузере (vite dev без wails) их нет — тогда методы деградируют безопасно
 * (no-op / reject), чтобы UI не падал.
 */
import { EventsOn } from "../../wailsjs/runtime/runtime";
import {
  Connect,
  Disconnect,
  GetStatus,
  ParseLink,
  Reconnect,
  SetSystemProxyEnabled,
  SyncTray,
  UpdateBypassList,
} from "../../wailsjs/go/main/App";
import type { main } from "../../wailsjs/go/models";
import type { TunnelStatus } from "@/types";

export type Metrics = main.MetricsDTO;
export type StreamMetric = main.StreamDTO;
export type LinkInfo = main.LinkInfo;
export type ConnectConfig = main.ConnectConfig;

/** Запущены ли мы внутри Wails (а не в чистом браузере). */
export function isWails(): boolean {
  return (
    typeof window !== "undefined" &&
    !!(window as any).go &&
    !!(window as any).runtime
  );
}

const noUnsub = () => {};

export const backend = {
  connect(cfg: ConnectConfig): Promise<void> {
    if (!isWails())
      return Promise.reject(new Error("Бэкенд недоступен (браузерный режим)"));
    return Connect(cfg);
  },

  disconnect(): Promise<void> {
    if (!isWails()) return Promise.resolve();
    return Disconnect();
  },

  /** Перезапуск с новым конфигом (смена режима TUN на лету). */
  reconnect(cfg: ConnectConfig): Promise<void> {
    if (!isWails())
      return Promise.reject(new Error("Бэкенд недоступен (браузерный режим)"));
    return Reconnect(cfg);
  },

  /** Включить/выключить системный прокси на лету. */
  setSystemProxyEnabled(enabled: boolean): Promise<void> {
    if (!isWails()) return Promise.resolve();
    return SetSystemProxyEnabled(enabled);
  },

  parseLink(link: string): Promise<LinkInfo> {
    if (!isWails())
      return Promise.reject(new Error("Бэкенд недоступен (браузерный режим)"));
    return ParseLink(link);
  },

  getStatus(): Promise<string> {
    if (!isWails()) return Promise.resolve("disconnected");
    return GetStatus();
  },

  updateBypassList(list: string[]): Promise<void> {
    if (!isWails()) return Promise.resolve();
    return UpdateBypassList(list);
  },

  onStatus(cb: (status: TunnelStatus, error: string) => void): () => void {
    if (!isWails()) return noUnsub;
    return EventsOn("tunnel:status", (d: { status: string; error: string }) =>
      cb(d.status as TunnelStatus, d.error ?? "")
    );
  },

  onMetrics(cb: (m: Metrics) => void): () => void {
    if (!isWails()) return noUnsub;
    return EventsOn("tunnel:metrics", (d: Metrics) => cb(d));
  },

  onLog(cb: (level: string, msg: string) => void): () => void {
    if (!isWails()) return noUnsub;
    return EventsOn("tunnel:log", (d: { level: string; msg: string }) =>
      cb(d.level, d.msg)
    );
  },

  /** Отдаёт трею снимок состояния (статус, профили, активный). */
  syncTray(state: {
    status: string;
    profiles: { id: string; name: string }[];
    activeId: string;
  }): void {
    if (!isWails()) return;
    void SyncTray(state as any);
  },

  /** Клик «Подключить/Отключить» в меню трея. */
  onTrayToggle(cb: () => void): () => void {
    if (!isWails()) return noUnsub;
    return EventsOn("tray:toggle", () => cb());
  },

  /** Выбор профиля в меню трея. */
  onTraySelectProfile(cb: (id: string) => void): () => void {
    if (!isWails()) return noUnsub;
    return EventsOn("tray:selectProfile", (d: { id: string }) => cb(d?.id));
  },
};
