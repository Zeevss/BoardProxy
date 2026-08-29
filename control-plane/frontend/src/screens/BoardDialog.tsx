import { ChevronRight } from 'lucide-react'
import { useState } from 'react'
import { useCreateBoard, useDeleteBoard, useNodes, useUpdateBoard } from '@/api/nodes'
import type { Board, Node } from '@/api/types'
import { ApiError, ConflictError } from '@/api/errors'
import { useLanguage } from '@/app/language'
import { Button } from '@/components/ui/button'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Modal, ModalSection } from '@/components/ui/modal'
import { StatusDot } from '@/components/ui/status'
import { useToast } from '@/components/ui/toast'
import { boardHash, boardId } from '@/lib/board-link'
import { cn } from '@/lib/utils'

/** Что открыто: новая доска на выбранной ноде или правка существующей. */
export type BoardTarget = { mode: 'create'; nodeId: string } | { mode: 'edit'; board: Board }

export function BoardDialog({
  target,
  onClose,
}: {
  target: BoardTarget | null
  onClose: () => void
}) {
  const { t } = useLanguage()
  const { toast } = useToast()
  const nodes = useNodes()
  const create = useCreateBoard()
  const update = useUpdateBoard()
  const remove = useDeleteBoard()

  const board = target?.mode === 'edit' ? target.board : null

  const [openFor, setOpenFor] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [link, setLink] = useState('')
  const [nodeId, setNodeId] = useState('')
  const [lanes, setLanes] = useState('4')
  const [apiBase, setApiBase] = useState('')
  const [advanced, setAdvanced] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Ключ цели: правку одной доски нельзя показать полями другой.
  const key = target
    ? target.mode === 'edit'
      ? `edit:${target.board.nodeId}/${target.board.id}`
      : `create:${target.nodeId}`
    : null

  if (target && key !== openFor) {
    setOpenFor(key)
    setName(board?.name ?? '')
    // В режиме правки хэш уже разобран — ссылки хаб не хранит.
    setLink(board?.hash ?? '')
    setNodeId(target.mode === 'edit' ? target.board.nodeId : target.nodeId)
    setLanes(String(board?.maxLanes ?? 4))
    setApiBase(board?.apiBase ?? '')
    setAdvanced(false)
    setConfirmDelete(false)
    setError(null)
  }
  if (!target && openFor !== null) setOpenFor(null)

  const hash = boardHash(link)
  const busy = create.isPending || update.isPending || remove.isPending

  function explain(cause: unknown): string {
    if (cause instanceof ConflictError) return t.errorConflict
    if (cause instanceof ApiError) return cause.message
    return t.errorOffline
  }

  async function save() {
    setError(null)
    if (!hash) {
      setError(t.boardLinkHint)
      return
    }
    try {
      if (board) {
        await update.mutateAsync({
          board,
          patch: {
            name: name.trim() || hash,
            hash,
            maxLanes: clampLanes(lanes),
            apiBase: apiBase.trim() || null,
          },
        })
      } else {
        const title = name.trim() || hash
        await create.mutateAsync({
          id: boardId(title, hash),
          nodeId,
          name: title,
          hash,
          maxLanes: clampLanes(lanes),
        })
      }
      toast(t.saved)
      onClose()
    } catch (cause) {
      setError(explain(cause))
    }
  }

  return (
    <Modal
      open={target !== null}
      onOpenChange={(value) => !value && onClose()}
      title={board ? t.editBoard : t.newBoard}
      subtitle={board ? `${board.id} · ${board.nodeId}` : t.boardModalHint}
      className="max-w-[620px]"
      footer={
        <div className="flex w-full items-center gap-2">
          {board ? (
            <Button
              variant={confirmDelete ? 'danger' : 'dangerGhost'}
              disabled={busy}
              onClick={() => {
                if (!confirmDelete) {
                  setConfirmDelete(true)
                  setTimeout(() => setConfirmDelete(false), 4000)
                  return
                }
                remove.mutate(board, {
                  onSuccess: () => {
                    toast(`${t.delete} · ${board.id}`)
                    onClose()
                  },
                  onError: (cause) => setError(explain(cause)),
                })
              }}
            >
              {confirmDelete ? `${t.delete}?` : t.delete}
            </Button>
          ) : null}
          <div className="ml-auto flex gap-2">
            <Button variant="outline" disabled={busy} onClick={onClose}>
              {t.cancel}
            </Button>
            <Button variant="primary" disabled={busy} onClick={() => void save()}>
              {board ? t.save : t.create}
            </Button>
          </div>
        </div>
      }
    >
      <ModalSection title={t.secBasics}>
        <Field label={t.boardNameOptional}>
          <Input
            placeholder="Main board"
            className="bg-canvas"
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </Field>
        <Field label={t.boardLink} hint={hash ? `hash · ${hash}` : t.boardLinkHint}>
          <Input
            autoFocus
            placeholder="https://…/?hash=…"
            className="bg-canvas font-mono"
            value={link}
            onChange={(event) => setLink(event.target.value)}
          />
        </Field>
      </ModalSection>

      <ModalSection title={t.node} hint={board ? t.boardNodeLocked : t.boardNodeHint}>
        <div className="grid gap-2 sm:grid-cols-[repeat(auto-fill,minmax(178px,1fr))]">
          {(nodes.data?.items ?? [])
            .filter((node) => node.state !== 'revoked')
            .map((node) => (
              <NodeOption
                key={node.id}
                node={node}
                active={nodeId === node.id}
                // Хаб берёт nodeId из пути и молча игнорирует его в теле, так
                // что перенос доски правкой не работает: у существующей доски
                // выбор заблокирован, чтобы не показывать несбыточное.
                locked={board !== null}
                onPick={() => setNodeId(node.id)}
              />
            ))}
        </div>
      </ModalSection>

      <section className="overflow-hidden rounded-xl border border-line bg-raised">
        <button
          type="button"
          aria-expanded={advanced}
          onClick={() => setAdvanced(!advanced)}
          className="flex w-full items-center justify-between px-4 py-3.5 text-[12.5px] font-semibold tracking-[0.04em] uppercase"
        >
          {t.secAdvanced}
          <ChevronRight
            className={cn('size-4 text-dim transition-transform duration-200', advanced && 'rotate-90')}
          />
        </button>
        {advanced ? (
          <div className="grid gap-3 px-4 pb-4 sm:grid-cols-2">
            <Field label="maxLanes" hint="1…32">
              <Input
                inputMode="numeric"
                className="bg-canvas font-mono"
                value={lanes}
                onChange={(event) => setLanes(event.target.value.replace(/\D/g, ''))}
              />
            </Field>
            <Field label="apiBase" hint={t.apiBaseHint}>
              <Input
                placeholder="https://api.example.net"
                className="bg-canvas font-mono"
                value={apiBase}
                onChange={(event) => setApiBase(event.target.value)}
              />
            </Field>
          </div>
        ) : null}
      </section>

      {error ? (
        <p
          role="alert"
          className="rounded-lg border border-danger-line bg-danger-bg px-3 py-2 text-xs text-danger"
        >
          {error}
        </p>
      ) : null}
    </Modal>
  )
}

function clampLanes(value: string): number {
  return Math.min(32, Math.max(1, Number(value) || 4))
}

function NodeOption({
  node,
  active,
  locked,
  onPick,
}: {
  node: Node
  active: boolean
  locked: boolean
  onPick: () => void
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      disabled={locked}
      onClick={onPick}
      className={cn(
        'flex items-center gap-2.25 rounded-[9px] border px-3 py-2.5 text-left transition-colors',
        active ? 'border-fg bg-line' : 'border-line bg-canvas',
        locked ? 'cursor-not-allowed opacity-60' : 'hover:border-line-strong',
      )}
    >
      <StatusDot tone={node.state === 'enabled' ? 'ok' : 'muted'} className="size-1.75" />
      <span className="min-w-0">
        <span className={cn('block truncate text-[12.5px] font-medium', active ? 'text-fg' : 'text-bright')}>
          {node.name}
        </span>
        <span className="mt-0.5 block truncate font-mono text-[10.5px] text-dim">{node.id}</span>
      </span>
    </button>
  )
}
