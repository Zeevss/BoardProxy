import { useDeferredValue, useEffect, useState, type FormEvent } from 'react'
import type {
  DashboardData, FleetUser, Language, ProvisionedUser, QuotaAction, QuotaPeriod, UserTrafficLimit,
} from '../types'
import type { ControlApi } from '../api/controlApi'
import { PageHeader } from '../components/AppShell'
import { Icon } from '../components/Icon'
import { Field, Modal, SecretResult } from '../components/Modal'
import { Progress } from '../components/Charts'
import { Badge, ConfirmButton, Empty, ErrorBanner, Panel } from '../components/UI'
import { bytes, date } from '../lib/format'

const PERIODS: Array<[Lowercase<QuotaPeriod>, string]> = [
  ['daily', 'Ежедневно'], ['weekly', 'Еженедельно'], ['monthly', 'Ежемесячно'], ['none', 'Без сброса'],
]

const POLICIES: Array<[Lowercase<QuotaAction>, string, string]> = [
  ['reset', 'Сбросить счётчик', 'доступ сохраняется'],
  ['disable', 'Заблокировать', 'до сброса'],
  ['alert', 'Только уведомить', 'без ограничений'],
]

const GIGABYTE = 1000 ** 3

export function Users({ language, data, search, api, onChanged }: {
  language: Language; data: DashboardData; search: string; api: ControlApi; onChanged: () => Promise<unknown> | void
}) {
  const query = useDeferredValue(search.toLowerCase())
  const users = data.users.filter(user =>
    `${user.name} ${user.id} ${user.placements.map(p => p.nodeName).join(' ')}`.toLowerCase().includes(query))
  const [create, setCreate] = useState(false)
  const [openUser, setOpenUser] = useState<string>()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string>()
  const selected = data.users.find(user => user.id === openUser)

  async function action(run: () => Promise<unknown>) {
    setBusy(true); setError(undefined)
    try { await run(); await onChanged() } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) } finally { setBusy(false) }
  }

  return <>
    <PageHeader language={language} section="users" action={
      <button className="button primary" type="button" disabled={!data.nodes.length} onClick={() => setCreate(true)}>
        <Icon name="plus" size={15}/> Добавить
      </button>
    }/>
    {error ? <ErrorBanner onClose={() => setError(undefined)}>{error}</ErrorBanner> : null}

    <Panel>
      {users.length ? <div className="table-scroll"><table>
        <thead><tr>
          <th>Пользователь</th><th>Состояние</th><th>Ноды</th><th>Устройства</th><th>Страницы</th>
          <th>Трафик / лимит</th><th>Сброс</th><th/>
        </tr></thead>
        <tbody>{users.map(user => <tr className="clickable" key={user.id} onClick={() => setOpenUser(user.id)}>
          <td><strong>{user.name}</strong><small>{user.id}</small></td>
          <td><Badge tone={stateTone(user.state)}>{stateLabel(user.state)}</Badge></td>
          <td><div className="node-tags">{user.placements.map(placement =>
            <span className="node-tag" key={placement.nodeId} title={placement.boards.map(b => b.name).join(', ')}>
              {placement.nodeName}
            </span>)}</div></td>
          <td className="mono">{user.limits.maxDevices || '∞'}</td>
          <td className="mono">{user.limits.maxPages}</td>
          <td><TrafficCell traffic={user.limits.traffic}/></td>
          <td className="mono">{user.limits.traffic ? periodLabel(user.limits.traffic.period) : '—'}</td>
          <td><Icon name="chevron" size={14}/></td>
        </tr>)}</tbody>
      </table></div> : <Empty
        title={data.users.length ? 'Ничего не найдено' : 'Пользователей пока нет'}
        text={data.users.length ? undefined : 'Создайте пользователя — подписку и ключи бэкенд сформирует сам.'}
      />}
    </Panel>

    {create ? <CreateUserModal data={data} api={api} busy={busy} onAction={action} onClose={() => setCreate(false)}/> : null}
    {selected ? <UserDrawer
      user={selected} api={api} onAction={action} onClose={() => setOpenUser(undefined)}
    /> : null}
  </>
}

function TrafficCell({ traffic }: { traffic?: UserTrafficLimit }) {
  if (!traffic) return <span className="muted">Без лимита</span>
  const percent = traffic.limitBytes ? (traffic.usedBytes / traffic.limitBytes) * 100 : 0
  return <div className="quota-cell">
    <span>{bytes(traffic.usedBytes)} / {bytes(traffic.limitBytes)}</span>
    <Progress value={percent} color={percent > 90 ? '#f2635f' : percent > 75 ? '#f0b429' : '#4fd1a5'}/>
  </div>
}

function UserDrawer({ user, api, onAction, onClose }: {
  user: FleetUser; api: ControlApi
  onAction: (run: () => Promise<unknown>) => Promise<void>; onClose: () => void
}) {
  const [link, setLink] = useState<string>()
  const subscriptionId = user.subscription?.id
  useEffect(() => {
    if (!subscriptionId) return
    const controller = new AbortController()
    void api.subscriptionLink(subscriptionId)
      .then(result => { if (!controller.signal.aborted && result?.url) setLink(result.url) })
      .catch(() => undefined)
    return () => controller.abort()
  }, [api, subscriptionId])
  const traffic = user.limits.traffic
  return <div className="drawer-backdrop" onMouseDown={event => { if (event.target === event.currentTarget) onClose() }}>
    <aside className="drawer">
      <header className="drawer-header">
        <div>
          <span className={`dot ${stateTone(user.state) === 'ok' ? 'ok' : stateTone(user.state) === 'bad' ? 'bad' : 'neutral'}`}/>
          <h2>{user.name}</h2>
          <Badge tone={stateTone(user.state)}>{stateLabel(user.state)}</Badge>
          <p>{user.id} · изменён {date(user.updatedAt)}</p>
        </div>
        <button type="button" className="icon-button" onClick={onClose}><Icon name="close"/></button>
      </header>

      <div className="drawer-body">
        <SubscriptionLink user={user} link={link} onRotate={() => void onAction(async () => {
          if (!subscriptionId) return
          const current = await api.get<{ version: number }>(`/api/v1/subscriptions/${encodeURIComponent(subscriptionId)}`)
          const rotated = await api.rotateSubscription(subscriptionId, current.version)
          setLink(rotated.url ?? undefined)
        })}/>

        <div className="detail-metrics">
          <div><span>Трафик за период</span><strong>{traffic ? bytes(traffic.usedBytes) : '—'}</strong></div>
          <div><span>Устройства</span><strong>{user.limits.maxDevices || '∞'}</strong></div>
          <div><span>Страницы</span><strong>{user.limits.maxPages}</strong></div>
          <div><span>Сброс</span><strong>{traffic ? periodLabel(traffic.period) : '—'}</strong></div>
        </div>

        <h3>Доступные ноды</h3>
        <div className="compact-list">{user.placements.map(placement => <div key={placement.nodeId}>
          <strong>{placement.nodeName}</strong>
          <Badge tone={stateTone(placement.state)}>{stateLabel(placement.state)}</Badge>
          <span>{placement.boards.map(board => board.name).join(', ') || 'без бордов'}</span>
        </div>)}</div>

        <h3>Лимиты</h3>
        <div className="compact-list">
          <div><strong>Лимит устройств</strong><span>{user.limits.maxDevices || 'без лимита'}</span></div>
          <div><strong>Лимит страниц</strong><span>{user.limits.maxPages}</span></div>
          <div><strong>Лимит трафика</strong><span>{traffic ? bytes(traffic.limitBytes) : 'без лимита'}</span></div>
          {traffic ? <div><strong>При достижении</strong><span>{policyLabel(traffic.action)}</span></div> : null}
        </div>

        <div className="drawer-actions">
          <ConfirmButton className="button secondary" onConfirm={() => void onAction(() => Promise.all(
            user.placements.map(placement => api.putUser(placement.nodeId, user.id, placement.version, {
              name: user.name, publicKey: null,
              state: user.state === 'enabled' ? 'disabled' : 'enabled',
              maxSessions: user.limits.maxDevices, maxLanes: user.limits.maxPages,
            })),
          ))}>{user.state === 'enabled' ? 'Выключить' : 'Включить'}</ConfirmButton>
        </div>
        <p className="form-note">
          Изменение состояния применяется ко всем нодам пользователя: доступ должен закрываться целиком, а не частично.
        </p>
      </div>
    </aside>
  </div>
}

/** Ссылка постоянная: секреты подписки хранятся зашифрованными и восстанавливаются по запросу. */
function SubscriptionLink({ user, link, onRotate }: {
  user: FleetUser; link?: string; onRotate: () => void
}) {
  if (!user.subscription) {
    return <div className="subscription-block">
      <Empty title="Подписка не выпущена" text="У пользователя нет подписки — ключи выдаются напрямую."/>
    </div>
  }
  return <div className="subscription-block">
    {link ? <>
      <SecretResult label="Ссылка подписки" value={link}/>
      <p className="form-note">
        Ссылка постоянная: смена ключей или нод доезжает при следующем запросе клиента.
        Обновление выдаёт новую ссылку и немедленно обесценивает прежнюю.
      </p>
    </> : <div className="subscription-hidden">
      <strong>{user.subscription.name}</strong>
      <span>Ссылка недоступна: доставка подписками выключена либо подписка выпущена до включения постоянных ссылок.</span>
    </div>}
    <ConfirmButton className="button secondary" onConfirm={onRotate}>Обновить ссылку</ConfirmButton>
  </div>
}

function CreateUserModal({ data, api, busy, onAction, onClose }: {
  data: DashboardData; api: ControlApi; busy: boolean
  onAction: (run: () => Promise<unknown>) => Promise<void>; onClose: () => void
}) {
  const [nodes, setNodes] = useState<string[]>(data.nodes[0] ? [data.nodes[0].nodeId] : [])
  const [policy, setPolicy] = useState<Lowercase<QuotaAction>>('reset')
  const [issued, setIssued] = useState<ProvisionedUser>()

  return <Modal
    title="Добавить пользователя"
    hint="Имя и доступные ноды обязательны. Остальное — лимиты: пустое поле означает отсутствие лимита."
    busy={busy}
    submitLabel={issued ? undefined : 'Создать пользователя'}
    onClose={onClose}
    onSubmit={(event: FormEvent<HTMLFormElement>) => {
      event.preventDefault()
      const form = new FormData(event.currentTarget)
      const name = String(form.get('name')).trim()
      const trafficGb = Number(form.get('traffic'))
      void onAction(async () => {
        // Лимит уходит вместе с пользователем: бэкенд ставит квоту в той же транзакции.
        setIssued(await api.provisionUser({
          id: slug(name), name,
          targets: nodes.map(nodeId => ({
            nodeId,
            boardIds: data.boards.filter(board => board.nodeId === nodeId && board.assigned).map(board => board.id),
            keyName: null,
          })),
          maxSessions: Number(form.get('devices')) || 0,
          maxLanes: Number(form.get('pages')) || 1,
          traffic: trafficGb > 0
            ? { limitBytes: Math.round(trafficGb * GIGABYTE), period: String(form.get('period')), action: policy }
            : null,
        }))
      })
    }}
  >
    {issued ? <div className="issued-results">
      {issued.subscriptionUrl
        ? <SecretResult label="Ссылка подписки" value={issued.subscriptionUrl}/>
        : issued.keys.map(key => <SecretResult key={key.id} label={`${key.name} · ${key.nodeId}`} value={key.keylink}/>)}
      <p className="form-note">
        Приватный ключ и токен подписки сгенерированы на сервере и в панель больше не возвращаются.
      </p>
    </div> : <>
      <Field label="Имя пользователя"><input required name="name" placeholder="Алиса"/></Field>

      <div className="modal-section-title">Доступные ноды</div>
      <div className="node-picker-grid">{data.nodes.map(node => {
        const picked = nodes.includes(node.nodeId)
        return <button
          type="button" key={node.nodeId} className={picked ? 'node-option picked' : 'node-option'}
          onClick={() => setNodes(picked ? nodes.filter(item => item !== node.nodeId) : [...nodes, node.nodeId])}
        >
          <span className="node-option-box">{picked ? <Icon name="check" size={12}/> : null}</span>
          <span><strong>{node.name}</strong><small>{node.boards} бордов</small></span>
        </button>
      })}</div>
      <p className="field-hint">{nodes.length
        ? `Выбрано нод: ${nodes.length}`
        : 'Выберите хотя бы одну ноду.'}</p>

      <div className="modal-section-title">Лимиты <span className="optional-mark">пусто — без лимита</span></div>
      <div className="field-row">
        <Field label="Лимит устройств"><input name="devices" type="number" min="0" placeholder="0"/></Field>
        <Field label="Лимит страниц"><input name="pages" type="number" min="1" max="32" defaultValue="4"/></Field>
      </div>
      <div className="field-row">
        <Field label="Лимит трафика, ГБ"><input name="traffic" type="number" min="0" step="0.1" placeholder="0"/></Field>
        <Field label="Период сброса">
          <select name="period" defaultValue="monthly">
            {PERIODS.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
          </select>
        </Field>
      </div>

      <Field label="При достижении лимита">
        <div className="policy-grid">{POLICIES.map(([value, label, hint]) => <button
          type="button" key={value} className={policy === value ? 'policy-option picked' : 'policy-option'}
          onClick={() => setPolicy(value)}
        ><strong>{label}</strong><small>{hint}</small></button>)}</div>
      </Field>
    </>}
  </Modal>
}

function slug(name: string) {
  const base = name.trim().toLowerCase().replace(/[^a-z0-9._-]+/g, '-').replace(/^-+|-+$/g, '')
  // Кириллическое имя целиком выпадает из допустимого набора — тогда нужен стабильный запасной id.
  return base || `user-${Date.now().toString(36)}`
}

function stateTone(state: string) { return state === 'enabled' ? 'ok' : state === 'revoked' ? 'bad' : 'neutral' as const }
function stateLabel(state: string) { return state === 'enabled' ? 'включён' : state === 'revoked' ? 'отозван' : 'выключен' }
function periodLabel(period: string) { return PERIODS.find(([value]) => value === period)?.[1] ?? period }
function policyLabel(action: string) { return POLICIES.find(([value]) => value === action)?.[1] ?? action }
