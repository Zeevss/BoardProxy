/**
 * Общие доменные типы приложения.
 *
 * Внимание: на данном этапе UI работает на моках, но типы здесь описаны так,
 * чтобы совпасть с будущим реальным бэкендом (Go + Wails bindings или HTTP).
 */

/** Статус туннеля. */
export type TunnelStatus =
  | "disconnected"
  | "connecting"
  | "connected"
  | "reconnecting"
  | "stopping"
  | "error";

/** Уровень лога. */
export type LogLevel = "debug" | "info" | "warn" | "error";

/** Профиль подключения. Ключ — это полная ссылка bproxy://<token>@<boards>#<label>,
 *  которая содержит ключи, адрес сервера и метку. */
export interface Profile {
  id: string;
  name: string;
  /** Полная ссылка подключения bproxy://… (источник правды). */
  key: string;
  /** Заметка пользователя. */
  note?: string;
  /** Когда создан/изменён (мс epoch). */
  updatedAt: number;
}

/** Настройки локального прокси и системной интеграции. */
export interface ProxySettings {
  /** Порт локального mixed-прокси. */
  port: number;
  /** Принудительно SOCKS5/HTTP-прокси на указанном интерфейсе. */
  listenAddr: string;
  /** Прописать локальный SOCKS как системный прокси ОС. */
  systemProxy: boolean;
  /** Полный туннель (TUN): весь трафик ОС через доску. Требует прав. */
  tunMode: boolean;
  /** Максимум физических страниц одного логического подключения. */
  maxLanes: number;
  /** Список доменов/хостов, чьи TCP-стримы идут в обход туннеля. */
  bypassList: string[];
}

/** Одна запись лога. */
export interface LogEntry {
  id: string;
  ts: number;
  level: LogLevel;
  msg: string;
}

/** Точка временного ряда (для спарклайна трафика). */
export interface TrafficPoint {
  ts: number;
  /** байт/с */
  up: number;
  down: number;
}

export interface LaneDebugPoint {
  id: number;
  congestionWindow: number;
  inflight: number;
  peerWindow: number;
  effectiveWindow: number;
  targetPayload: number;
  rttMs: number;
  baseRttMs: number;
  draining: boolean;
}

export interface TransportDebugPoint {
  ts: number;
  rateUp: number;
  rateDown: number;
  rateConfirmedTx: number;
  backlogFrames: number;
  backlogBytes: number;
  blockedWriters: number;
  lanes: LaneDebugPoint[];
}

/** Тема оформления. */
export type Theme = "light" | "dark";

/**
 * TCP-стрим через туннель (для экрана «Статистика»).
 * Активные стримы показываются в начале; неактивные (closedAt !== null)
 * переносятся в конец списка и через короткую задержку удаляются.
 */
export interface TcpStream {
  id: string;
  /** IP:порт цели (в TUN-режиме — всегда IP). */
  target: string;
  /** Домен из DNS-кэша, если известен (для TUN-режима). */
  host?: string;
  /** Активен ли стрим в данный момент. */
  active: boolean;
  /** Время создания (мс epoch). */
  startedAt: number;
  /** Время закрытия (мс epoch) или null, если активен. */
  closedAt: number | null;
  /** Суммарно отправлено байт. */
  totalUp: number;
  /** Суммарно получено байт. */
  totalDown: number;
  /** Текущая скорость отправки (байт/с) — для «живого» индикатора. */
  rateUp: number;
  /** Текущая скорость приёма (байт/с). */
  rateDown: number;
}
