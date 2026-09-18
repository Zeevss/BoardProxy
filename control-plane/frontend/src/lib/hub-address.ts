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

/**
 * Догадка для поля ввода, а не истина.
 *
 * Берётся имя, по которому открыта панель: по нему хаб доступен хотя бы
 * откуда-то, и его же обычно вписывают в `CONTROL_GRPC_SERVER_NAMES`.
 *
 * Раньше для `localhost` и IP подставлялось `hub:8443` — имя сервиса из общего
 * compose. Пока нода поднималась профилем в той же сети, оно работало; с
 * отдельным compose у ноды своя сеть, и `hub` не резолвится ни во что —
 * агент бесконечно повторяет «name resolver error: produced zero addresses».
 * Подставлять заведомо нерабочее значение хуже, чем неточное: `localhost`
 * оператор хотя бы прочитает как «поправь на адрес хаба».
 */
export function suggestHubAddress(location: { hostname: string } = window.location): string {
  return `${location.hostname}:${GRPC_PORT}`
}

/** Пусто, схема или отсутствие порта — верный признак того, что взяли адрес панели. */
export function hubAddressProblem(value: string): 'empty' | 'scheme' | 'port' | null {
  const address = value.trim()
  if (!address) return 'empty'
  if (address.includes('://')) return 'scheme'
  if (!/:\d{1,5}$/.test(address)) return 'port'
  return null
}
