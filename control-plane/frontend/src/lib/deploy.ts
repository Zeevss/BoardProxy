/**
 * Готовые к запуску docker-compose для ноды и сервиса подписок.
 *
 * Значения подставляются прямо в файл, а не через `${...}` и отдельный .env:
 * секрет показывается один раз, и заставлять оператора перекладывать его между
 * двумя файлами — лишний шаг, на котором и теряются секреты. Те же шаблоны, но
 * с переменными, лежат в репозитории в `deploy/`.
 */

/** Образы публикует CI; `latest` — последний тег релиза. */
const REGISTRY = 'ghcr.io/zeevss'
const TAG = 'latest'

/**
 * Строка для YAML в двойных кавычках.
 *
 * Синтаксис строк JSON — подмножество двойных кавычек YAML, поэтому
 * `JSON.stringify` даёт корректное экранирование и для него. Секреты сейчас
 * base64 и кавычек не содержат, но полагаться на это нельзя: формат токена
 * может смениться, а сломанный YAML оператор увидит уже на своём сервере.
 */
function yaml(value: string): string {
  return JSON.stringify(value)
}

export interface NodeDeployOptions {
  secret: string
  /** Интерфейс для счётчиков трафика. */
  statsInterface?: string
}

export function nodeCompose({ secret, statsInterface = 'eth0' }: NodeDeployOptions): string {
  return `# Адрес хаба зашит в секрете и должен резолвиться с машины ноды.
# Если нода стоит рядом с хабом и в секрете осталось имя из его compose —
# раскомментируйте networks внизу, иначе агент будет повторять
# «name resolver error: produced zero addresses».
services:
  node:
    image: ${REGISTRY}/boardproxy-node-agent:${TAG}
    restart: unless-stopped
    environment:
      BPROXY_NODE_SECRET: ${yaml(secret)}
      BPROXY_STATS_INTERFACES: ${yaml(statsInterface)}
      BPROXY_CORE_CONTROL: unix:///run/bproxy/control.sock
    volumes:
      - node-data:/var/lib/bproxy-node
#    networks:
#      - hub

#networks:
#  hub:
#    external: true
#    name: boardproxy_default

volumes:
  node-data:
`
}

export interface SubscribeDeployOptions {
  token: string
  /** Адрес хаба, по которому сервис ходит за конфигурацией. */
  controlPlaneUrl: string
  /** Порт на хосте; наружу его публикует реверс-прокси. */
  port?: number
  /** Сервис поднимают рядом с хабом — подключаем его к сети хаба. */
  sameHost?: boolean
}

/**
 * Адрес хаба для сервиса подписок.
 *
 * Origin панели годится, только когда её открывают по внешнему имени. На
 * `localhost` он указывает внутрь контейнера сервиса, а не на хаб, и сервис
 * бесконечно повторяет «connect: connection refused». В этом случае берём имя
 * сервиса из compose хаба — оно разрешится, если подключить сервис к его сети.
 */
export function suggestControlPlaneUrl(location: { hostname: string; origin: string }): {
  url: string
  sameHost: boolean
} {
  const local = location.hostname === 'localhost' || /^\d{1,3}(\.\d{1,3}){3}$/.test(location.hostname)
  return local ? { url: 'http://hub:8080', sameHost: true } : { url: location.origin, sameHost: false }
}

export function subscribeCompose({
  token,
  controlPlaneUrl,
  port = 8090,
  sameHost = false,
}: SubscribeDeployOptions): string {
  // Блок сети нужен, только когда сервис стоит рядом с хабом: иначе он остаётся
  // в своей сети, где имени `hub` не существует.
  const networks = sameHost
    ? `    networks:
      - hub

networks:
  hub:
    external: true
    name: boardproxy_default
`
    : ''
  return `services:
  subscribe:
    image: ${REGISTRY}/boardproxy-subscribe:${TAG}
    restart: unless-stopped
    environment:
      SUBSCRIBE_LISTEN: ":8090"
      SUBSCRIBE_CONTROL_PLANE_URL: ${yaml(controlPlaneUrl)}
      SUBSCRIBE_CONTROL_PLANE_TOKEN: ${yaml(token)}
      SUBSCRIBE_CONTROL_PLANE_TIMEOUT: 10s
    ports:
      - "127.0.0.1:${port}:8090"
    read_only: true
    tmpfs:
      - /tmp:size=16m,mode=1777
    cap_drop:
      - ALL
    security_opt:
      - no-new-privileges:true
${networks}`
}

/** Одна и та же команда для обоих шаблонов. */
export const RUN_COMMAND = 'docker compose up -d'
