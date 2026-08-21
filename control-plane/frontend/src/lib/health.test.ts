import { describe, expect, it } from 'vitest'
import { nodeHealth } from './health'
import { DICTIONARY } from './i18n'
import type { Agent, Node } from '@/api/types'

const t = DICTIONARY.ru

const node = (state: Node['state'] = 'enabled'): Node =>
  ({ id: 'n1', name: 'Нода', state, version: 1, updatedAt: '', settings: {} }) as unknown as Node

const agent = (patch: Partial<Agent> = {}): Agent => ({
  id: 'n1',
  kind: 'node',
  name: 'Нода',
  online: true,
  appliedRevision: 5,
  desiredRevision: 5,
  appliedSha256: 'abc',
  desiredSha256: 'abc',
  agentVersion: '1.4.2',
  coreVersion: '0.9.3',
  applyError: null,
  bootId: 'boot',
  lastReportAt: new Date().toISOString(),
  activeSessions: 3,
  activeLanes: 9,
  coreReporting: true,
  coreSnapshotAt: new Date().toISOString(),
  ...patch,
})

describe('состояние ноды', () => {
  it('в сети, когда агент отчитывается и ревизии сошлись', () => {
    expect(nodeHealth(node(), agent(), t)).toMatchObject({ bucket: 'ok', label: t.healthOnline })
  })

  it('выключенная нода не считается проблемой', () => {
    expect(nodeHealth(node('disabled'), agent(), t)).toMatchObject({ bucket: 'off' })
  })

  it('состояние ноды важнее наблюдаемого: отозванная остаётся отозванной', () => {
    expect(nodeHealth(node('revoked'), agent(), t)).toMatchObject({
      bucket: 'off',
      label: t.healthRevoked,
    })
  })

  it('различает «ни разу не выходила» и «связь потеряна»', () => {
    expect(nodeHealth(node(), agent({ online: false, lastReportAt: null }), t).meta).toBe(
      t.healthNeverSeen,
    )
    expect(nodeHealth(node(), agent({ online: false }), t).meta).toBe(t.healthLinkLostMeta)
  })

  it('агент без свежего снимка означает молчащее ядро', () => {
    expect(nodeHealth(node(), agent({ coreReporting: false }), t)).toMatchObject({
      bucket: 'issue',
      label: t.healthCoreSilent,
    })
  })

  it('потерянная связь важнее расхождения ревизий', () => {
    const lost = agent({ online: false, appliedRevision: 1, desiredRevision: 5 })
    expect(nodeHealth(node(), lost, t).label).toBe(t.healthLinkLost)
  })

  it('расхождение ревизий показывается как синхронизация', () => {
    const drifting = agent({ appliedRevision: 4, desiredRevision: 5 })
    expect(nodeHealth(node(), drifting, t)).toMatchObject({
      bucket: 'issue',
      label: t.healthSyncing,
      meta: 'applied 4 / 5',
      live: true,
    })
  })

  it('ошибка применения перекрывает расхождение ревизий', () => {
    const broken = agent({ applyError: 'hash mismatch', appliedRevision: 4, desiredRevision: 5 })
    expect(nodeHealth(node(), broken, t)).toMatchObject({
      label: t.healthApplyError,
      meta: 'hash mismatch',
    })
  })

  it('нода без записи агента не считается здоровой', () => {
    expect(nodeHealth(node(), undefined, t).bucket).toBe('issue')
  })
})
