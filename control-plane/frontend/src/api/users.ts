import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, getOrNull } from './client'
import { keys } from './keys'
import type {
  Grant,
  NodeKeylink,
  Page,
  QuotaAction,
  QuotaPeriod,
  ResourceState,
  TrafficQuota,
  TrafficQuotaUsage,
  User,
} from './types'

const PAGE_LIMIT = 200

export function useUsers(query?: string) {
  return useQuery({
    queryKey: keys.users.list(query),
    queryFn: () => api.get<Page<User>>('/users', { query: { query, limit: PAGE_LIMIT } }),
  })
}

/**
 * Гранты с точностью до досок.
 *
 * Список отдаёт только ноды размещения — этого хватает строке таблицы. Полный
 * набор нужен карточке, потому что пустой список досок у ноды означает «все её
 * включённые доски», и затирать его при сохранении нельзя.
 */
export function useGrants(userId: string | null) {
  return useQuery({
    queryKey: keys.users.grants(userId ?? ''),
    queryFn: () => api.get<Grant[]>(`/users/${userId}/grants`),
    enabled: userId !== null,
  })
}

/** Ссылки по нодам. Требует роли OPERATOR и выше. */
export function useKeylinks(userId: string | null, enabled: boolean) {
  return useQuery({
    queryKey: keys.users.keylinks(userId ?? ''),
    queryFn: () => api.get<NodeKeylink[]>(`/users/${userId}/keylinks`),
    enabled: userId !== null && enabled,
  })
}

/** `null` — квоты нет, то есть ограничения нет. Это штатное состояние, не ошибка. */
export function useQuota(userId: string | null) {
  return useQuery({
    queryKey: keys.users.quota(userId ?? ''),
    queryFn: () => getOrNull<TrafficQuotaUsage>(`/users/${userId}/quota`),
    enabled: userId !== null,
  })
}

function invalidateUser(client: ReturnType<typeof useQueryClient>, userId?: string) {
  void client.invalidateQueries({ queryKey: keys.users.all })
  if (userId) void client.invalidateQueries({ queryKey: keys.users.quota(userId) })
  // Правка пользователя пересобирает конфигурацию нод его размещения.
  void client.invalidateQueries({ queryKey: keys.agents })
}

export interface UserDraft {
  id: string
  name: string
  description?: string
  maxSessions?: number
  maxLanes?: number
}

export function useCreateUser() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (draft: UserDraft) =>
      api.post<User>('/users', {
        id: draft.id,
        name: draft.name,
        description: draft.description ?? '',
        state: 'enabled',
        maxSessions: draft.maxSessions ?? 0,
        maxLanes: draft.maxLanes ?? 4,
      }),
    onSuccess: () => invalidateUser(client),
  })
}

/**
 * `PUT` заменяет запись целиком, поэтому неизменённые поля приходится
 * пересылать: отправить одно имя означало бы сбросить лимиты в значения по
 * умолчанию.
 */
export function useUpdateUser() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: ({
      user,
      patch,
    }: {
      user: User
      patch: Partial<Pick<User, 'name' | 'description' | 'state' | 'maxSessions' | 'maxLanes'>>
    }) =>
      api.put<User>(
        `/users/${user.id}`,
        {
          name: patch.name ?? user.name,
          description: patch.description ?? user.description,
          state: (patch.state ?? user.state) as ResourceState,
          maxSessions: patch.maxSessions ?? user.maxSessions,
          maxLanes: patch.maxLanes ?? user.maxLanes,
        },
        { ifMatch: user.version },
      ),
    onSuccess: (_data, variables) => invalidateUser(client, variables.user.id),
  })
}

export function useDeleteUser() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (user: User) => api.delete(`/users/${user.id}`, { ifMatch: user.version }),
    onSuccess: () => invalidateUser(client),
  })
}

/** Размещения заменяются целиком: частичные правки порождали рассинхрон. */
export function useReplaceGrants() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: ({ userId, grants }: { userId: string; grants: Grant[] }) =>
      api.put<Grant[]>(`/users/${userId}/grants`, grants),
    onSuccess: (_data, variables) => {
      void client.invalidateQueries({ queryKey: keys.users.grants(variables.userId) })
      void client.invalidateQueries({ queryKey: keys.users.keylinks(variables.userId) })
      invalidateUser(client, variables.userId)
    },
  })
}

/** Обесценивает все ранее выданные ссылки этого пользователя на всех нодах. */
export function useRotateKey() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (user: User) =>
      api.post<User>(`/users/${user.id}/key/rotate`, undefined, { ifMatch: user.version }),
    onSuccess: (_data, user) => {
      void client.invalidateQueries({ queryKey: keys.users.keylinks(user.id) })
      invalidateUser(client, user.id)
    },
  })
}

export interface QuotaDraft {
  period: QuotaPeriod
  limitBytes: number
  action: QuotaAction
  enabled: boolean
}

/**
 * Хаб требует положительный лимит, поэтому «без ограничения» выражается не
 * нулём, а отсутствием квоты — см. [useDeleteQuota].
 */
export function usePutQuota() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: ({
      userId,
      draft,
      version,
    }: {
      userId: string
      draft: QuotaDraft
      version?: number
    }) =>
      api.put<TrafficQuota>(
        `/users/${userId}/quota`,
        {
          period: draft.period.toLowerCase(),
          limitBytes: draft.limitBytes,
          action: draft.action.toLowerCase(),
          enabled: draft.enabled,
        },
        // Версия только у существующей квоты: при создании If-Match не шлём,
        // иначе хаб отвечает конфликтом.
        version === undefined ? undefined : { ifMatch: version },
      ),
    onSuccess: (_data, variables) => invalidateUser(client, variables.userId),
  })
}

export function useDeleteQuota() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: ({ userId, version }: { userId: string; version: number }) =>
      api.delete(`/users/${userId}/quota`, { ifMatch: version }),
    onSuccess: (_data, variables) => invalidateUser(client, variables.userId),
  })
}
