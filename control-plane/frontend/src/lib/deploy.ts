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
  return `services:
  node:
    image: ${REGISTRY}/boardproxy-node-agent:${TAG}
    restart: unless-stopped
    environment:
      BPROXY_NODE_SECRET: ${yaml(secret)}
      BPROXY_STATS_INTERFACES: ${yaml(statsInterface)}
      BPROXY_CORE_CONTROL: unix:///run/bproxy/control.sock
    volumes:
      - node-data:/var/lib/bproxy-node

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
}

export function subscribeCompose({
  token,
  controlPlaneUrl,
  port = 8090,
}: SubscribeDeployOptions): string {
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
`
}

/** Одна и та же команда для обоих шаблонов. */
export const RUN_COMMAND = 'docker compose up -d'
