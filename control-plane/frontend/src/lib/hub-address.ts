/**
 * Адрес хаба, который уезжает ноде внутри enrollment-секрета.
 *
 * Это **не** адрес панели. node-agent передаёт это значение прямо в
 * `grpc.NewClient`, а gRPC ждёт `host:port` без схемы: с `http://…` он
 * дописывает свой `:443` и падает на «too many colons in address». Порт тоже
 * другой — панель на 8080, gRPC на 8443. И хост обязан входить в
 * `CONTROL_GRPC_SERVER_NAMES`, иначе не сойдётся имя в сертификате.
 *
 * Поэтому вывести его из `window.location.origin` нельзя ни одним полем.
 */
export const GRPC_PORT = 8443

/** IP вместо имени: в SAN сертификата по умолчанию только `hub` и `localhost`. */
const IP_ADDRESS = /^\d{1,3}(\.\d{1,3}){3}$/

/**
 * Догадка для поля ввода, а не истина.
 *
 * Локально панель открывают по `localhost` или по адресу — оба не совпадут с
 * именем в сертификате, зато совпадёт `hub` из compose. На настоящем стенде
 * панель открывают по имени хаба, и оно же обычно годится для gRPC.
 */
export function suggestHubAddress(location: { hostname: string } = window.location): string {
  const { hostname } = location
  if (hostname === 'localhost' || IP_ADDRESS.test(hostname)) return `hub:${GRPC_PORT}`
  return `${hostname}:${GRPC_PORT}`
}

/** Пусто, схема или отсутствие порта — верный признак того, что взяли адрес панели. */
export function hubAddressProblem(value: string): 'empty' | 'scheme' | 'port' | null {
  const address = value.trim()
  if (!address) return 'empty'
  if (address.includes('://')) return 'scheme'
  if (!/:\d{1,5}$/.test(address)) return 'port'
  return null
}
