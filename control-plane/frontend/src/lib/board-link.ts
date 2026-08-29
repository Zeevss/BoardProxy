/**
 * Хэш доски из ссылки, которую оператор скопировал из браузера.
 *
 * Ссылка приходит в разном виде — с `?hash=…`, с хвостом в пути, иногда с
 * лишними параметрами. Заставлять оператора вырезать хэш руками значило бы
 * получать опечатки в идентификаторе, который потом нигде не проверить.
 */
import { slugify } from './slug'

/**
 * Идентификатор доски.
 *
 * Хаб его не генерирует: `id` обязателен и должен подходить под
 * `^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`. Выводим из названия, а когда название
 * пустое или нелатинское — из начала хэша: заставлять оператора придумывать
 * идентификатор ради записи, которую он видит по имени, незачем.
 */
export function boardId(name: string, hash: string): string {
  const slug = slugify(name, 'b-')
  if (slug) return slug
  return hash ? `b-${hash.slice(0, 12).replace(/[^a-zA-Z0-9]/g, '')}` : ''
}

export function boardHash(link: string): string {
  const raw = link.trim()
  if (!raw) return ''

  const parameter = /[?&#]hash=([^&#\s]+)/i.exec(raw)
  if (parameter) return decodeURIComponent(parameter[1])

  // Иначе берём последний непустой сегмент: у коротких ссылок хэш стоит в пути.
  const tail = raw.split(/[?#/]/).filter(Boolean).pop() ?? ''
  return tail
}
