import { useQuery } from '@tanstack/react-query'
import { api } from './client'
import { keys } from './keys'
import type { Page, Subscription } from './types'

/** url = null, когда сервис подписок выключен: ссылку тогда собрать не из чего. */
interface SubscriptionLink {
  url: string | null
}

export function useUserSubscriptions(userId: string | null) {
  return useQuery({
    queryKey: keys.subscriptions.ofUser(userId ?? ''),
    queryFn: () => api.get<Page<Subscription>>('/subscriptions', { query: { userId } }),
    enabled: userId !== null,
  })
}

/**
 * Постоянная ссылка подписки.
 *
 * Хаб хранит токен зашифрованным и восстанавливает его на запрос, поэтому
 * ссылка не приходит вместе со списком и запрашивается отдельно.
 */
export function useSubscriptionLink(subscriptionId: string | null) {
  return useQuery({
    queryKey: keys.subscriptions.link(subscriptionId ?? ''),
    queryFn: () => api.get<SubscriptionLink>(`/subscriptions/${subscriptionId}/link`),
    enabled: subscriptionId !== null,
  })
}
