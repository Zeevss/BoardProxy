import { afterEach, describe, expect, it, vi } from 'vitest'
import { ControlApi } from './controlApi'

describe('ControlApi', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('sends bearer authentication and optimistic catalog version', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ ok: true }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }))
    vi.stubGlobal('fetch', fetchMock)

    await new ControlApi('secret').mutate('/api/v1/nodes/node-a/users/alice', 'PUT', { name: 'Alice' }, 7)

    const init = fetchMock.mock.calls[0][1] as RequestInit
    expect(init.headers).toMatchObject({ Authorization: 'Bearer secret', 'If-Match': '"7"' })
  })
})
