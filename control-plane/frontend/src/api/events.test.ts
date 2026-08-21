import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { invalidate, parseFrame, type ControlEvent } from './events'

/**
 * Кадр ровно в том виде, в каком его шлёт `FrontendEventStream`: имя события
 * строкой `event:`, тело — `data:`.
 */
function frame(type: string, payload: Record<string, unknown> = {}): string {
  return `event:${type}\ndata:${JSON.stringify({
    type,
    aggregateId: 'x',
    payload,
    occurredAt: '2026-08-21T14:41:05.961905Z',
  })}`
}

describe('разбор кадров SSE', () => {
  it('читает событие целиком', () => {
    const parsed = parseFrame(frame('desired-state.changed', { nodeId: 'de-fra-1', revision: 4 }))
    expect(parsed).toMatchObject({ type: 'desired-state.changed', payload: { nodeId: 'de-fra-1' } })
  })

  it('склеивает data, разбитую на несколько строк', () => {
    const parsed = parseFrame('event:x\ndata:{"type":"quota.changed",\ndata:"payload":{}}')
    expect(parsed?.type).toBe('quota.changed')
  })

  it('пропускает служебные кадры: приветствие и heartbeat', () => {
    expect(parseFrame(':heartbeat')).toBeNull()
    // ready приходит без поля type и данными не является
    expect(parseFrame('event:ready\ndata:{"ready":true}')).toBeNull()
  })

  it('не падает на битом JSON', () => {
    expect(parseFrame('event:x\ndata:{не json')).toBeNull()
  })
})

describe('карта инвалидации', () => {
  function spyOn() {
    const client = new QueryClient()
    const spy = vi.spyOn(client, 'invalidateQueries').mockReturnValue(Promise.resolve())
    const dropped = () => spy.mock.calls.map((call) => JSON.stringify(call[0]?.queryKey))
    return { client, dropped }
  }

  const event = (type: string, payload: Record<string, unknown> = {}): ControlEvent => ({
    type,
    aggregateId: 'x',
    payload,
    occurredAt: '2026-08-21T14:41:05.961905Z',
  })

  /**
   * Тип пришёл из живого потока, а не из списка действий журнала: `node.created`
   * наружу не выходит, и попытка ловить его оставляла панель без обновлений.
   */
  it('desired-state.changed сбрасывает всё, что зависит от конфигурации', () => {
    const { client, dropped } = spyOn()
    invalidate(client, event('desired-state.changed', { nodeId: 'de-fra-1' }))
    const keys = dropped()
    expect(keys).toContain(JSON.stringify(['nodes']))
    expect(keys).toContain(JSON.stringify(['agents']))
    expect(keys).toContain(JSON.stringify(['boards']))
    expect(keys).toContain(JSON.stringify(['users']))
    expect(keys).toContain(JSON.stringify(['nodes', 'config', 'de-fra-1']))
  })

  it('не трогает конфигурацию, когда ноды в payload нет', () => {
    const { client, dropped } = spyOn()
    invalidate(client, event('desired-state.changed'))
    expect(dropped().some((key) => key.includes('config'))).toBe(false)
  })

  it('квотные события задевают только пользователей', () => {
    const { client, dropped } = spyOn()
    invalidate(client, event('traffic.quota.exceeded', { userId: 'u-bob' }))
    const keys = dropped()
    expect(keys).toContain(JSON.stringify(['users']))
    expect(keys).not.toContain(JSON.stringify(['boards']))
  })

  it('журнал обновляется на любое событие', () => {
    const { client, dropped } = spyOn()
    invalidate(client, event('quota.changed'))
    expect(dropped()).toContain(JSON.stringify(['audit']))
  })
})
