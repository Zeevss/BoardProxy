import { useQuery } from '@tanstack/react-query'
import { api } from './client'
import { keys } from './keys'
import type { AuditEvent, Page } from './types'

/**
 * Лента изменений.
 *
 * Журнал пишется на каждую правку, поэтому лента обновляется не по таймеру, а
 * инвалидацией из SSE: любое событие потока означает, что кто-то что-то сделал.
 */
export function useAudit(limit = 40, nodeId?: string) {
  return useQuery({
    queryKey: keys.audit.list(nodeId),
    queryFn: () => api.get<Page<AuditEvent>>('/audit', { query: { nodeId, limit } }),
  })
}
