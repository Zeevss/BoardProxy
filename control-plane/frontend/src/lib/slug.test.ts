import { describe, expect, it } from 'vitest'
import { slugify, transliterate } from './slug'

describe('транслитерация', () => {
  it('переводит кириллицу в латиницу', () => {
    expect(transliterate('Щука')).toBe('schuka')
    expect(transliterate('Подъезд')).toBe('podezd')
  })

  it('латиницу не трогает', () => {
    expect(transliterate('Grace Hopper')).toBe('grace hopper')
  })
})

describe('идентификатор из имени', () => {
  /**
   * Ровно тот случай, из-за которого понадобилась транслитерация: без неё
   * русское имя вырезалось целиком и поле id оставалось пустым.
   */
  it('русское имя даёт пригодный идентификатор', () => {
    expect(slugify('Дмитрий Орлов', 'u-')).toBe('u-dmitriy-orlov')
  })

  it('схлопывает разделители и обрезает края', () => {
    expect(slugify('  Grace   Hopper!  ', 'u-')).toBe('u-grace-hopper')
  })

  it('не оставляет дефис на срезе длины', () => {
    // 24 знака приходятся ровно на пробел между словами.
    expect(slugify('aaaaaaaaaaaaaaaaaaaaaaaa bbb', 'u-')).toBe('u-aaaaaaaaaaaaaaaaaaaaaaaa')
    expect(slugify('aaaaaaaaaaaaaaaaaaaaaaa bbb', 'u-')).toBe('u-aaaaaaaaaaaaaaaaaaaaaaa')
  })

  /** Пустой результат — сигнал вызывающему показать подсказку, а не отправить запрос. */
  it('из имени без букв и цифр не выдумывает идентификатор', () => {
    expect(slugify('!!! ???', 'u-')).toBe('')
    expect(slugify('', 'u-')).toBe('')
  })
})
