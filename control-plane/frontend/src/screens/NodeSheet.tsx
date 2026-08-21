import { useState } from 'react'
import {
  useAgents,
  useBoards,
  useDeleteNode,
  useIssueEnrollmentToken,
  useNodeConfig,
  useNodeEvents,
  useToggleBoard,
  useUpdateNode,
} from '@/api/nodes'
import type { Agent, Board, Node } from '@/api/types'
import { ApiError, ConflictError, RateLimitedError } from '@/api/errors'
import { useLanguage } from '@/app/language'
import { Button } from '@/components/ui/button'
import { CopyButton } from '@/components/ui/copy'
import { Sheet } from '@/components/ui/sheet'
import { Badge, StatusDot } from '@/components/ui/status'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useToast } from '@/components/ui/toast'
import { absoluteTime, relativeTime } from '@/lib/format'
import { nodeHealth } from '@/lib/health'
import { EnrollmentSecretDialog } from './EnrollmentSecretDialog'

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

function SheetTitle({ node, agent }: { node: Node; agent: Agent | undefined }) {
  const { t, language } = useLanguage()
  const health = nodeHealth(node, agent, t)
  const seen = relativeTime(agent?.lastReportAt, language)

  return (
    <div className="flex items-start gap-3">
      <StatusDot tone={health.tone} live={health.live} className="mt-1.5" />
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <h2 className="truncate text-base font-medium">{node.name}</h2>
          <Badge tone={health.tone}>{health.label}</Badge>
        </div>
        <p className="mt-0.5 truncate font-mono text-xs text-dim">
          {node.id} · {health.meta}
          {seen ? ` · ${seen}` : ''}
        </p>
      </div>
    </div>
  )
}

function SheetActions({ node, onClose }: { node: Node; onClose: () => void }) {
  const { t } = useLanguage()
  const { toast } = useToast()
  const update = useUpdateNode()
  const remove = useDeleteNode()
  const issue = useIssueEnrollmentToken()
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

  const busy = update.isPending || remove.isPending || issue.isPending

  return (
    <>
      <Button
        size="sm"
        disabled={busy}
        onClick={() =>
          update.mutate(
            { node, patch: { state: node.state === 'enabled' ? 'disabled' : 'enabled' } },
            {
              onSuccess: () => toast(t.save),
              onError: (error) => toast(explain(error), 'danger'),
            },
          )
        }
      >
        {node.state === 'enabled' ? t.disable : t.enable}
      </Button>

      <Button
        size="sm"
        variant="outline"
        disabled={busy}
        onClick={() =>
          issue.mutate(
            { nodeId: node.id, hubUrl: window.location.origin },
            {
              onSuccess: (issued) => setSecret(issued),
              onError: (error) => toast(explain(error), 'danger'),
            },
          )
        }
      >
        {t.issueSecret}
      </Button>

      <Button
        size="sm"
        variant={confirmDelete ? 'danger' : 'dangerGhost'}
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

      <EnrollmentSecretDialog secret={secret} onClose={() => setSecret(null)} />
    </>
  )
}

function SheetBody({ node, agent }: { node: Node; agent: Agent | undefined }) {
  const { t } = useLanguage()

  return (
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
        <BoardsTab nodeId={node.id} />
      </TabsContent>
      <TabsContent value="config">
        <ConfigTab nodeId={node.id} />
      </TabsContent>
      <TabsContent value="logs">
        <LogsTab nodeId={node.id} />
      </TabsContent>
    </Tabs>
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

function BoardsTab({ nodeId }: { nodeId: string }) {
  const { t } = useLanguage()
  const { toast } = useToast()
  const boards = useBoards()
  const toggle = useToggleBoard()
  const rows = (boards.data?.items ?? []).filter((board) => board.nodeId === nodeId)

  if (boards.isLoading) return <p className="text-sm text-dim">{t.loading}</p>
  if (rows.length === 0) return <p className="text-sm text-dim">{t.nothingFound}</p>

  return (
    <ul className="flex flex-col gap-2">
      {rows.map((board) => (
        <li
          key={board.id}
          className="flex items-center justify-between gap-3 rounded-lg border border-line px-3 py-2.5"
        >
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="truncate text-sm">{board.name}</span>
              <span className="font-mono text-xs text-muted">{board.id}</span>
            </div>
            <p className="truncate font-mono text-xs text-dim">
              hash {board.hash} · {board.maxLanes} lanes
            </p>
          </div>
          <Button
            size="sm"
            variant={board.state === 'enabled' ? 'secondary' : 'outline'}
            disabled={toggle.isPending}
            onClick={() =>
              toggle.mutate(board as Board, {
                onSuccess: () => toast(t.save),
                onError: () => toast(t.errorConflict, 'danger'),
              })
            }
          >
            {board.state === 'enabled' ? t.disable : t.enable}
          </Button>
        </li>
      ))}
    </ul>
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
