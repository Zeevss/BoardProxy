import { describe, expect, it } from 'vitest'
import { hubAddressProblem, suggestHubAddress } from './hub-address'

describe('подсказка адреса хаба', () => {
  it('берёт имя, по которому открыта панель, с портом gRPC', () => {
    expect(suggestHubAddress({ hostname: 'hub.example.net' })).toBe('hub.example.net:8443')
    expect(suggestHubAddress({ hostname: 'localhost' })).toBe('localhost:8443')
    expect(suggestHubAddress({ hostname: '10.1.2.3' })).toBe('10.1.2.3:8443')
  })

  /**
   * `hub` — имя сервиса из общего compose. У ноды в отдельном compose своя
   * сеть, и такое имя не резолвится ни во что: агент падает в бесконечный
   * «name resolver error: produced zero addresses». Заведомо нерабочая
   * подсказка хуже неточной.
   */
  it('никогда не предлагает имя сервиса из compose', () => {
    for (const hostname of ['localhost', '127.0.0.1', '10.1.2.3', 'panel.example.net']) {
      expect(suggestHubAddress({ hostname })).not.toBe('hub:8443')
    }
  })
})

describe('разбор адреса хаба', () => {
  /**
   * Ровно те три ошибки, из-за которых нода не регистрировалась: в поле
   * оказывался origin панели — со схемой, с портом 8080 или вовсе без порта.
   */
  it('ловит схему', () => {
    expect(hubAddressProblem('http://127.0.0.1:8080')).toBe('scheme')
    expect(hubAddressProblem('https://hub.example.net:8443')).toBe('scheme')
  })

  it('ловит отсутствие порта', () => {
    expect(hubAddressProblem('hub.example.net')).toBe('port')
  })

  it('ловит пустое значение', () => {
    expect(hubAddressProblem('   ')).toBe('empty')
  })

  it('пропускает корректный host:port', () => {
    expect(hubAddressProblem('hub:8443')).toBeNull()
    expect(hubAddressProblem(' hub.example.net:8443 ')).toBeNull()
  })
})
