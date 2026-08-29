import { useState } from 'react'
import {
  useAgents,
  useBoards,
  useDeleteNode,
  useNodeConfig,
  useNodeEvents,
  useUpdateBoard,
  useUpdateNode,
} from '@/api/nodes'
import type { Agent, Board, Node } from '@/api/types'
import { ApiError, ConflictError, RateLimitedError } from '@/api/errors'
import { useLanguage } from '@/app/language'
import { Button } from '@/components/ui/button'
import { CopyButton } from '@/components/ui/copy'
import { Sheet } from '@/components/ui/sheet'
import { StatusDot } from '@/components/ui/status'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useToast } from '@/components/ui/toast'
import { Switch } from '@/components/ui/switch'
import { useUsers } from '@/api/users'
import { absoluteTime, relativeTime } from '@/lib/format'
import { nodeHealth } from '@/lib/health'
import { cn } from '@/lib/utils'
import { BoardDialog } from './BoardDialog'
import { EnrollmentSecretDialog } from './EnrollmentSecretDialog'
import { IssueSecretDialog } from './IssueSecretDialog'

export function NodeSheet({ node, onClose }: { node: Node | null; onClose: () => void }) {
  const { t } = useLanguage()
  const agents = useAgents()
  const agent = agents.data?.find((item) => item.id === node?.id)

  return (
    <Sheet
      open={node !== null}
      onOpenChange={(open) => !open && onClose()}
      label={node?.name ?? t.node}
      title={node ? <SheetTitle node={node} agent={agent} /> : null}
      actions={node ? <SheetActions node={node} onClose={onClose} /> : null}
    >
      {node ? <SheetBody node={node} agent={agent} /> : null}
    </Sheet>
  )
}

const HEALTH_TEXT: Record<ReturnType<typeof nodeHealth>['tone'], string> = {
  ok: 'text-ok-fg',
  warn: 'text-warn-fg',
  danger: 'text-danger',
  info: 'text-info',
  muted: 'text-dim',
}

function SheetTitle({ node, agent }: { node: Node; agent: Agent | undefined }) {
  const { t, language } = useLanguage()
  const health = nodeHealth(node, agent, t)
  const seen = relativeTime(agent?.lastReportAt, language)

  return (
    <div className="flex gap-3">
      <StatusDot tone={health.tone} live={health.live} className="mt-1.5 size-2.5" />
      <div className="min-w-0">
        <h2 className="truncate text-lg font-semibold tracking-tight">{node.name}</h2>
        <div className="mt-1 flex flex-wrap items-center gap-2">
          <span className="font-mono text-xs text-dim">{node.id}</span>
          <span className={cn('text-[12.5px]', HEALTH_TEXT[health.tone])}>· {health.label}</span>
          <span className="text-xs text-muted">
            · {health.meta}
            {seen ? ` ${seen}` : ''}
          </span>
        </div>
      </div>
    </div>
  )
}

function SheetActions({ node, onClose }: { node: Node; onClose: () => void }) {
  const { t } = useLanguage()
  const { toast } = useToast()
  const update = useUpdateNode()
  const remove = useDeleteNode()
  const [issuing, setIssuing] = useState(false)
  const [secret, setSecret] = useState<{ nodeSecret: string; expiresAt: string } | null>(null)
  const [confirmDelete, setConfirmDelete] = useState(false)

  /**
   * Ошибки мутаций различаются по смыслу, а не по коду: конфликт версий значит
   * «перечитай и повтори», лимит — «подожди», остальное показываем как есть.
   */
  function explain(error: unknown): string {
    if (error instanceof ConflictError) return t.errorConflict
    if (error instanceof RateLimitedError) return t.errorRateLimited
    if (error instanceof ApiError) return error.message
    return t.errorOffline
  }

  const busy = update.isPending || remove.isPending

  return (
    <>
      <Button
        size="lg"
        variant="primary"
        disabled={busy}
        onClick={() =>
          update.mutate(
            { node, patch: { state: node.state === 'enabled' ? 'disabled' : 'enabled' } },
            {
              onSuccess: () => toast(t.saved),
              onError: (error) => toast(explain(error), 'danger'),
            },
          )
        }
      >
        <span
          aria-hidden
          className={cn(
            'size-1.75 rounded-full',
            node.state === 'enabled' ? 'bg-danger-solid' : 'bg-ok',
          )}
        />
        {node.state === 'enabled' ? t.disable : t.enable}
      </Button>

      <Button size="lg" variant="secondary" disabled={busy} onClick={() => setIssuing(true)}>
        {t.issueSecret}
      </Button>

      <Button
        size="lg"
        variant={confirmDelete ? 'danger' : 'dangerGhost'}
        className="ml-auto"
        disabled={busy}
        onClick={() => {
          // Удаление ноды уносит каскадом её борды, гранты, конфигурацию и
          // телеметрию, поэтому требует второго нажатия, а не одного.
          if (!confirmDelete) {
            setConfirmDelete(true)
            setTimeout(() => setConfirmDelete(false), 4000)
            return
          }
          remove.mutate(node, {
            onSuccess: () => {
              toast(`${t.delete} · ${node.id}`)
              onClose()
            },
            onError: (error) => toast(explain(error), 'danger'),
          })
        }}
      >
        {confirmDelete ? `${t.delete}?` : t.delete}
      </Button>

      <IssueSecretDialog
        nodeId={issuing ? node.id : null}
        onIssued={setSecret}
        onClose={() => setIssuing(false)}
      />
      <EnrollmentSecretDialog secret={secret} onClose={() => setSecret(null)} />
    </>
  )
}

function SheetBody({ node, agent }: { node: Node; agent: Agent | undefined }) {
  const { t } = useLanguage()
  const users = useUsers()
  const boards = useBoards()

  // Пользователи ноды считаются по размещениям, которые уже пришли со списком.
  const nodeUsers = (users.data?.items ?? []).filter((user) => user.nodeIds.includes(node.id))
  const nodeBoards = (boards.data?.items ?? []).filter((board) => board.nodeId === node.id)

  return (
    <div className="flex flex-col gap-4 px-5.5 pt-4 pb-6">
      <div className="grid grid-cols-3 gap-2.5">
        <Stat label={t.sessions} value={agent?.activeSessions ?? 0} />
        <Stat label="lanes" value={agent?.activeLanes ?? 0} />
        <Stat label={t.users} value={nodeUsers.length} />
      </div>

      <Tabs defaultValue="overview">
        <TabsList>
          <TabsTrigger value="overview">{t.overview}</TabsTrigger>
          <TabsTrigger value="boards">{t.boards}</TabsTrigger>
          <TabsTrigger value="config">TOML</TabsTrigger>
          <TabsTrigger value="logs">{t.logs}</TabsTrigger>
        </TabsList>

        <TabsContent value="overview">
          <OverviewTab node={node} agent={agent} />
        </TabsContent>
        <TabsContent value="boards">
          <BoardsTab nodeId={node.id} boards={nodeBoards} loading={boards.isLoading} />
        </TabsContent>
        <TabsContent value="config">
          <ConfigTab nodeId={node.id} />
        </TabsContent>
        <TabsContent value="logs">
          <LogsTab nodeId={node.id} />
        </TabsContent>
      </Tabs>
    </div>
  )
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-[10px] border border-line bg-canvas px-3.5 py-3">
      <p className="text-[11.5px] font-medium text-dim">{label}</p>
      <p className="mt-1.5 text-xl font-semibold tracking-tight tabular-nums">{value}</p>
    </div>
  )
}

function OverviewTab({ node, agent }: { node: Node; agent: Agent | undefined }) {
  const { t, language } = useLanguage()
  const drift = agent ? agent.appliedRevision !== agent.desiredRevision : false

  const rows: Array<{ label: string; value: string; tone?: 'ok' | 'warn' | 'danger' }> = [
    { label: 'agentVersion', value: agent?.agentVersion ?? '—' },
    { label: 'coreVersion', value: agent?.coreVersion ?? '—' },
    {
      label: t.revision,
      value: agent ? `${agent.appliedRevision} / ${agent.desiredRevision}` : '—',
      tone: drift ? 'warn' : 'ok',
    },
    { label: 'configSha256', value: agent?.appliedSha256?.slice(0, 16) ?? '—' },
    { label: 'bootId', value: agent?.bootId ?? '—' },
    {
      label: t.lastSeen,
      value: absoluteTime(agent?.lastReportAt, language) ?? t.never,
      tone: agent?.online ? 'ok' : 'danger',
    },
    { label: 'grpcListen', value: node.settings.grpcListen },
    { label: 'idleTimeout', value: node.settings.idleTimeout },
  ]

  return (
    <div className="flex flex-col gap-4">
      {agent?.applyError ? (
        <div className="rounded-lg border border-danger-line bg-danger-bg p-3">
          <p className="text-xs font-medium text-danger">{t.lastError}</p>
          <p className="mt-1 font-mono text-xs break-words text-bright">{agent.applyError}</p>
        </div>
      ) : null}

      <dl className="flex flex-col divide-y divide-line rounded-lg border border-line">
        {rows.map((row) => (
          <div key={row.label} className="flex items-center justify-between gap-4 px-3 py-2">
            <dt className="text-xs text-soft">{row.label}</dt>
            <dd
              className={
                row.tone === 'warn'
                  ? 'truncate font-mono text-xs text-warn'
                  : row.tone === 'danger'
                    ? 'truncate font-mono text-xs text-danger'
                    : row.tone === 'ok'
                      ? 'truncate font-mono text-xs text-ok-fg'
                      : 'truncate font-mono text-xs text-bright'
              }
            >
              {row.value}
            </dd>
          </div>
        ))}
      </dl>
    </div>
  )
}

function BoardsTab({
  nodeId,
  boards,
  loading,
}: {
  nodeId: string
  boards: Board[]
  loading: boolean
}) {
  const { t } = useLanguage()
  const { toast } = useToast()
  const update = useUpdateBoard()
  const [creating, setCreating] = useState(false)

  if (loading) return <p className="text-sm text-dim">{t.loading}</p>

  return (
    <div className="flex flex-col gap-2.5">
      {boards.map((board) => (
        <div
          key={board.id}
          className="flex items-center gap-3.5 rounded-[11px] border border-line bg-canvas px-3.75 py-3.25 transition-colors hover:border-line-strong"
        >
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              <span className="truncate text-[13.5px] font-medium">{board.name}</span>
              <span className="font-mono text-[11px] text-muted">{board.id}</span>
            </div>
            <p className="mt-1 truncate font-mono text-[11.5px] text-dim">{board.hash}</p>
          </div>
          <span className="shrink-0 text-[11.5px] text-muted tabular-nums">
            {board.maxLanes} lanes
          </span>
          <Switch
            label={board.name}
            checked={board.state === 'enabled'}
            disabled={update.isPending}
            onCheckedChange={(next) =>
              update.mutate(
                { board, patch: { state: next ? 'enabled' : 'disabled' } },
                {
                  onSuccess: () => toast(t.saved),
                  onError: () => toast(t.errorConflict, 'danger'),
                },
              )
            }
          />
        </div>
      ))}

      <button
        type="button"
        onClick={() => setCreating(true)}
        className="h-9.5 rounded-[11px] border border-dashed border-line-strong text-[13px] font-medium text-soft transition-colors hover:bg-raised"
      >
        + {t.newBoard}
      </button>

      <BoardDialog
        target={creating ? { mode: 'create', nodeId } : null}
        onClose={() => setCreating(false)}
      />
    </div>
  )
}

function ConfigTab({ nodeId }: { nodeId: string }) {
  const { t } = useLanguage()
  const config = useNodeConfig(nodeId)

  if (config.isLoading) return <p className="text-sm text-dim">{t.loading}</p>
  if (!config.data) return <p className="text-sm text-dim">{t.noConfigYet}</p>

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between gap-3">
        <p className="font-mono text-xs text-dim">
          revision {config.data.revision} · sha256 {config.data.configSha256.slice(0, 16)}
        </p>
        <CopyButton size="sm" variant="outline" value={config.data.toml} label="TOML">
          {t.copy}
        </CopyButton>
      </div>
      <pre className="overflow-x-auto rounded-lg border border-line bg-surface p-3 font-mono text-xs text-bright">
        {config.data.toml}
      </pre>
    </div>
  )
}

function LogsTab({ nodeId }: { nodeId: string }) {
  const { t, language } = useLanguage()
  const events = useNodeEvents(nodeId)

  if (events.isLoading) return <p className="text-sm text-dim">{t.loading}</p>
  const rows = events.data?.items ?? []
  if (rows.length === 0) return <p className="text-sm text-dim">{t.noEvents}</p>

  return (
    <ul className="flex flex-col gap-1.5 font-mono text-xs">
      {rows.map((event) => (
        <li key={event.id} className="flex gap-3 rounded-md border border-line px-2.5 py-1.5">
          <span className="shrink-0 text-muted">{relativeTime(event.occurredAt, language)}</span>
          <span className="shrink-0 text-info">{event.type}</span>
          <span className="min-w-0 break-words text-bright">{JSON.stringify(event.payload)}</span>
        </li>
      ))}
    </ul>
  )
}
