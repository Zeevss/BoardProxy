/** Ответ хаба об ошибке: RFC 7807, как его отдаёт `ApiExceptionHandler`. */
export interface Problem {
  status: number
  title: string
  detail?: string
  instance?: string
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly title: string,
    readonly detail?: string,
  ) {
    super(detail || title)
    this.name = 'ApiError'
  }
}

/**
 * Сессия панели истекла или её отозвали.
 *
 * Отдельным типом, потому что реакция на неё одна на всё приложение — увести на
 * экран входа, — и разбирать код ответа в каждом вызове незачем.
 */
export class UnauthorizedError extends ApiError {
  constructor(detail?: string) {
    super(401, 'Unauthorized', detail)
    this.name = 'UnauthorizedError'
  }
}

/**
 * Версия сущности не совпала: между чтением и записью её кто-то изменил.
 *
 * Хаб держит оптимистичную блокировку на каждой сущности отдельно, поэтому это
 * штатный исход при двух операторах, а не сбой.
 */
export class ConflictError extends ApiError {
  constructor(status: number, detail?: string) {
    super(status, 'Conflict', detail)
    this.name = 'ConflictError'
  }
}

/** Сработал `ApiProtectionFilter` — 300 запросов в минуту на источник. */
export class RateLimitedError extends ApiError {
  constructor(detail?: string) {
    super(429, 'Too Many Requests', detail)
    this.name = 'RateLimitedError'
  }
}

/** Ресурса нет. Для квоты и runtime это нормальное состояние, а не поломка. */
export class NotFoundError extends ApiError {
  constructor(detail?: string) {
    super(404, 'Not Found', detail)
    this.name = 'NotFoundError'
  }
}

export const isNotFound = (error: unknown): boolean => error instanceof NotFoundError
export const isConflict = (error: unknown): boolean => error instanceof ConflictError
