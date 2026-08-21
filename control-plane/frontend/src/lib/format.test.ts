import { describe, expect, it } from 'vitest'
import { bytes, percent, relativeTime } from './format'

describe('объём', () => {
  it('ноль и пустое значение читаются одинаково', () => {
    expect(bytes(0)).toBe('0 B')
    expect(bytes(null)).toBe('0 B')
    expect(bytes(undefined)).toBe('0 B')
  })

  /**
   * Ровно тот случай, ради которого выбраны десятичные единицы: поле ввода
   * подписано «ГБ» и умножает на 10⁹, поэтому 1000 ГБ обязаны прочитаться как
   * 1 TB, а не как 931 GB.
   */
  it('замыкает круг с полем ввода квоты', () => {
    expect(bytes(1000 * 1_000_000_000)).toBe('1 TB')
    expect(bytes(100 * 1_000_000_000)).toBe('100 GB')
  })

  it('дробную часть показывает только у малых значений', () => {
    expect(bytes(1_500_000_000)).toBe('1.5 GB')
    expect(bytes(412_000_000_000)).toBe('412 GB')
  })

  it('байты не дробит', () => {
    expect(bytes(999)).toBe('999 B')
  })
})

describe('доля', () => {
  it('нулевой знаменатель не даёт NaN', () => {
    expect(percent(5, 0)).toBe(0)
  })

  it('не превышает ста процентов при перерасходе', () => {
    expect(percent(150, 100)).toBe(100)
  })
})

describe('относительное время', () => {
  it('пустое значение остаётся пустым', () => {
    expect(relativeTime(null, 'ru')).toBeNull()
    expect(relativeTime('не дата', 'ru')).toBeNull()
  })

  /**
   * Часы браузера могут уйти вперёд серверных. Без схлопывания ближайшее
   * будущее показывалось бы как «через 3 секунды» вместо «сейчас».
   */
  it('ближайшее будущее схлопывает в настоящее', () => {
    const soon = new Date(Date.now() + 20_000).toISOString()
    expect(relativeTime(soon, 'ru')).toBe(relativeTime(new Date().toISOString(), 'ru'))
  })

  it('прошлое считает в подходящих единицах', () => {
    const hourAgo = new Date(Date.now() - 3_600_000).toISOString()
    expect(relativeTime(hourAgo, 'en')).toContain('hour')
  })
})
