import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { Settings } from './Settings'
import type { ControlApi } from '../api/controlApi'
import type { DashboardData, SubscriptionService } from '../types'

const EMPTY: DashboardData = {
  nodes: [], statuses: {}, runtimes: {}, interfaceTraffic: [], userTraffic: [], interfaceTotals: [], userTotals: [],
  events: [], quotas: [], users: [], boards: [], subscriptions: [], tokens: [], certificates: [], revisions: [],
}

function service(status: Partial<SubscriptionService['status']>): SubscriptionService {
  return {
    settings: {
      enabled: true, serviceName: 'BoardProxy', icon: '◈', publicUrl: 'https://subscribe.example.com',
      yandexEditorUrl: 'https://disk.yandex.ru/i/sheet', recoveryKeyId: 'recovery-2026-01',
      apps: [{ platform: 'android', url: 'https://example.com/android' }],
      revision: 4, updatedAt: '2026-08-16T00:00:00Z',
    },
    status: { tokenIssued: false, connected: false, ...status },
  }
}

function renderSettings(data: Partial<DashboardData>) {
  const api = {} as ControlApi
  render(<Settings language="ru" data={{ ...EMPTY, ...data }} api={api} onChanged={vi.fn()}/>)
}

describe('Settings — система подписок', () => {
  it('сообщает о недоступном сервисе, когда control-plane не отдаёт эндпоинт', () => {
    renderSettings({})
    expect(screen.getByText('Сервис подписок недоступен')).toBeInTheDocument()
  })

  it('предлагает выпустить токен, пока он не выпущен', () => {
    renderSettings({ subscriptionService: service({ tokenIssued: false }) })
    expect(screen.getByRole('button', { name: /Выпустить токен/ })).toBeInTheDocument()
    expect(screen.getByText('не настроен')).toBeInTheDocument()
  })

  it('блокирует настройки, пока сервис ни разу не пришёл за конфигом', () => {
    renderSettings({ subscriptionService: service({ tokenIssued: true, connected: false }) })
    expect(screen.getByText('Ожидание подключения')).toBeInTheDocument()
    expect(screen.getByLabelText(/URL страницы подписки/)).toBeDisabled()
  })

  it('разблокирует настройки и показывает статус при подключённом сервисе', () => {
    renderSettings({
      subscriptionService: service({
        tokenIssued: true, connected: true, serviceVersion: '1.4.0', appliedRevision: 4,
        lastSeenAt: new Date().toISOString(), recoveryWatcherReady: true,
      }),
    })
    expect(screen.getByText('подключен')).toBeInTheDocument()
    const publicUrl = screen.getByLabelText(/URL страницы подписки/)
    expect(publicUrl).toBeEnabled()
    expect(publicUrl).toHaveValue('https://subscribe.example.com')
    expect(screen.getByLabelText('Android')).toHaveValue('https://example.com/android')
  })
})
