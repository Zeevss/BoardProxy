import {
  ApiError,
  ConflictError,
  NotFoundError,
  type Problem,
  RateLimitedError,
  UnauthorizedError,
} from './errors'
import { clearSession, readSession } from './session'

export interface RequestOptions {
  /**
   * Версия сущности для оптимистичной блокировки.
   *
   * Кэша ETag тут намеренно нет: хаб кладёт версию прямо в тело ответа
   * (`version` у сущностей, `revision` у настроек сервиса подписок), поэтому
   * вызывающий передаёт её из объекта, который только что читал. Отдельный кэш
   * заголовков был бы вторым источником правды о том же числе.
   */
  ifMatch?: number | string
  signal?: AbortSignal
  query?: Record<string, string | number | boolean | undefined | null>
}

/** Все пути относительные: панель и API отдаёт один и тот же хаб. */
const BASE = '/api/v1'

function buildUrl(path: string, query?: RequestOptions['query']): string {
  const url = `${BASE}${path}`
  if (!query) return url
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null || value === '') continue
    params.set(key, String(value))
  }
  const search = params.toString()
  return search ? `${url}?${search}` : url
}

async function toProblem(response: Response): Promise<Problem> {
  const fallback: Problem = {
    status: response.status,
    title: response.statusText || 'Request failed',
  }
  try {
    const body = (await response.json()) as Partial<Problem>
    return {
      // Код ответа берём из самого ответа: тело может его не нести, а вот
      // разойтись с ним — вполне.
      status: fallback.status,
      title: body.title ?? fallback.title,
      detail: body.detail,
      instance: body.instance,
    }
  } catch {
    return fallback
  }
}

function fail(problem: Problem): never {
  switch (problem.status) {
    case 401:
      // Сессии больше нет — гасим её здесь, чтобы это не пришлось помнить в
      // каждом вызове. Подписчики уведут пользователя на экран входа.
      clearSession()
      throw new UnauthorizedError(problem.detail)
    case 404:
      throw new NotFoundError(problem.detail)
    case 409:
    case 412:
      throw new ConflictError(problem.status, problem.detail)
    case 429:
      throw new RateLimitedError(problem.detail)
    default:
      throw new ApiError(problem.status, problem.title, problem.detail)
  }
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  options: RequestOptions = {},
): Promise<T> {
  const headers = new Headers({ Accept: 'application/json' })
  const session = readSession()
  if (session) headers.set('Authorization', `Bearer ${session.token}`)
  if (body !== undefined) headers.set('Content-Type', 'application/json')
  if (options.ifMatch !== undefined) headers.set('If-Match', `"${options.ifMatch}"`)

  const response = await fetch(buildUrl(path, options.query), {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: options.signal ?? null,
  })

  if (!response.ok) fail(await toProblem(response))
  if (response.status === 204) return undefined as T

  const text = await response.text()
  return (text ? JSON.parse(text) : undefined) as T
}

export const api = {
  get: <T>(path: string, options?: RequestOptions) => request<T>('GET', path, undefined, options),
  post: <T>(path: string, body?: unknown, options?: RequestOptions) =>
    request<T>('POST', path, body, options),
  put: <T>(path: string, body: unknown, options?: RequestOptions) =>
    request<T>('PUT', path, body, options),
  delete: (path: string, options?: RequestOptions) =>
    request<void>('DELETE', path, undefined, options),
}

/**
 * Как [api.get], но 404 превращается в `null`.
 *
 * У квоты и runtime-снимка «нет записи» — обычное состояние: квота не задана,
 * нода ещё ни разу не отчиталась. Ронять на этом экран нельзя.
 */
export async function getOrNull<T>(path: string, options?: RequestOptions): Promise<T | null> {
  try {
    return await api.get<T>(path, options)
  } catch (error) {
    if (error instanceof NotFoundError) return null
    throw error
  }
}
