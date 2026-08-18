import { useDeferredValue, useMemo, useState, type FormEvent } from 'react'
import type { DashboardData, FleetBoard, Language } from '../types'
import type { ControlApi } from '../api/controlApi'
import { PageHeader } from '../components/AppShell'
import { Icon } from '../components/Icon'
import { Field, Modal } from '../components/Modal'
import { Badge, ConfirmButton, Empty, ErrorBanner } from '../components/UI'
import { short } from '../lib/format'

type NodeGroup = { nodeId: string; nodeName: string; nodeState: string; boards: FleetBoard[] }

export function Boards({ language, data, search, api, onChanged }: {
  language: Language; data: DashboardData; search: string; api: ControlApi; onChanged: () => Promise<unknown> | void
}) {
  const query = useDeferredValue(search.toLowerCase())
  const [create, setCreate] = useState<string>()
  const [move, setMove] = useState<FleetBoard>()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string>()

  // Ноды берутся из флота, а не из бордов: нода без бордов обязана показать пустое состояние.
  const groups = useMemo<NodeGroup[]>(() => data.nodes.map(node => ({
    nodeId: node.nodeId,
    nodeName: node.name,
    nodeState: node.state,
    boards: data.boards.filter(board =>
      board.nodeId === node.nodeId &&
      `${board.name} ${board.id} ${board.hash}`.toLowerCase().includes(query)),
  })), [data.nodes, data.boards, query])

  async function action(run: () => Promise<unknown>) {
    setBusy(true); setError(undefined)
    try { await run(); await onChanged() } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) } finally { setBusy(false) }
  }

  function catalogVersion(nodeId: string) {
    const node = data.nodes.find(item => item.nodeId === nodeId)
    if (!node) throw new Error(`Нода ${nodeId} не найдена`)
    return node.version
  }

  return <>
    <PageHeader language={language} section="boards" action={
      <button className="button primary" type="button" disabled={!data.nodes.length} onClick={() => setCreate(data.nodes[0]?.nodeId)}>
        <Icon name="plus" size={15}/> Новый борд
      </button>
    }/>
    {error ? <ErrorBanner onClose={() => setError(undefined)}>{error}</ErrorBanner> : null}

    {groups.length ? groups.map(group => <section className="board-group" key={group.nodeId}>
      <header className="board-group-header">
        <div>
          <h2>{group.nodeName}</h2>
          <code>{group.nodeId}</code>
          <span>{group.boards.length} бордов</span>
        </div>
        <button className="button secondary" type="button" onClick={() => setCreate(group.nodeId)}>
          <Icon name="plus" size={14}/> Добавить борд
        </button>
      </header>

      {group.boards.length ? <div className="board-grid">{group.boards.map(board => <BoardCard
        key={`${board.nodeId}:${board.id}`}
        board={board}
        runtimeState={data.runtimes[board.nodeId]?.boards.find(item => item.boardTag === board.id)}
        sessions={data.runtimes[board.nodeId]?.sessions.filter(session => session.boardTag === board.id).length ?? 0}
        canMove={data.nodes.length > 1}
        onToggle={() => void action(() => api.putBoard(
          board.nodeId, board.id, catalogVersion(board.nodeId),
          boardBody(board, board.state === 'enabled' ? 'disabled' : 'enabled'),
        ))}
        onMove={() => setMove(board)}
        onDetach={() => void action(async () => {
          const version = catalogVersion(board.nodeId)
          const assignment = await api.replaceAssignment(board.nodeId, version, {
            boardIds: data.boards.filter(item => item.nodeId === board.nodeId && item.assigned && item.id !== board.id).map(item => item.id),
            users: [],
          })
          await api.removeBoard(board.nodeId, board.id, assignment.catalog.version)
        })}
      />)}</div> : <Empty title="На этой ноде пока нет бордов" text="Добавьте борд, чтобы нода начала обслуживать сессии."/>}
    </section>) : <Empty title="Нод пока нет" text="Сначала создайте ноду — борды живут на ней."/>}

    {create ? <BoardModal
      data={data} nodeId={create} busy={busy}
      onClose={() => setCreate(undefined)}
      onSubmit={(nodeId, body, boardId) => void action(async () => {
        const version = catalogVersion(nodeId)
        const created = await api.putBoard(nodeId, boardId, version, body)
        const assigned = data.boards.filter(item => item.nodeId === nodeId && item.assigned).map(item => item.id)
        await api.replaceAssignment(nodeId, created.catalog.version, {
          boardIds: assigned.includes(boardId) ? assigned : [...assigned, boardId],
          users: [],
        })
        setCreate(undefined)
      })}
    /> : null}

    {move ? <MoveBoardModal
      board={move} data={data} busy={busy}
      onClose={() => setMove(undefined)}
      onSubmit={target => void action(async () => {
        // Перенос — это создание на цели и снятие с источника; каталог у каждой ноды свой.
        const created = await api.putBoard(target, move.id, catalogVersion(target), boardBody(move, move.state))
        const targetAssigned = data.boards.filter(item => item.nodeId === target && item.assigned).map(item => item.id)
        await api.replaceAssignment(target, created.catalog.version, {
          boardIds: [...targetAssigned, move.id], users: [],
        })
        const sourceVersion = catalogVersion(move.nodeId)
        const sourceAssignment = await api.replaceAssignment(move.nodeId, sourceVersion, {
          boardIds: data.boards.filter(item => item.nodeId === move.nodeId && item.assigned && item.id !== move.id).map(item => item.id),
          users: [],
        })
        await api.removeBoard(move.nodeId, move.id, sourceAssignment.catalog.version)
        setMove(undefined)
      })}
    /> : null}
  </>
}

function BoardCard({ board, runtimeState, sessions, canMove, onToggle, onMove, onDetach }: {
  board: FleetBoard
  runtimeState?: { state: string; error: string }
  sessions: number
  canMove: boolean
  onToggle: () => void
  onMove: () => void
  onDetach: () => void
}) {
  const tone = runtimeState?.error ? 'warn' : runtimeState?.state === 'running' ? 'ok' : board.state === 'revoked' ? 'bad' : 'info'
  return <article className="board-card">
    <header>
      <div>
        <span className={`dot ${runtimeState?.error ? 'warn' : board.state === 'enabled' ? 'ok' : board.state === 'revoked' ? 'bad' : 'neutral'}`}/>
        <h2>{board.name}</h2>
        <code>{board.id}</code>
      </div>
      <Badge tone={tone}>{runtimeState?.state ?? board.state}</Badge>
    </header>
    <p className="board-hash">sha256:{short(board.hash, 30)}</p>
    {runtimeState?.error ? <p className="board-error">{runtimeState.error}</p> : null}
    <dl>
      <div><dt>Полосы</dt><dd>{board.maxLanes}</dd></div>
      <div><dt>Сессии</dt><dd>{sessions}</dd></div>
      <div><dt>Пользователи</dt><dd>{board.users}</dd></div>
      <div><dt>Ревизия</dt><dd>{board.version}</dd></div>
    </dl>
    <footer>
      {board.state !== 'revoked' ? <button type="button" onClick={onToggle}>
        {board.state === 'enabled' ? 'Выключить' : 'Включить'}
      </button> : null}
      {canMove ? <button type="button" onClick={onMove}>Перенести</button> : null}
      {board.users === 0
        ? <ConfirmButton onConfirm={onDetach}>Отвязать</ConfirmButton>
        : <span className="board-locked" title="Сначала отвяжите пользователей от борда">{board.users} польз.</span>}
    </footer>
  </article>
}

function BoardModal({ data, nodeId, busy, onClose, onSubmit }: {
  data: DashboardData; nodeId: string; busy: boolean; onClose: () => void
  onSubmit: (nodeId: string, body: Record<string, unknown>, boardId: string) => void
}) {
  return <Modal
    title="Новый борд"
    hint="Борд принадлежит одной ноде, на ноде может быть несколько бордов. Добавление поднимает ревизию каталога только выбранной ноды."
    busy={busy}
    submitLabel="Создать борд"
    onClose={onClose}
    onSubmit={(event: FormEvent<HTMLFormElement>) => {
      event.preventDefault()
      const form = new FormData(event.currentTarget)
      const boardId = String(form.get('id')).trim()
      onSubmit(String(form.get('nodeId')), {
        name: String(form.get('name')).trim(),
        hash: String(form.get('hash')).trim(),
        hubSlide: String(form.get('hubSlide')).trim() || null,
        apiBase: String(form.get('apiBase')).trim() || null,
        guestName: String(form.get('guestName')).trim() || null,
        state: 'enabled',
        maxLanes: Number(form.get('maxLanes')),
      }, boardId)
    }}
  >
    <Field label="Нода">
      <select name="nodeId" defaultValue={nodeId}>
        {data.nodes.map(node => <option key={node.nodeId} value={node.nodeId}>{node.name} · {node.nodeId}</option>)}
      </select>
    </Field>
    <div className="field-row">
      <Field label="ID борда"><input required name="id" pattern="[A-Za-z0-9][A-Za-z0-9._]*(?:-[A-Za-z0-9._]+)*" placeholder="primary"/></Field>
      <Field label="Название"><input required name="name" placeholder="Primary board"/></Field>
    </div>
    <Field label="Хеш борда" hint="Идентификатор подключения, выданный доской"><input required name="hash"/></Field>
    <div className="field-row">
      <Field label="Полос на сессию"><input required type="number" min="1" max="32" name="maxLanes" defaultValue="2"/></Field>
      <Field label="Hub slide"><input name="hubSlide"/></Field>
    </div>
    <div className="field-row">
      <Field label="API base"><input name="apiBase"/></Field>
      <Field label="Guest name"><input name="guestName"/></Field>
    </div>
  </Modal>
}

function MoveBoardModal({ board, data, busy, onClose, onSubmit }: {
  board: FleetBoard; data: DashboardData; busy: boolean; onClose: () => void; onSubmit: (target: string) => void
}) {
  const targets = data.nodes.filter(node => node.nodeId !== board.nodeId)
  return <Modal
    title={`Перенести «${board.name}»`}
    hint="Борд снимается с текущей ноды и создаётся на выбранной. Обе ноды получат новую ревизию каталога."
    busy={busy}
    submitLabel="Перенести"
    onClose={onClose}
    onSubmit={(event: FormEvent<HTMLFormElement>) => {
      event.preventDefault()
      onSubmit(String(new FormData(event.currentTarget).get('target')))
    }}
  >
    <Field label="Куда переносим">
      <select required name="target" defaultValue={targets[0]?.nodeId}>
        {targets.map(node => <option key={node.nodeId} value={node.nodeId}>{node.name} · {node.nodeId}</option>)}
      </select>
    </Field>
    <p className="form-note">
      Пользователи борда останутся на исходной ноде — перенос не переносит их доступ.
    </p>
  </Modal>
}

function boardBody(board: FleetBoard, state: string) {
  return {
    name: board.name, hash: board.hash,
    hubSlide: board.hubSlide ?? null, apiBase: board.apiBase ?? null, guestName: board.guestName ?? null,
    state, maxLanes: board.maxLanes,
  }
}
