import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './client'
import { keys } from './keys'
import type {
  AccessRole,
  ApiToken,
  IssuedApiToken,
  SubscriptionApp,
  SubscriptionService,
  SubscriptionServiceSettings,
} from './types'

export function useSubscriptionService() {
  return useQuery({
    queryKey: keys.subscriptions.service,
    queryFn: () => api.get<SubscriptionService>('/subscription-service'),
    // Состояние сервиса — наблюдаемое: оно меняется само, без правок оператора.
    refetchInterval: 20_000,
  })
}

export interface ServiceDraft {
  enabled: boolean
  serviceName: string
  icon: string
  publicUrl: string
  yandexEditorUrl: string
  recoveryKeyId: string
  apps: SubscriptionApp[]
}

/**
 * Настройки сервиса заменяются целиком под `If-Match` с ревизией.
 *
 * Ревизия здесь не версия строки, а номер конфигурации, которую забирает сам
 * сервис подписок: увеличив её, хаб публикует новую, и сервис её применяет.
 */
export function useUpdateSubscriptionService() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: ({ draft, revision }: { draft: ServiceDraft; revision: number }) =>
      api.put<SubscriptionService>('/subscription-service', draft, { ifMatch: revision }),
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: keys.subscriptions.service })
      // Ссылки подписок собираются из publicUrl — при его правке они меняются.
      void client.invalidateQueries({ queryKey: keys.subscriptions.all })
    },
  })
}

/** Секрет показывается один раз: хаб хранит только его хеш. */
export function useIssueServiceToken() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: () =>
      api.post<{ id: string; name: string; secret: string }>('/subscription-service/token'),
    onSuccess: () => client.invalidateQueries({ queryKey: keys.subscriptions.service }),
  })
}

export function useRestartService() {
  return useMutation({
    mutationFn: () => api.post<void>('/subscription-service/restart'),
  })
}

export function useApiTokens() {
  return useQuery({
    queryKey: keys.access.tokens,
    queryFn: () => api.get<ApiToken[]>('/access/tokens'),
  })
}

export function useIssueApiToken() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (draft: { name: string; role: AccessRole; ttlSeconds?: number }) =>
      api.post<IssuedApiToken>('/access/tokens', {
        name: draft.name,
        role: draft.role,
        ttlSeconds: draft.ttlSeconds ?? null,
      }),
    onSuccess: () => client.invalidateQueries({ queryKey: keys.access.tokens }),
  })
}

export function useRevokeApiToken() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api.delete(`/access/tokens/${id}`),
    onSuccess: () => client.invalidateQueries({ queryKey: keys.access.tokens }),
  })
}

export type { SubscriptionServiceSettings }
