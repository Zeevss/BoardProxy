import { afterEach, describe, expect, it, vi } from 'vitest'
import { ControlApi, PanelAuthApi } from './controlApi'

afterEach(() => vi.unstubAllGlobals())
describe('ControlApi', () => {
  it('adds authorization and If-Match headers to mutations', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)
    await new ControlApi('secret').mutate('/resource', 'PUT', { state: 'enabled' }, 7)
    const init = fetchMock.mock.calls[0][1] as RequestInit
    expect(init.headers).toMatchObject({ Authorization: 'Bearer secret', 'If-Match': '"7"', 'Content-Type': 'application/json' })
  })
})

describe('PanelAuthApi', () => {
  it('sends credentials only to the setup endpoint', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      token: 'bps_secret', expiresAt: '2026-08-17T00:00:00Z', user: { username: 'admin', role: 'ADMIN' },
    }), { status: 201, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)

    await new PanelAuthApi().setup('admin', 'correct horse battery')

    expect(fetchMock).toHaveBeenCalledWith('/api/v1/auth/setup', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ username: 'admin', password: 'correct horse battery' }),
    }))
  })

  it('uses the opaque panel session for me', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ username: 'admin', role: 'ADMIN' }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await new PanelAuthApi().me('bps_secret')

    expect((fetchMock.mock.calls[0][1] as RequestInit).headers).toMatchObject({ Authorization: 'Bearer bps_secret' })
  })
})
