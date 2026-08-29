import { useMemo, useState } from 'react'
import { Plus } from 'lucide-react'
import { useNavigate } from 'react-router'
import { useAudit } from '@/api/audit'
import { useAgents, useBoards, useNodes } from '@/api/nodes'
import { useTrafficTotals } from '@/api/traffic'
import { useUsers } from '@/api/users'
import type { Agent, AuditEvent, Node } from '@/api/types'
import { useLanguage } from '@/app/language'
import { ScreenHeader } from '@/components/ScreenHeader'
import { Button } from '@/components/ui/button'
import { StatusDot } from '@/components/ui/status'
import { bytes, plural, relativeTime } from '@/lib/format'
import { nodeHealth, userStatus, type HealthBucket } from '@/lib/health'
import { cn } from '@/lib/utils'
import { CreateUserDialog } from './CreateUserDialog'
import { NodeSheet } from './NodeSheet'
import { NodeWizard } from './NodeWizard'

const TRAFFIC_DAYS = 30

export function OverviewScreen() {
  const { t, language } = useLanguage()
  const navigate = useNavigate()

  const [creatingNode, setCreatingNode] = useState(false)
  const [creatingUser, setCreatingUser] = useState(false)
  const [openNode, setOpenNode] = useState<string | null>(null)

  const nodes = useNodes()
  const agents = useAgents()
  const boards = useBoards()
  const users = useUsers()
  const audit = useAudit(40)
  const traffic = useTrafficTotals('user', TRAFFIC_DAYS)

  const agentById = useMemo(() => {
    const map = new Map<string, Agent>()
    for (const agent of agents.data ?? []) map.set(agent.id, agent)
    return map
  }, [agents.data])

  const fleet = useMemo(() => {
    const counts: Record<HealthBucket, number> = { ok: 0, issue: 0, off: 0 }
    const rows = (nodes.data?.items ?? []).map((node) => ({
      node,
      agent: agentById.get(node.id),
      health: nodeHealth(node, agentById.get(node.id), t),
    }))
    for (const row of rows) counts[row.health.bucket] += 1
    return { rows, counts }
  }, [nodes.data, agentById, t])

  const userCounts = useMemo(() => {
    const counts = { active: 0, pending: 0, off: 0 }
    for (const user of users.data?.items ?? []) counts[userStatus(user, t).key] += 1
    return counts
  }, [users.data, t])

  const nodeTotal = nodes.data?.items.length ?? 0
  const boardTotal = boards.data?.items.length ?? 0
  const userTotal = users.data?.items.length ?? 0
  const sessions = (agents.data ?? []).reduce((sum, agent) => sum + agent.activeSessions, 0)
  const used = (traffic.data ?? []).reduce((sum, row) => sum + row.rxBytes + row.txBytes, 0)
  const quotaTotal = (users.data?.items ?? []).reduce(
    (sum, user) => sum + (user.quota?.enabled ? user.quota.limitBytes : 0),
    0,
  )

  /**
   * Первая нода, у которой применённая ревизия отстаёт от желаемой.
   *
   * Только среди отчитывавшихся: у ноды, ни разу не вышедшей на связь,
   * applied всегда ноль, и расхождением это не является — она просто ещё не
   * начинала. Баннер о ней сообщал бы о проблеме там, где её нет.
   */
  const drift = fleet.rows.find(
    (row) =>
      row.agent &&
      row.agent.lastReportAt !== null &&
      row.agent.appliedRevision !== row.agent.desiredRevision,
  )

  const selected = nodes.data?.items.find((node) => node.id === openNode) ?? null

  return (
    <section className="mx-auto flex max-w-6xl flex-col gap-5">
      <ScreenHeader
        title={t.overview}
        subtitle={t.overviewSub}
        actions={
          <>
            <Button variant="outline" onClick={() => setCreatingNode(true)}>
              <Plus />
              {t.newNode}
            </Button>
            <Button variant="primary" onClick={() => setCreatingUser(true)}>
              <Plus />
              {t.newUser}
            </Button>
          </>
        }
      />

      <div className="grid gap-3.5 sm:grid-cols-2 lg:grid-cols-4">
        <Stat
          label={t.statNodes}
          value={`${fleet.counts.ok} / ${nodeTotal}`}
          tag={fleet.counts.issue > 0 ? `${t.metaIssues} ${fleet.counts.issue}` : 'ok'}
          tagTone={fleet.counts.issue > 0 ? 'warn' : 'ok'}
          percent={nodeTotal ? Math.round((fleet.counts.ok / nodeTotal) * 100) : 0}
          barTone={fleet.counts.issue > 0 ? 'bg-warn' : 'bg-ok-fg'}
          hint={t.statNodesHint}
        />
        <Stat
          label={t.users}
          value={String(userTotal)}
          tag={userCounts.pending > 0 ? `+${userCounts.pending}` : 'ok'}
          tagTone={userCounts.pending > 0 ? 'info' : 'muted'}
          percent={userTotal ? Math.round((userCounts.active / userTotal) * 100) : 0}
          barTone="bg-fg"
          hint={`${userCounts.active} ${t.statUsersActive}`}
        />
        <Stat
          label={t.statSessions}
          value={String(sessions)}
          tag="live"
          tagTone="ok"
          hint={t.statSessionsHint}
        />
        <Stat
          label={`${t.traffic} · ${TRAFFIC_DAYS} ${t.days}`}
          value={bytes(used)}
          tag={quotaTotal ? `${Math.round((used / quotaTotal) * 100)}% ${t.statOfQuota}` : '—'}
          tagTone="muted"
          percent={quotaTotal ? Math.min(100, Math.round((used / quotaTotal) * 100)) : 0}
          barTone="bg-fg"
          hint={t.statTrafficHint}
        />
      </div>

      {drift ? (
        <div className="flex flex-wrap items-center justify-between gap-3.5 rounded-xl border border-warn-line bg-warn-bg px-4 py-3.5">
          <div className="min-w-0">
            <p className="text-[13.5px] font-semibold text-warn-fg">{t.driftTitle}</p>
            <p className="mt-0.5 font-mono text-xs text-warn">
              {drift.node.id} · desired {drift.agent?.desiredRevision} / applied{' '}
              {drift.agent?.appliedRevision}
            </p>
          </div>
          <Button
            size="sm"
            variant="outline"
            className="border-warn-line text-warn-fg"
            onClick={() => setOpenNode(drift.node.id)}
          >
            {t.openNode}
          </Button>
        </div>
      ) : null}

      <div className="grid gap-3.5 lg:grid-cols-[3fr_2fr]">
        <section className="overflow-hidden rounded-xl border border-line bg-canvas">
          <header className="flex items-center justify-between gap-3 px-4 py-3.5">
            <div className="flex min-w-0 items-baseline gap-2.5">
              <h2 className="text-sm font-semibold">{t.fleet}</h2>
              <span className="truncate font-mono text-[11.5px] text-muted">
                {nodeTotal}{' '}
                {plural(nodeTotal, { one: t.nodeOne, few: t.nodeFew, many: t.nodeMany }, language)} ·{' '}
                {boardTotal}{' '}
                {plural(boardTotal, { one: t.boardOne, few: t.boardFew, many: t.boardMany }, language)}
              </span>
            </div>
            <button
              type="button"
              onClick={() => void navigate('/nodes')}
              className="shrink-0 text-[12.5px] text-dim transition-colors hover:text-fg"
            >
              {t.showAll}
            </button>
          </header>

          {/* Полоса состава флота: сколько нод в сети, сколько с проблемами. */}
          <div className="flex h-[3px] bg-raised">
            <Segment weight={fleet.counts.ok} className="bg-ok" />
            <Segment weight={fleet.counts.issue} className="bg-warn" />
            <Segment weight={fleet.counts.off} className="bg-line-strong" />
          </div>

          {fleet.rows.slice(0, 5).map((row) => (
            <FleetRow
              key={row.node.id}
              node={row.node}
              agent={row.agent}
              health={row.health}
              language={language}
              onOpen={() => setOpenNode(row.node.id)}
            />
          ))}

          {fleet.rows.length === 0 ? (
            <p className="border-t border-line-soft px-4 py-4 text-[12.5px] text-muted">
              {t.emptyFleet}
            </p>
          ) : null}
        </section>

        <section className="flex flex-col overflow-hidden rounded-xl border border-line bg-canvas">
          <header className="flex items-center gap-2 border-b border-line-soft px-4 py-3.5">
            <h2 className="text-sm font-semibold">{t.activity}</h2>
            <StatusDot tone="ok" live className="size-1.5" />
          </header>
          <div className="max-h-[420px] overflow-y-auto">
            {(audit.data?.items ?? []).map((event) => (
              <ActivityRow key={event.id} event={event} language={language} />
            ))}
            {!audit.isLoading && (audit.data?.items.length ?? 0) === 0 ? (
              <p className="px-4 py-4 text-[12.5px] text-muted">{t.noActivity}</p>
            ) : null}
          </div>
        </section>
      </div>

      <NodeSheet node={selected} onClose={() => setOpenNode(null)} />
      <NodeWizard open={creatingNode} onClose={() => setCreatingNode(false)} />
      <CreateUserDialog open={creatingUser} onClose={() => setCreatingUser(false)} />
    </section>
  )
}

/** Ноль тоже занимает место: иначе полоса состава дёргается при пустой группе. */
function Segment({ weight, className }: { weight: number; className: string }) {
  return <div className={className} style={{ flex: weight || 0.001 }} />
}

function Stat({
  label,
  value,
  tag,
  tagTone,
  percent,
  barTone,
  hint,
}: {
  label: string
  value: string
  tag: string
  tagTone: 'ok' | 'warn' | 'info' | 'muted'
  percent?: number
  barTone?: string
  hint: string
}) {
  const tagClass = {
    ok: 'text-ok-fg',
    warn: 'text-warn-fg',
    info: 'text-info',
    muted: 'text-dim',
  }[tagTone]

  return (
    <div className="flex flex-col gap-2.25 rounded-xl border border-line bg-canvas px-4 py-3.75">
      <div className="flex items-center justify-between gap-2">
        <span className="min-w-0 truncate text-xs font-medium text-dim">{label}</span>
        <span className={cn('shrink-0 font-mono text-[10.5px] whitespace-nowrap', tagClass)}>
          {tag}
        </span>
      </div>
      <p className="text-[26px] leading-tight font-semibold tracking-tight tabular-nums">{value}</p>
      {percent !== undefined ? (
        <div className="h-1 overflow-hidden rounded-full bg-line">
          <div
            className={cn('h-full rounded-full transition-[width] duration-300', barTone)}
            style={{ width: `${percent}%` }}
          />
        </div>
      ) : null}
      <p className="text-[11.5px] text-muted">{hint}</p>
    </div>
  )
}

function FleetRow({
  node,
  agent,
  health,
  language,
  onOpen,
}: {
  node: Node
  agent: Agent | undefined
  health: ReturnType<typeof nodeHealth>
  language: 'ru' | 'en'
  onOpen: () => void
}) {
  const seen = relativeTime(agent?.lastReportAt, language)

  return (
    <button
      type="button"
      onClick={onOpen}
      className="flex w-full items-center gap-3 border-t border-line-soft px-4 py-3 text-left transition-colors hover:bg-raised"
    >
      <StatusDot tone={health.tone} live={health.live} className="size-2" />
      <div className="min-w-0 flex-1">
        <p className="truncate text-[13px] font-medium">{node.name}</p>
        <p className="truncate font-mono text-[11px] text-muted">
          {node.id} · {health.meta}
          {seen ? ` ${seen}` : ''}
        </p>
      </div>
      <span
        className={cn(
          'shrink-0 text-xs',
          health.tone === 'ok'
            ? 'text-ok-fg'
            : health.tone === 'warn'
              ? 'text-warn-fg'
              : health.tone === 'danger'
                ? 'text-danger'
                : health.tone === 'info'
                  ? 'text-info'
                  : 'text-dim',
        )}
      >
        {health.label}
      </span>
    </button>
  )
}

/**
 * Строка журнала.
 *
 * Действие показывается как есть (`node.created`, `quota.changed`): это
 * машинное имя из audit_events, и переводить его значило бы разойтись с тем,
 * что оператор увидит в логах хаба.
 */
function ActivityRow({ event, language }: { event: AuditEvent; language: 'ru' | 'en' }) {
  const when = relativeTime(event.occurredAt, language)
  const quota = event.action.startsWith('quota') || event.action.startsWith('traffic')

  return (
    <div className="flex flex-col gap-0.75 border-b border-line-soft px-4 py-2.75 last:border-b-0">
      <div className="flex items-baseline gap-2 font-mono text-[11px]">
        <span className="shrink-0 text-muted">{when}</span>
        <span className={cn('min-w-0 truncate', quota ? 'text-warn-fg' : 'text-dim')}>
          {event.action}
        </span>
      </div>
      <p className="text-[12.5px] text-bright">
        {event.resourceType} {event.resourceId}
        <span className="text-dim"> · {event.actor}</span>
      </p>
    </div>
  )
}
