import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, getOrNull } from './client'
import { ConflictError, NotFoundError, RateLimitedError, UnauthorizedError } from './errors'
import { readSession, writeSession } from './session'

function respond(status: number, body?: unknown, statusText = ''): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    statusText,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
  })
}

const fetchMock = vi.fn()

beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock)
  sessionStorage.clear()
})

afterEach(() => {
  fetchMock.mockReset()
  vi.unstubAllGlobals()
})

describe('клиент API', () => {
  it('подставляет токен сессии в Authorization', async () => {
    writeSession({
      token: 'tok-1',
      expiresAt: new Date(Date.now() + 3_600_000).toISOString(),
      user: { username: 'admin', role: 'ADMIN' },
    })
    fetchMock.mockResolvedValue(respond(200, { ok: true }))

    await api.get('/nodes')

    const headers = fetchMock.mock.calls[0][1].headers as Headers
    expect(headers.get('Authorization')).toBe('Bearer tok-1')
  })

  it('оборачивает версию в кавычки для If-Match', async () => {
    fetchMock.mockResolvedValue(respond(200, { ok: true }))

    await api.put('/nodes/n1', { name: 'x' }, { ifMatch: 7 })

    const headers = fetchMock.mock.calls[0][1].headers as Headers
    expect(headers.get('If-Match')).toBe('"7"')
  })

  it('опускает пустые параметры запроса', async () => {
    fetchMock.mockResolvedValue(respond(200, []))

    await api.get('/users', { query: { query: '', nodeId: 'n1', limit: 50, state: undefined } })

    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/users?nodeId=n1&limit=50')
  })

  it('на 401 гасит сессию — иначе панель осталась бы с мёртвым токеном', async () => {
    writeSession({
      token: 'tok-1',
      expiresAt: new Date(Date.now() + 3_600_000).toISOString(),
      user: { username: 'admin', role: 'ADMIN' },
    })
    fetchMock.mockResolvedValue(respond(401, { status: 401, title: 'Unauthorized' }))

    await expect(api.get('/nodes')).rejects.toBeInstanceOf(UnauthorizedError)
    expect(readSession()).toBeNull()
  })

  it('различает конфликт версий, лимит запросов и отсутствие ресурса', async () => {
    fetchMock.mockResolvedValueOnce(respond(409, { status: 409, detail: 'version changed' }))
    await expect(api.put('/nodes/n1', {}, { ifMatch: 1 })).rejects.toBeInstanceOf(ConflictError)

    fetchMock.mockResolvedValueOnce(respond(412, { status: 412 }))
    await expect(api.put('/nodes/n1', {}, { ifMatch: 1 })).rejects.toBeInstanceOf(ConflictError)

    fetchMock.mockResolvedValueOnce(respond(429, { status: 429 }))
    await expect(api.get('/nodes')).rejects.toBeInstanceOf(RateLimitedError)

    fetchMock.mockResolvedValueOnce(respond(404, { status: 404 }))
    await expect(api.get('/nodes/nope')).rejects.toBeInstanceOf(NotFoundError)
  })

  it('getOrNull отдаёт null на 404 — у квоты это штатное состояние', async () => {
    fetchMock.mockResolvedValue(respond(404, { status: 404 }))
    await expect(getOrNull('/users/u1/quota')).resolves.toBeNull()
  })

  it('getOrNull не глотает прочие ошибки', async () => {
    fetchMock.mockResolvedValue(respond(500, { status: 500, title: 'Boom' }))
    await expect(getOrNull('/users/u1/quota')).rejects.toThrow()
  })

  it('переваривает 204 без тела', async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }))
    await expect(api.delete('/nodes/n1', { ifMatch: 2 })).resolves.toBeUndefined()
  })

  it('не падает, когда ошибка пришла не в JSON', async () => {
    fetchMock.mockResolvedValue(new Response('<html>502</html>', { status: 502, statusText: 'Bad Gateway' }))
    await expect(api.get('/nodes')).rejects.toMatchObject({ status: 502, title: 'Bad Gateway' })
  })
})

describe('сессия', () => {
  it('считает просроченный токен отсутствующим', () => {
    writeSession({
      token: 'stale',
      expiresAt: new Date(Date.now() - 1000).toISOString(),
      user: { username: 'admin', role: 'ADMIN' },
    })
    expect(readSession()).toBeNull()
  })

  it('переживает мусор в хранилище', () => {
    sessionStorage.setItem('boardproxy.panel.session', 'не json')
    expect(readSession()).toBeNull()
  })
})
