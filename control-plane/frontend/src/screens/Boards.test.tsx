import { render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { Boards } from './Boards'
import type { ControlApi } from '../api/controlApi'
import type { DashboardData, FleetBoard, NodeSummary } from '../types'

const EMPTY: DashboardData = {
  nodes: [], statuses: {}, runtimes: {}, interfaceTraffic: [], userTraffic: [], interfaceTotals: [], userTotals: [],
  events: [], quotas: [], users: [], boards: [], subscriptions: [], tokens: [], certificates: [], revisions: [],
}

const nodes: NodeSummary[] = [
  { nodeId: 'node-a', name: 'Frankfurt', state: 'enabled', boards: 2, users: 2, version: 7, updatedAt: '2026-08-16T00:00:00Z' },
  { nodeId: 'node-b', name: 'Amsterdam', state: 'enabled', boards: 0, users: 0, version: 3, updatedAt: '2026-08-16T00:00:00Z' },
]

function board(overrides: Partial<FleetBoard> = {}): FleetBoard {
  return {
    nodeId: 'node-a', nodeName: 'Frankfurt', nodeState: 'enabled',
    id: 'primary', name: 'Primary', hash: 'a'.repeat(64),
    hubSlide: 'slide-7', apiBase: 'https://api.example', guestName: 'guest',
    state: 'enabled', maxLanes: 4, assigned: true, users: 0, version: 2,
    updatedAt: '2026-08-16T00:00:00Z', ...overrides,
  }
}

function renderBoards(data: Partial<DashboardData>, api: Partial<ControlApi> = {}) {
  render(<Boards language="ru" data={{ ...EMPTY, ...data }} search="" api={api as ControlApi} onChanged={vi.fn()}/>)
}

describe('Boards — группировка по нодам', () => {
  it('показывает каждую ноду отдельной группой', () => {
    renderBoards({ nodes, boards: [board(), board({ id: 'media', name: 'Media' })] })

    expect(screen.getByText('Frankfurt')).toBeInTheDocument()
    expect(screen.getByText('Amsterdam')).toBeInTheDocument()
    expect(screen.getByText('Primary')).toBeInTheDocument()
    expect(screen.getByText('Media')).toBeInTheDocument()
  })

  it('нода без бордов показывает пустое состояние, а не исчезает', () => {
    renderBoards({ nodes, boards: [board()] })

    expect(screen.getByText('Amsterdam')).toBeInTheDocument()
    expect(screen.getByText('На этой ноде пока нет бордов')).toBeInTheDocument()
  })

  it('без нод предлагает начать с ноды', () => {
    renderBoards({})
    expect(screen.getByText('Нод пока нет')).toBeInTheDocument()
  })
})

describe('Boards — мутации', () => {
  it('переключение состояния шлёт версию каталога ноды и сохраняет необязательные поля', async () => {
    const putBoard = vi.fn().mockResolvedValue({ catalog: { version: 8 } })
    renderBoards({ nodes, boards: [board()] }, { putBoard })

    screen.getByRole('button', { name: 'Выключить' }).click()

    expect(putBoard).toHaveBeenCalledWith('node-a', 'primary', 7, expect.objectContaining({
      state: 'disabled',
      // Поля, которых нет в таблице, обязаны доехать обратно, иначе бэкенд их затрёт.
      hubSlide: 'slide-7', apiBase: 'https://api.example', guestName: 'guest',
      hash: 'a'.repeat(64), maxLanes: 4,
    }))
  })

  it('борд с пользователями нельзя отвязать', () => {
    renderBoards({ nodes, boards: [board({ users: 3 })] })

    expect(screen.queryByRole('button', { name: 'Отвязать' })).not.toBeInTheDocument()
    expect(screen.getByText('3 польз.')).toBeInTheDocument()
  })

  it('перенос не предлагается, когда нода одна', () => {
    renderBoards({ nodes: [nodes[0]], boards: [board()] })

    expect(screen.queryByRole('button', { name: 'Перенести' })).not.toBeInTheDocument()
  })

  it('показывает число пользователей борда и полосы', () => {
    renderBoards({ nodes, boards: [board({ users: 2 })] })

    const card = screen.getByText('Primary').closest('article')!
    expect(within(card).getByText('Полосы').nextElementSibling).toHaveTextContent('4')
    expect(within(card).getByText('Пользователи').nextElementSibling).toHaveTextContent('2')
  })
})
