const STORAGE_KEY = 'boardproxy.panel.session'

export interface PanelUser {
  username: string
  role: string
}

export interface PanelSession {
  token: string
  expiresAt: string
  user: PanelUser
}

/**
 * Сессия живёт в `sessionStorage`, а не в `localStorage`.
 *
 * Токен панели непрозрачный и истекающий; переживать закрытие вкладки ему не
 * нужно, а вкладка, закрытая на чужой машине, не должна оставлять доступ.
 */
const listeners = new Set<(session: PanelSession | null) => void>()

export function readSession(): PanelSession | null {
  const raw = sessionStorage.getItem(STORAGE_KEY)
  if (!raw) return null
  try {
    const parsed = JSON.parse(raw) as PanelSession
    if (!parsed?.token) return null
    // Просроченный токен не отправляем вовсе: 401 всё равно неизбежен, а так
    // экран входа появляется сразу, без мигающей пустой панели.
    if (Date.parse(parsed.expiresAt) <= Date.now()) return null
    return parsed
  } catch {
    return null
  }
}

export function writeSession(session: PanelSession): void {
  sessionStorage.setItem(STORAGE_KEY, JSON.stringify(session))
  listeners.forEach((listener) => listener(session))
}

export function clearSession(): void {
  sessionStorage.removeItem(STORAGE_KEY)
  listeners.forEach((listener) => listener(null))
}

/** Подписка для React: возвращает функцию отписки. */
export function onSessionChange(listener: (session: PanelSession | null) => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}
