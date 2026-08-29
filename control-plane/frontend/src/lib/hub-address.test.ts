import { describe, expect, it } from 'vitest'
import { hubAddressProblem, suggestHubAddress } from './hub-address'

describe('подсказка адреса хаба', () => {
  /** Локально имя панели с сертификатом не сойдётся, а `hub` из compose — да. */
  it('для localhost и IP предлагает имя из compose', () => {
    expect(suggestHubAddress({ hostname: 'localhost' })).toBe('hub:8443')
    expect(suggestHubAddress({ hostname: '127.0.0.1' })).toBe('hub:8443')
    expect(suggestHubAddress({ hostname: '10.1.2.3' })).toBe('hub:8443')
  })

  it('для доменного имени берёт его же с портом gRPC', () => {
    expect(suggestHubAddress({ hostname: 'hub.example.net' })).toBe('hub.example.net:8443')
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
