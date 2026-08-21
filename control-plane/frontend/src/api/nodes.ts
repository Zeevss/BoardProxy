import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, getOrNull } from './client'
import { keys } from './keys'
import type {
  Agent,
  AppliedConfig,
  Board,
  IssuedEnrollmentToken,
  Node,
  Page,
  ResourceState,
  RuntimeEvent,
} from './types'

/** Верхняя граница страницы у хаба — 200; панель читает флот целиком. */
const PAGE_LIMIT = 200

export function useNodes(query?: string) {
  return useQuery({
    queryKey: keys.nodes.list(query),
    queryFn: () => api.get<Page<Node>>('/nodes', { query: { query, limit: PAGE_LIMIT } }),
  })
}

/**
 * Наблюдаемое состояние флота. Обновляется чаще списка нод: сам список меняется
 * только правкой оператора, а вот отчёты агентов приходят постоянно.
 */
export function useAgents() {
  return useQuery({
    queryKey: keys.agents,
    queryFn: () => api.get<Agent[]>('/agents'),
    refetchInterval: 15_000,
    staleTime: 10_000,
  })
}

/**
 * Все доски флота одним запросом.
 *
 * Списку нод нужно лишь их количество в каждой строке, и запрашивать доски
 * по ноде значило бы получить запрос на строку — при лимите в 300 запросов в
 * минуту список упёрся бы в него сам.
 */
export function useBoards() {
  return useQuery({
    queryKey: keys.boards.list(),
    queryFn: () => api.get<Page<Board>>('/boards', { query: { limit: PAGE_LIMIT } }),
  })
}

/**
 * Текущая конфигурация ноды. `null` — хаб ещё ничего для неё не публиковал;
 * это нормальное состояние только что созданной ноды, а не ошибка.
 */
export function useNodeConfig(nodeId: string | null) {
  return useQuery({
    queryKey: keys.nodes.config(nodeId ?? ''),
    queryFn: () => getOrNull<AppliedConfig>(`/nodes/${nodeId}/config`),
    enabled: nodeId !== null,
  })
}

export function useNodeEvents(nodeId: string | null) {
  return useQuery({
    queryKey: keys.nodes.events(nodeId ?? ''),
    queryFn: () => api.get<Page<RuntimeEvent>>(`/nodes/${nodeId}/runtime/events`, {
      query: { limit: 100 },
    }),
    enabled: nodeId !== null,
  })
}

/**
 * Правка ноды.
 *
 * `PUT` заменяет запись целиком, поэтому настройки core приходится переслать
 * как есть: отправить один изменённый флаг означало бы затереть транспортные
 * параметры значениями по умолчанию.
 */
export function useUpdateNode() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ node, patch }: { node: Node; patch: Partial<Pick<Node, 'name' | 'state' | 'settings'>> }) =>
      api.put<Node>(
        `/nodes/${node.id}`,
        { name: patch.name ?? node.name, state: patch.state ?? node.state, settings: patch.settings ?? node.settings },
        { ifMatch: node.version },
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: keys.nodes.all })
      void queryClient.invalidateQueries({ queryKey: keys.agents })
    },
  })
}

export function useDeleteNode() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (node: Node) => api.delete(`/nodes/${node.id}`, { ifMatch: node.version }),
    onSuccess: () => {
      // Удаление ноды каскадом уносит её борды, гранты и телеметрию, поэтому
      // сбрасываем всё, что могло их показывать.
      void queryClient.invalidateQueries({ queryKey: keys.nodes.all })
      void queryClient.invalidateQueries({ queryKey: keys.agents })
      void queryClient.invalidateQueries({ queryKey: keys.boards.all })
      void queryClient.invalidateQueries({ queryKey: keys.users.all })
    },
  })
}

export function useCreateNode() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: { id: string; name: string; state?: ResourceState }) =>
      api.post<Node>('/nodes', { id: input.id, name: input.name, state: input.state ?? 'enabled' }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: keys.nodes.all })
      void queryClient.invalidateQueries({ queryKey: keys.agents })
    },
  })
}

/**
 * Одноразовый enrollment-секрет.
 *
 * Живёт пятнадцать минут и показывается ровно один раз — хаб хранит только его
 * хеш. Поэтому результат мутации нельзя просто выбросить в тост.
 */
export function useIssueEnrollmentToken() {
  return useMutation({
    mutationFn: ({ nodeId, hubUrl }: { nodeId: string; hubUrl: string }) =>
      api.post<IssuedEnrollmentToken>(`/nodes/${nodeId}/enrollment-tokens`, {
        hubUrl,
        ttlSeconds: 900,
      }),
  })
}

export function useToggleBoard() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (board: Board) =>
      api.put<Board>(
        `/boards/${board.nodeId}/${board.id}`,
        {
          id: board.id,
          nodeId: board.nodeId,
          name: board.name,
          hash: board.hash,
          hubSlide: board.hubSlide,
          apiBase: board.apiBase,
          guestName: board.guestName,
          state: board.state === 'enabled' ? 'disabled' : 'enabled',
          maxLanes: board.maxLanes,
        },
        { ifMatch: board.version },
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: keys.boards.all })
      // Правка борда публикует новую ревизию конфигурации ноды.
      void queryClient.invalidateQueries({ queryKey: keys.nodes.all })
      void queryClient.invalidateQueries({ queryKey: keys.agents })
    },
  })
}
