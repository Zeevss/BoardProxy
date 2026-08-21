/**
 * Ключи кэша в одном месте.
 *
 * Инвалидация после мутации и после события из SSE обязана попадать в те же
 * ключи, что и чтение. Разъехавшись, они дают самый неприятный сорт багов:
 * панель показывает устаревшее, не подавая виду.
 */
export const keys = {
  auth: {
    status: ['auth', 'status'] as const,
  },
  nodes: {
    all: ['nodes'] as const,
    list: (query?: string) => ['nodes', 'list', query ?? ''] as const,
    one: (id: string) => ['nodes', 'one', id] as const,
    config: (id: string) => ['nodes', 'config', id] as const,
    events: (id: string) => ['nodes', 'events', id] as const,
    certificates: (id: string) => ['nodes', 'certificates', id] as const,
  },
  /** Наблюдаемое состояние всего флота одним запросом. */
  agents: ['agents'] as const,
  boards: {
    all: ['boards'] as const,
    list: (nodeId?: string, query?: string) => ['boards', 'list', nodeId ?? '', query ?? ''] as const,
  },
  users: {
    all: ['users'] as const,
    list: (query?: string, nodeId?: string) => ['users', 'list', query ?? '', nodeId ?? ''] as const,
    grants: (id: string) => ['users', 'grants', id] as const,
    keylinks: (id: string) => ['users', 'keylinks', id] as const,
    quota: (id: string) => ['users', 'quota', id] as const,
  },
  traffic: {
    all: ['traffic'] as const,
  },
  audit: {
    all: ['audit'] as const,
    list: (nodeId?: string) => ['audit', 'list', nodeId ?? ''] as const,
  },
  access: {
    tokens: ['access', 'tokens'] as const,
  },
  subscriptions: {
    all: ['subscriptions'] as const,
    ofUser: (userId: string) => ['subscriptions', 'user', userId] as const,
    link: (id: string) => ['subscriptions', 'link', id] as const,
    service: ['subscription-service'] as const,
  },
} as const
