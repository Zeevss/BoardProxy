import { useMemo, useState } from 'react'
import { Plus } from 'lucide-react'
import { useAgents, useBoards, useNodes } from '@/api/nodes'
import type { Agent, Node } from '@/api/types'
import {
  EmptyState,
  FilterMeta,
  FilterRow,
  FilterTabs,
  Row,
  RowList,
  ScreenHeader,
} from '@/components/ScreenHeader'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { StatusDot } from '@/components/ui/status'
import { useLanguage } from '@/app/language'
import { nodeHealth, type HealthBucket } from '@/lib/health'
import { relativeTime } from '@/lib/format'
import { cn } from '@/lib/utils'
import { NodeSheet } from './NodeSheet'
import { NodeWizard } from './NodeWizard'

type Filter = 'all' | HealthBucket

export function NodesScreen() {
  const { t, language } = useLanguage()
  const [filter, setFilter] = useState<Filter>('all')
  const [search, setSearch] = useState('')
  const [openNode, setOpenNode] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

  const nodes = useNodes()
  const agents = useAgents()
  const boards = useBoards()

  const agentById = useMemo(() => {
    const map = new Map<string, Agent>()
    for (const agent of agents.data ?? []) map.set(agent.id, agent)
    return map
  }, [agents.data])

  /**
   * Поиск и фильтр считаются на клиенте: флот читается целиком одной страницей,
   * а состояние здоровья вычисляется здесь же и серверу неизвестно.
   */
  const rows = useMemo(() => {
    const query = search.trim().toLowerCase()
    return (nodes.data?.items ?? [])
      .map((node) => ({ node, health: nodeHealth(node, agentById.get(node.id), t) }))
      .filter(({ node }) => !query || `${node.id} ${node.name}`.toLowerCase().includes(query))
      .filter(({ health }) => filter === 'all' || health.bucket === filter)
  }, [nodes.data, agentById, t, search, filter])

  const buckets = useMemo(() => {
    const counts: Record<HealthBucket, number> = { ok: 0, issue: 0, off: 0 }
    for (const node of nodes.data?.items ?? []) counts[nodeHealth(node, agentById.get(node.id), t).bucket] += 1
    return counts
  }, [nodes.data, agentById, t])

  /** Доски читаются одним запросом на флот, здесь только раскладываются по нодам. */
  const boardCount = useMemo(() => {
    const counts = new Map<string, number>()
    for (const board of boards.data?.items ?? []) {
      counts.set(board.nodeId, (counts.get(board.nodeId) ?? 0) + 1)
    }
    return counts
  }, [boards.data])

  const total = nodes.data?.items.length ?? 0
  const selected = nodes.data?.items.find((node) => node.id === openNode) ?? null

  const create = (
    <Button variant="primary" onClick={() => setCreating(true)}>
      <Plus />
      {t.newNode}
    </Button>
  )

  return (
    <section className="mx-auto flex max-w-6xl flex-col gap-4.5">
      <ScreenHeader
        title={t.nodes}
        subtitle={t.nodesSub}
        actions={
          <>
            <Input
              type="search"
              placeholder={t.search}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              className="w-40 sm:w-55"
            />
            {create}
          </>
        }
      />

      <FilterRow>
        <FilterTabs
          value={filter}
          onChange={setFilter}
          options={[
            { key: 'all', label: t.filterAll, count: total },
            { key: 'ok', label: t.filterOk, count: buckets.ok },
            { key: 'issue', label: t.filterIssue, count: buckets.issue },
            { key: 'off', label: t.filterOff, count: buckets.off },
          ]}
        />
        <FilterMeta>
          {t.metaFleet} {total} · {t.metaOnline} {buckets.ok} · {t.metaIssues} {buckets.issue}
        </FilterMeta>
      </FilterRow>

      {nodes.isLoading ? (
        <EmptyState>{t.loading}</EmptyState>
      ) : rows.length === 0 ? (
        <EmptyState action={create}>{total === 0 ? t.emptyFleet : t.nothingFound}</EmptyState>
      ) : (
        <RowList>
          {rows.map(({ node, health }) => (
            <NodeRow
              key={node.id}
              node={node}
              agent={agentById.get(node.id)}
              health={health}
              language={language}
              boards={boardCount.get(node.id) ?? 0}
              onOpen={() => setOpenNode(node.id)}
            />
          ))}
        </RowList>
      )}

      <NodeSheet node={selected} onClose={() => setOpenNode(null)} />
      <NodeWizard open={creating} onClose={() => setCreating(false)} />
    </section>
  )
}

const HEALTH_TEXT: Record<ReturnType<typeof nodeHealth>['tone'], string> = {
  ok: 'text-ok-fg',
  warn: 'text-warn-fg',
  danger: 'text-danger',
  info: 'text-info',
  muted: 'text-dim',
}

interface NodeRowProps {
  node: Node
  agent: Agent | undefined
  health: ReturnType<typeof nodeHealth>
  language: 'ru' | 'en'
  boards: number
  onOpen: () => void
}

function NodeRow({ node, agent, health, language, boards, onOpen }: NodeRowProps) {
  const { t } = useLanguage()
  const seen = relativeTime(agent?.lastReportAt, language)

  return (
    <Row onOpen={onOpen}>
      <StatusDot tone={health.tone} live={health.live} className="size-2.25" />

      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-[13.5px] font-medium">{node.name}</span>
          {/* Здоровье ноды — цветной подписью, а не бейджем: у ноды это одна
              шкала, и заливка спорила бы с бейджем состояния пользователя,
              который означает совсем другое. */}
          <span className={cn('shrink-0 text-xs', HEALTH_TEXT[health.tone])}>{health.label}</span>
        </div>
        <p className="truncate font-mono text-[11px] text-muted">
          {node.id} · {health.meta}
          {seen && health.bucket === 'ok' ? ` ${seen}` : ''}
        </p>
      </div>

      <div className="hidden shrink-0 gap-1.5 lg:flex">
        <VersionChip label="agent" value={agent?.agentVersion} />
        <VersionChip label="core" value={agent?.coreVersion} />
      </div>

      <dl className="hidden shrink-0 gap-3 text-xs tabular-nums text-soft sm:flex">
        <Metric label={t.boards} value={boards} />
        <Metric label={t.sessions} value={agent?.activeSessions ?? 0} />
        <Metric label={t.lanes} value={agent?.activeLanes ?? 0} />
      </dl>
    </Row>
  )
}

function VersionChip({ label, value }: { label: string; value: string | null | undefined }) {
  return (
    <span
      className={cn(
        'inline-flex h-5.25 items-center gap-1.25 rounded-md border border-line bg-raised',
        'px-2 font-mono text-[10.5px] text-soft',
      )}
    >
      <span className="text-muted">{label}</span>
      {value ?? '—'}
    </span>
  )
}

function Metric({ label, value }: { label: string; value: number }) {
  return (
    <div className="flex gap-1.25">
      <dt className="text-muted">{label}</dt>
      <dd>{value}</dd>
    </div>
  )
}
