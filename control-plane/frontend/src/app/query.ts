import { QueryClient } from '@tanstack/react-query'
import { ApiError, ConflictError, RateLimitedError, UnauthorizedError } from '@/api/errors'

/**
 * Повторять нужно далеко не всё.
 *
 * 401 и 409 повтором не лечатся: первый требует нового входа, второй — свежего
 * чтения. 429 повторять особенно вредно — именно повторы в него и завели.
 * Остаётся сеть и 5xx, где вторая попытка осмысленна.
 */
function shouldRetry(failureCount: number, error: unknown): boolean {
  if (error instanceof UnauthorizedError) return false
  if (error instanceof ConflictError) return false
  if (error instanceof RateLimitedError) return false
  if (error instanceof ApiError && error.status >= 400 && error.status < 500) return false
  return failureCount < 2
}

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: shouldRetry,
        // Панель обновляется событиями из SSE, поэтому агрессивный поллинг ей
        // не нужен: данные считаются свежими, пока не пришло уведомление.
        staleTime: 30_000,
        refetchOnWindowFocus: true,
      },
      mutations: { retry: false },
    },
  })
}
