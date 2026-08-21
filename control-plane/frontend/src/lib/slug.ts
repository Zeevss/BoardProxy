/**
 * Кириллица в латиницу для идентификаторов.
 *
 * Панель русскоязычная, а `id` пользователя виден в keylink'ах, в TOML ноды и в
 * журнале — то есть там, где кириллице не место. Без транслитерации имя
 * «Дмитрий Орлов» схлопывалось в пустую строку, и оператору приходилось
 * придумывать идентификатор самому.
 */
const CYRILLIC: Record<string, string> = {
  а: 'a', б: 'b', в: 'v', г: 'g', д: 'd', е: 'e', ё: 'e', ж: 'zh', з: 'z',
  и: 'i', й: 'y', к: 'k', л: 'l', м: 'm', н: 'n', о: 'o', п: 'p', р: 'r',
  с: 's', т: 't', у: 'u', ф: 'f', х: 'h', ц: 'ts', ч: 'ch', ш: 'sh',
  щ: 'sch', ъ: '', ы: 'y', ь: '', э: 'e', ю: 'yu', я: 'ya',
}

export function transliterate(value: string): string {
  return value
    .toLowerCase()
    .split('')
    .map((character) => CYRILLIC[character] ?? character)
    .join('')
}

/**
 * Идентификатор из имени: строчная латиница, цифры и дефис.
 *
 * Пустая строка означает, что из имени не удалось извлечь ничего пригодного —
 * вызывающий показывает подсказку вместо того, чтобы отправлять заведомо
 * неверный запрос.
 */
export function slugify(value: string, prefix = ''): string {
  const slug = transliterate(value)
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 24)
    .replace(/-+$/, '')
  return slug ? `${prefix}${slug}` : ''
}
