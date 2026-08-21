import type { Agent, Node } from '@/api/types'
import type { Dictionary } from './i18n'

/** Крупная группа для фильтров: «в сети», «проблемы», «выключены». */
export type HealthBucket = 'ok' | 'issue' | 'off'

export interface NodeHealth {
  bucket: HealthBucket
  label: string
  /** Уточнение под заголовком: что именно наблюдается. */
  meta: string
  /** Ключ цвета точки; сами цвета живут в теме. */
  tone: 'ok' | 'warn' | 'danger' | 'info' | 'muted'
  /** Показывать ли пульсацию: состояние живое и меняется само. */
  live: boolean
}

/**
 * Лестница состояний ноды, выведенная строго из наблюдаемого.
 *
 * Дизайн различал «ядро остановлено» и «ядро запускается», но контракт ноды
 * (`message Health` в node.proto) таких полей не несёт — есть только отчёт
 * агента и присланный снимок. Поэтому вместо выдумывания признака ядро
 * оценивается по факту свежего снимка: агент отчитывается, снимка нет — ядро
 * молчит.
 *
 * Порядок проверок — от более общего отказа к более частному: потерянная связь
 * делает бессмысленным разговор о ревизиях.
 */
export function nodeHealth(node: Node, agent: Agent | undefined, t: Dictionary): NodeHealth {
  if (node.state === 'revoked') {
    return { bucket: 'off', label: t.healthRevoked, meta: t.healthRevokedMeta, tone: 'danger', live: false }
  }
  if (node.state === 'disabled') {
    return { bucket: 'off', label: t.healthDisabled, meta: t.healthDisabledMeta, tone: 'muted', live: false }
  }
  if (!agent || agent.lastReportAt === null) {
    return { bucket: 'issue', label: t.healthLinkLost, meta: t.healthNeverSeen, tone: 'danger', live: false }
  }
  if (!agent.online) {
    return { bucket: 'issue', label: t.healthLinkLost, meta: t.healthLinkLostMeta, tone: 'danger', live: false }
  }
  if (!agent.coreReporting) {
    return { bucket: 'issue', label: t.healthCoreSilent, meta: t.healthCoreSilentMeta, tone: 'danger', live: false }
  }
  if (agent.applyError) {
    return { bucket: 'issue', label: t.healthApplyError, meta: agent.applyError, tone: 'warn', live: false }
  }
  if (agent.appliedRevision !== agent.desiredRevision) {
    return {
      bucket: 'issue',
      label: t.healthSyncing,
      meta: `applied ${agent.appliedRevision} / ${agent.desiredRevision}`,
      tone: 'info',
      live: true,
    }
  }
  return { bucket: 'ok', label: t.healthOnline, meta: t.healthOnlineMeta, tone: 'ok', live: true }
}

export type UserStatusKey = 'active' | 'pending' | 'off'

export interface UserStatus {
  key: UserStatusKey
  label: string
  tone: 'ok' | 'warn' | 'danger' | 'info' | 'muted'
  live: boolean
}

export function userStatus(
  user: { state: string; activated: boolean; quota: { exceeded: boolean } | null },
  t: Dictionary,
): UserStatus {
  if (user.state === 'revoked') return { key: 'off', label: t.uRevoked, tone: 'danger', live: false }
  if (user.state === 'disabled') return { key: 'off', label: t.uOff, tone: 'muted', live: false }
  if (!user.activated) return { key: 'pending', label: t.uPending, tone: 'info', live: true }
  if (user.quota?.exceeded) return { key: 'active', label: t.uOver, tone: 'warn', live: false }
  return { key: 'active', label: t.uActive, tone: 'ok', live: true }
}
