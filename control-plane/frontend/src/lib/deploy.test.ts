import { describe, expect, it } from 'vitest'
import { nodeCompose, subscribeCompose, suggestControlPlaneUrl } from './deploy'

describe('nodeCompose', () => {
  it('подставляет секрет в кавычках', () => {
    const compose = nodeCompose({ secret: 'eyJub2RlX2lkIjoiZnJhbmtmdXJ0In0=' })

    expect(compose).toContain('BPROXY_NODE_SECRET: "eyJub2RlX2lkIjoiZnJhbmtmdXJ0In0="')
    expect(compose).toContain('image: ghcr.io/zeevss/boardproxy-node-agent:latest')
  })

  /**
   * Секреты сейчас base64 и кавычек не содержат, но формат может смениться.
   * Сломанный YAML оператор увидел бы уже на своём сервере — и не связал бы
   * ошибку с панелью.
   */
  it('экранирует кавычки и обратные слэши', () => {
    const compose = nodeCompose({ secret: 'a"b\\c' })

    expect(compose).toContain('BPROXY_NODE_SECRET: "a\\"b\\\\c"')
  })

  it('берёт интерфейс по умолчанию и позволяет его заменить', () => {
    expect(nodeCompose({ secret: 's' })).toContain('BPROXY_STATS_INTERFACES: "eth0"')
    expect(nodeCompose({ secret: 's', statsInterface: 'ens3' })).toContain(
      'BPROXY_STATS_INTERFACES: "ens3"',
    )
  })

  /** Без тома нода теряет идентичность при первом же пересоздании контейнера. */
  it('объявляет том для идентичности', () => {
    const compose = nodeCompose({ secret: 's' })

    expect(compose).toContain('- node-data:/var/lib/bproxy-node')
    expect(compose).toMatch(/^volumes:\n {2}node-data:$/m)
  })
})

describe('адрес хаба для сервиса подписок', () => {
  /**
   * Origin панели на localhost указывает внутрь контейнера самого сервиса.
   * Именно так и вышло на стенде: `http://localhost:8080` и бесконечное
   * «dial tcp [::1]:8080: connect: connection refused».
   */
  it('для локальной панели берёт имя из compose хаба', () => {
    expect(suggestControlPlaneUrl({ hostname: 'localhost', origin: 'http://localhost:8080' })).toEqual(
      { url: 'http://hub:8080', sameHost: true },
    )
    expect(suggestControlPlaneUrl({ hostname: '10.1.2.3', origin: 'http://10.1.2.3:8080' })).toEqual(
      { url: 'http://hub:8080', sameHost: true },
    )
  })

  it('для внешнего имени берёт origin как есть', () => {
    expect(
      suggestControlPlaneUrl({ hostname: 'panel.example.net', origin: 'https://panel.example.net' }),
    ).toEqual({ url: 'https://panel.example.net', sameHost: false })
  })
})

describe('subscribeCompose', () => {
  it('подставляет токен и адрес хаба', () => {
    const compose = subscribeCompose({
      token: 'bps_secret',
      controlPlaneUrl: 'https://panel.example.net',
    })

    expect(compose).toContain('SUBSCRIBE_CONTROL_PLANE_TOKEN: "bps_secret"')
    expect(compose).toContain('SUBSCRIBE_CONTROL_PLANE_URL: "https://panel.example.net"')
    expect(compose).toContain('image: ghcr.io/zeevss/boardproxy-subscribe:latest')
  })

  /**
   * Сервис рядом с хабом обязан быть подключён к его сети: иначе он остаётся
   * в своей, где имени `hub` нет, и повторяет «connection refused».
   */
  it('подключает сервис к сети хаба, когда он рядом', () => {
    const compose = subscribeCompose({
      token: 't',
      controlPlaneUrl: 'http://hub:8080',
      sameHost: true,
    })

    expect(compose).toContain('    networks:\n      - hub')
    expect(compose).toContain('name: boardproxy_default')
  })

  it('не добавляет сеть, когда сервис стоит отдельно', () => {
    const compose = subscribeCompose({ token: 't', controlPlaneUrl: 'https://panel.example.net' })

    expect(compose).not.toContain('networks:')
  })

  /** Наружу сервис публикует реверс-прокси, сам он слушает только loopback. */
  it('публикует порт только на loopback', () => {
    expect(subscribeCompose({ token: 't', controlPlaneUrl: 'https://x' })).toContain(
      '- "127.0.0.1:8090:8090"',
    )
    expect(subscribeCompose({ token: 't', controlPlaneUrl: 'https://x', port: 9000 })).toContain(
      '- "127.0.0.1:9000:8090"',
    )
  })
})
