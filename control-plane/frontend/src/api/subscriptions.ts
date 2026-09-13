import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './client'
import { keys } from './keys'
import type { IssuedSubscription, Page, Subscription } from './types'

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
/**
 * Выдача подписки пользователю.
 *
 * Отдельный ресурс, а не свойство пользователя: включение сервиса подписок
 * ничего никому не выдаёт. Ссылку потом можно перечитать через `/link` —
 * хаб хранит токен зашифрованным, — поэтому показывать её один раз не нужно.
 */
export function useCreateSubscription() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: ({ userId, name }: { userId: string; name: string }) =>
      api.post<IssuedSubscription>('/subscriptions', { userId, name }),
    onSuccess: (_data, variables) => {
      void client.invalidateQueries({ queryKey: keys.subscriptions.ofUser(variables.userId) })
      void client.invalidateQueries({ queryKey: keys.users.all })
    },
  })
}

export function useSubscriptionLink(subscriptionId: string | null) {
  return useQuery({
    queryKey: keys.subscriptions.link(subscriptionId ?? ''),
    queryFn: () => api.get<SubscriptionLink>(`/subscriptions/${subscriptionId}/link`),
    enabled: subscriptionId !== null,
  })
}
