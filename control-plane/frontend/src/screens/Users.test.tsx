import { render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { Users } from './Users'
import type { ControlApi } from '../api/controlApi'
import type { DashboardData, FleetUser } from '../types'

const EMPTY: DashboardData = {
  nodes: [], statuses: {}, runtimes: {}, interfaceTraffic: [], userTraffic: [], interfaceTotals: [], userTotals: [],
  events: [], quotas: [], users: [], boards: [], subscriptions: [], tokens: [], certificates: [], revisions: [],
}

const alice: FleetUser = {
  id: 'alice', name: 'Алиса', state: 'enabled',
  placements: [
    { nodeId: 'node-a', nodeName: 'Frankfurt', state: 'enabled', boards: [{ id: 'primary', name: 'Primary' }], version: 1 },
    { nodeId: 'node-b', nodeName: 'Amsterdam', state: 'disabled', boards: [{ id: 'corp', name: 'Corp' }], version: 1 },
  ],
  limits: {
    maxDevices: 3, maxPages: 5,
    traffic: {
      limitBytes: 200_000_000_000, usedBytes: 50_000_000_000, period: 'monthly', action: 'reset',
      enabled: true, exceeded: false, periodStart: '2026-08-01T00:00:00Z', periodEnd: '2026-09-01T00:00:00Z',
    },
  },
  subscription: { id: 'sub-1', name: 'Алиса', state: 'enabled' },
  updatedAt: '2026-08-16T00:00:00Z',
}

function renderUsers(data: Partial<DashboardData>, api: Partial<ControlApi> = {}) {
  const stub = { subscriptionLink: vi.fn().mockResolvedValue(undefined), ...api }
  render(<Users language="ru" data={{ ...EMPTY, ...data }} search="" api={stub as unknown as ControlApi} onChanged={vi.fn()}/>)
}

describe('Users — флотовая таблица', () => {
  it('показывает пользователя один раз со всеми его нодами', () => {
    renderUsers({ users: [alice] })

    const row = screen.getByText('Алиса').closest('tr')!
    expect(within(row).getByText('Frankfurt')).toBeInTheDocument()
    expect(within(row).getByText('Amsterdam')).toBeInTheDocument()
    expect(screen.getAllByText('Алиса')).toHaveLength(1)
  })

  it('показывает расход и лимит трафика вместе', () => {
    renderUsers({ users: [alice] })

    const row = screen.getByText('Алиса').closest('tr')!
    expect(within(row).getByText('50.0 GB / 200 GB')).toBeInTheDocument()
    expect(within(row).getByText('Ежемесячно')).toBeInTheDocument()
  })

  it('пользователь без квоты помечается как безлимитный', () => {
    const bob: FleetUser = { ...alice, id: 'bob', name: 'Боб', subscription: undefined, limits: { maxDevices: 0, maxPages: 2 } }
    renderUsers({ users: [bob] })

    const row = screen.getByText('Боб').closest('tr')!
    expect(within(row).getByText('Без лимита')).toBeInTheDocument()
    // Ноль устройств означает отсутствие ограничения, а не запрет входа.
    expect(within(row).getByText('∞')).toBeInTheDocument()
  })

  it('пустой список приглашает создать пользователя', () => {
    renderUsers({})
    expect(screen.getByText('Пользователей пока нет')).toBeInTheDocument()
  })
})

describe('Users — карточка пользователя', () => {
  it('показывает постоянную ссылку подписки', async () => {
    renderUsers({ users: [alice] }, {
      subscriptionLink: vi.fn().mockResolvedValue({ url: 'https://subscribe.example.com/s/tok#bp1=capsule' }),
    })

    screen.getByText('Алиса').click()

    expect(await screen.findByText('https://subscribe.example.com/s/tok#bp1=capsule')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Обновить ссылку' })).toBeInTheDocument()
  })

  it('объясняет отсутствие ссылки, когда доставка подписками выключена', async () => {
    renderUsers({ users: [alice] }, { subscriptionLink: vi.fn().mockResolvedValue({ url: null }) })

    screen.getByText('Алиса').click()

    expect(await screen.findByText(/доставка подписками выключена/)).toBeInTheDocument()
  })

  it('показывает состояние пользователя на каждой ноде отдельно', async () => {
    renderUsers({ users: [alice] })

    screen.getByText('Алиса').click()

    const nodes = (await screen.findByText('Доступные ноды')).nextElementSibling!
    expect(within(nodes as HTMLElement).getByText('включён')).toBeInTheDocument()
    expect(within(nodes as HTMLElement).getByText('выключен')).toBeInTheDocument()
  })
})
