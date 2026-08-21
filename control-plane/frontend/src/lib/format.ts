import type { Language } from './i18n'

/**
 * Объём в десятичных единицах: 1 GB — это 10⁹ байт.
 *
 * Не двоичные намеренно. Квота задаётся полем «Лимит трафика, ГБ» и уходит на
 * хаб умножением на 10⁹; при делении на 1024 оператор вводил бы 1000 и видел
 * 931 — расхождение, которое выглядит как ошибка счёта, а не как выбор единиц.
 */
export function bytes(value: number | null | undefined): string {
  if (!value) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let index = 0
  let scaled = value
  while (scaled >= 1000 && index < units.length - 1) {
    scaled /= 1000
    index += 1
  }
  // Десятая доля показывается только когда она есть: «1.0 TB» читается как
  // случайная точность, хотя означает ровно то же, что «1 TB».
  const rounded =
    scaled >= 100 || index === 0 ? Math.round(scaled) : Number(scaled.toFixed(1))
  return `${rounded} ${units[index]}`
}

const RELATIVE_STEPS: Array<[limit: number, divisor: number, unit: Intl.RelativeTimeFormatUnit]> = [
  [60, 1, 'second'],
  [3600, 60, 'minute'],
  [86_400, 3600, 'hour'],
  [2_592_000, 86_400, 'day'],
  [31_536_000, 2_592_000, 'month'],
  [Number.POSITIVE_INFINITY, 31_536_000, 'year'],
]

/**
 * «2 мин назад» из ISO-времени хаба.
 *
 * Считается от часов браузера, а они могут разойтись с серверными. Небольшой
 * рассинхрон дал бы «через 3 секунды» вместо «только что», поэтому будущее в
 * пределах минуты схлопывается в ноль.
 */
export function relativeTime(iso: string | null | undefined, language: Language): string | null {
  if (!iso) return null
  const timestamp = Date.parse(iso)
  if (Number.isNaN(timestamp)) return null

  const deltaSeconds = (timestamp - Date.now()) / 1000
  const clamped = deltaSeconds > 0 && deltaSeconds < 60 ? 0 : deltaSeconds
  const magnitude = Math.abs(clamped)

  const step = RELATIVE_STEPS.find(([limit]) => magnitude < limit) ?? RELATIVE_STEPS.at(-1)!
  const [, divisor, unit] = step

  const formatter = new Intl.RelativeTimeFormat(language, { numeric: 'auto' })
  return formatter.format(Math.round(clamped / divisor), unit)
}

/** Абсолютное время для подсказок и таблиц журнала. */
export function absoluteTime(iso: string | null | undefined, language: Language): string | null {
  if (!iso) return null
  const timestamp = Date.parse(iso)
  if (Number.isNaN(timestamp)) return null
  return new Intl.DateTimeFormat(language, { dateStyle: 'medium', timeStyle: 'medium' }).format(
    timestamp,
  )
}

export function percent(part: number, whole: number): number {
  if (!whole) return 0
  return Math.min(100, Math.round((part / whole) * 100))
}
