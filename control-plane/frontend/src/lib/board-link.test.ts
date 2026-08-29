import { describe, expect, it } from 'vitest'
import { boardHash, boardId } from './board-link'

/** Хаб требует `^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$` и сам id не генерирует. */
const HUB_PATTERN = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$/

describe('идентификатор доски', () => {
  it('выводится из названия', () => {
    expect(boardId('Main board', 'abc123')).toBe('b-main-board')
  })

  it('русское название транслитерирует', () => {
    expect(boardId('Главная доска', 'abc123')).toBe('b-glavnaya-doska')
  })

  /** Без названия остаётся только хэш — иначе отправлять было бы нечего. */
  it('без пригодного названия берёт начало хэша', () => {
    expect(boardId('', '9d2f8c1a4b7e36502fd8')).toBe('b-9d2f8c1a4b7e')
    expect(boardId('!!!', '9d2f8c1a4b7e36502fd8')).toBe('b-9d2f8c1a4b7e')
  })

  it('пустой хэш и пустое название не дают мусорный id', () => {
    expect(boardId('', '')).toBe('')
  })

  it('всё, что выдаёт, проходит проверку хаба', () => {
    for (const [name, hash] of [
      ['Main board', 'abc'],
      ['Главная доска', 'abc'],
      ['', '9d2f8c1a4b7e36502fd8'],
      ['  ??  ', 'ZZ99'],
    ]) {
      expect(boardId(name, hash)).toMatch(HUB_PATTERN)
    }
  })
})

describe('хэш доски из ссылки', () => {
  it('берёт параметр hash независимо от его места в запросе', () => {
    expect(boardHash('https://board.example.net/?hash=abc123')).toBe('abc123')
    expect(boardHash('https://board.example.net/?utm=x&hash=abc123&y=1')).toBe('abc123')
  })

  it('не путает hash с похожим по имени параметром', () => {
    expect(boardHash('https://board.example.net/?prehash=zzz&hash=abc123')).toBe('abc123')
  })

  it('обрезает якорь после значения', () => {
    expect(boardHash('https://board.example.net/?hash=abc123#slide-2')).toBe('abc123')
  })

  /** Короткие ссылки несут хэш прямо в пути, без параметра. */
  it('из пути берёт последний сегмент', () => {
    expect(boardHash('https://board.example.net/b/abc123')).toBe('abc123')
    expect(boardHash('https://board.example.net/b/abc123/')).toBe('abc123')
  })

  it('принимает голый хэш', () => {
    expect(boardHash('  abc123  ')).toBe('abc123')
  })

  it('пустая ссылка даёт пустой хэш, а не мусор', () => {
    expect(boardHash('')).toBe('')
    expect(boardHash('   ')).toBe('')
  })
})
