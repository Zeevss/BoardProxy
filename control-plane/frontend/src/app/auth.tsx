import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api } from '@/api/client'
import { clearSession, onSessionChange, readSession, writeSession, type PanelSession } from '@/api/session'
import type { PanelAuthStatus, PanelSessionResponse } from '@/api/types'

interface AuthContextValue {
  session: PanelSession | null
  login: (username: string, password: string) => Promise<void>
  setup: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<PanelSession | null>(readSession)
  const queryClient = useQueryClient()

  /**
   * Клиент API гасит сессию сам, встретив 401. Подписка нужна, чтобы об этом
   * узнал и React: иначе панель осталась бы на месте, показывая устаревшие
   * данные и получая 401 на каждый следующий запрос.
   */
  useEffect(
    () =>
      onSessionChange((next) => {
        setSession(next)
        if (!next) queryClient.clear()
      }),
    [queryClient],
  )

  const authenticate = useCallback(
    async (path: '/auth/login' | '/auth/setup', username: string, password: string) => {
      const issued = await api.post<PanelSessionResponse>(path, { username, password })
      writeSession(issued)
      setSession(issued)
    },
    [],
  )

  const value = useMemo<AuthContextValue>(
    () => ({
      session,
      login: (username, password) => authenticate('/auth/login', username, password),
      setup: (username, password) => authenticate('/auth/setup', username, password),
      logout: async () => {
        // Промах отзыва на сервере не должен запирать оператора в панели:
        // локальную сессию гасим в любом случае.
        await api.post('/auth/logout').catch(() => undefined)
        clearSession()
        setSession(null)
      },
    }),
    [session, authenticate],
  )

  return <AuthContext value={value}>{children}</AuthContext>
}

export function useAuth(): AuthContextValue {
  const value = useContext(AuthContext)
  if (!value) throw new Error('useAuth вызван вне AuthProvider')
  return value
}

/** Нужен ли экран первичной настройки: на пустой базе администратора ещё нет. */
export function fetchAuthStatus(): Promise<PanelAuthStatus> {
  return api.get<PanelAuthStatus>('/auth/status')
}
