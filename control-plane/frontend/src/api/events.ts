import { useEffect } from 'react'
import { useQueryClient, type QueryClient } from '@tanstack/react-query'
import { keys } from './keys'
import { readSession } from './session'

export interface ControlEvent {
  type: string
  aggregateId: string
  payload: Record<string, unknown>
  occurredAt: string
}

/**
 * Какие ключи сбрасывает событие.
 *
 * Событие именно инвалидирует, а не приносит данные: очередь потока на хабе
 * ограничена тысячей и под нагрузкой теряет события, а после переподключения
 * панель не знает, что пропустила. Источником правды его считать нельзя —
 * поводом перечитать можно.
 *
 * Типов ровно три, и они не совпадают с действиями в журнале: `node.created` и
 * подобные — это `action` в audit_events, наружу они не выходят. В поток
 * попадают только outbox-события (`desired-state.changed`, `quota.changed`) и
 * realtime-уведомление `traffic.quota.exceeded`.
 *
 * Отсюда следствие, о котором стоит помнить: правка, не меняющая ни одного
 * байта TOML, события не порождает вовсе. Свои изменения панель гасит сама
 * через `onSuccess` мутации, а чужие подхватит ближайший опрос агентов или
 * возврат фокуса на вкладку.
 */
export function invalidate(client: QueryClient, event: ControlEvent): void {
  const drop = (key: readonly unknown[]) => void client.invalidateQueries({ queryKey: key })

  if (event.type === 'desired-state.changed') {
    // Пересборку конфигурации вызывает правка ноды, борда, пользователя или
    // гранта — какая именно, событие не сообщает, поэтому сбрасываем всё, что
    // могло её отразить.
    drop(keys.nodes.all)
    drop(keys.agents)
    drop(keys.boards.all)
    drop(keys.users.all)
    const nodeId = event.payload?.nodeId
    if (typeof nodeId === 'string') drop(keys.nodes.config(nodeId))
  }

  if (event.type === 'quota.changed' || event.type === 'traffic.quota.exceeded') {
    drop(keys.users.all)
  }

  // Лента активности показывает всё подряд, поэтому обновляется на любое событие.
  drop(keys.audit.all)
}

/** Разбор кадров SSE: события разделены пустой строкой, поля — префиксами. */
export function parseFrame(frame: string): ControlEvent | null {
  const data = frame
    .split('\n')
    .filter((line) => line.startsWith('data:'))
    .map((line) => line.slice(5).trim())
    .join('')
  if (!data) return null
  try {
    const parsed = JSON.parse(data) as Partial<ControlEvent>
    return parsed.type ? (parsed as ControlEvent) : null
  } catch {
    return null
  }
}

/**
 * Живая инвалидация кэша событиями хаба.
 *
 * Через `fetch`, а не `EventSource`: последний не умеет отправлять заголовки, а
 * поток требует Bearer-токен. Класть токен в query-строку нельзя — он попадёт в
 * логи прокси и историю браузера.
 */
export function useControlEvents(enabled: boolean): void {
  const queryClient = useQueryClient()

  useEffect(() => {
    if (!enabled) return
    const controller = new AbortController()
    let attempt = 0
    let stopped = false
    let timer: ReturnType<typeof setTimeout> | undefined

    async function pump(): Promise<void> {
      const session = readSession()
      if (!session) return

      const response = await fetch('/api/v1/events', {
        headers: { Authorization: `Bearer ${session.token}`, Accept: 'text/event-stream' },
        signal: controller.signal,
      })
      if (!response.ok || !response.body) throw new Error(`поток событий: ${response.status}`)

      attempt = 0
      const reader = response.body.pipeThrough(new TextDecoderStream()).getReader()
      let buffer = ''

      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += value
        // Кадры разделяются пустой строкой; хвост без разделителя — недочитанный
        // кадр, он остаётся в буфере до следующей порции.
        const frames = buffer.split('\n\n')
        buffer = frames.pop() ?? ''
        for (const frame of frames) {
          const event = parseFrame(frame)
          if (event) invalidate(queryClient, event)
        }
      }
    }

    function connect(): void {
      pump().catch(() => undefined).finally(() => {
        if (stopped || controller.signal.aborted) return
        // Разрыв потока — рутина: прокси и балансировщики рвут долгие
        // соединения. Отступ растёт, чтобы упавший хаб не получил шторм.
        attempt += 1
        timer = setTimeout(connect, Math.min(30_000, 1000 * 2 ** Math.min(attempt, 5)))
      })
    }

    connect()

    return () => {
      stopped = true
      if (timer) clearTimeout(timer)
      controller.abort()
    }
  }, [enabled, queryClient])
}
