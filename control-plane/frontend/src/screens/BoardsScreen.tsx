import { useMemo, useState } from 'react'
import { ChevronRight, Plus } from 'lucide-react'
import { useAgents, useBoards, useNodes } from '@/api/nodes'
import type { Agent, Board } from '@/api/types'
import { useLanguage } from '@/app/language'
import { EmptyState, ScreenHeader } from '@/components/ScreenHeader'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge, StatusDot } from '@/components/ui/status'
import { plural } from '@/lib/format'
import { nodeHealth } from '@/lib/health'
import { BoardDialog, type BoardTarget } from './BoardDialog'

export function BoardsScreen() {
  const { t, language } = useLanguage()
  const [search, setSearch] = useState('')
  const [target, setTarget] = useState<BoardTarget | null>(null)

  const nodes = useNodes()
  const boards = useBoards()
  const agents = useAgents()

  const agentById = useMemo(() => {
    const map = new Map<string, Agent>()
    for (const agent of agents.data ?? []) map.set(agent.id, agent)
    return map
  }, [agents.data])

  /**
   * Доски сгруппированы по нодам, а не показаны плоским списком: доска
   * принадлежит ровно одной ноде, и её состояние читается только вместе с
   * состоянием ноды, которая её раздаёт.
   */
  const groups = useMemo(() => {
    const query = search.trim().toLowerCase()
    const matches = (board: Board) =>
      !query || `${board.id} ${board.name} ${board.hash} ${board.nodeId}`.toLowerCase().includes(query)

    return (nodes.data?.items ?? [])
      .filter((node) => node.state !== 'revoked')
      .map((node) => ({
        node,
        health: nodeHealth(node, agentById.get(node.id), t),
        boards: (boards.data?.items ?? []).filter(
          (board) => board.nodeId === node.id && matches(board),
        ),
      }))
      // При поиске пустые группы только мешают: они не ответ на запрос.
      .filter((group) => !query || group.boards.length > 0)
  }, [nodes.data, boards.data, agentById, t, search])

  const loading = nodes.isLoading || boards.isLoading

  return (
    <section className="mx-auto flex max-w-6xl flex-col gap-4.5">
      <ScreenHeader
        title={t.boards}
        subtitle={t.boardsSub}
        actions={
          <Input
            type="search"
            placeholder={t.search}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            className="w-40 sm:w-55"
          />
        }
      />

      {loading ? (
        <EmptyState>{t.loading}</EmptyState>
      ) : groups.length === 0 ? (
        <EmptyState>{search.trim() ? t.nothingFound : t.emptyFleet}</EmptyState>
      ) : (
        groups.map((group) => (
          <section
            key={group.node.id}
            className="overflow-hidden rounded-xl border border-line bg-canvas"
          >
            <header className="flex items-center gap-3 border-b border-line bg-raised px-4.5 py-3">
              <StatusDot tone={group.health.tone} live={group.health.live} className="size-2" />
              <div className="flex min-w-0 flex-1 flex-wrap items-baseline gap-2.5">
                <span className="text-[13.5px] font-semibold">{group.node.name}</span>
                <span className="font-mono text-[11.5px] text-dim">{group.node.id}</span>
              </div>
              <span className="shrink-0 text-xs tabular-nums text-dim">
                {group.boards.length}{' '}
                {plural(
                  group.boards.length,
                  { one: t.boardOne, few: t.boardFew, many: t.boardMany },
                  language,
                )}
              </span>
              <Button
                size="xs"
                variant="outline"
                className="shrink-0"
                onClick={() => setTarget({ mode: 'create', nodeId: group.node.id })}
              >
                <Plus />
                {t.add}
              </Button>
            </header>

            {group.boards.length === 0 ? (
              <p className="px-4.5 py-4 text-[12.5px] text-muted">{t.noBoards}</p>
            ) : (
              <ul>
                {group.boards.map((board) => (
                  <BoardRow
                    key={board.id}
                    board={board}
                    onEdit={() => setTarget({ mode: 'edit', board })}
                  />
                ))}
              </ul>
            )}
          </section>
        ))
      )}

      <BoardDialog target={target} onClose={() => setTarget(null)} />
    </section>
  )
}

function BoardRow({ board, onEdit }: { board: Board; onEdit: () => void }) {
  const { t } = useLanguage()
  const on = board.state === 'enabled'

  return (
    <li className="border-b border-line-soft last:border-b-0">
      <button
        type="button"
        onClick={onEdit}
        className="flex w-full items-center gap-3.5 px-4 py-3 text-left transition-colors hover:bg-raised"
      >
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <div className="flex min-w-0 items-center gap-2">
            <span className="truncate text-[13.5px] font-medium">{board.name}</span>
            <Badge tone={on ? 'ok' : 'muted'} className="shrink-0 px-1.75 py-0 text-[11px]">
              {on ? t.enabled : t.disabled}
            </Badge>
          </div>
          <div className="flex min-w-0 items-center gap-1.75 font-mono text-[11px] text-muted">
            <span className="shrink-0">{board.id}</span>
            <span className="shrink-0 text-line-strong">·</span>
            <span className="truncate">{board.hash}</span>
            <span className="shrink-0 text-line-strong">·</span>
            <span className="shrink-0">{board.maxLanes} lanes</span>
          </div>
        </div>
        <span className="flex shrink-0 items-center gap-1 text-[12.5px] text-dim">
          {t.edit}
          <ChevronRight className="size-3.5" />
        </span>
      </button>
    </li>
  )
}
