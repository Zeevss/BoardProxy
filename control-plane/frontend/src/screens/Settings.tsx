import { useState, type FormEvent } from 'react'
import type { DashboardData, IssuedApiToken, IssuedServiceToken, Language, SubscriptionPlatform, SubscriptionService } from '../types'
import { SUBSCRIPTION_PLATFORMS } from '../types'
import type { ControlApi } from '../api/controlApi'
import { PageHeader } from '../components/AppShell'
import { Icon } from '../components/Icon'
import { Field, Modal, SecretResult } from '../components/Modal'
import { Badge, ConfirmButton, Empty, ErrorBanner, Panel } from '../components/UI'
import { ago, date, short } from '../lib/format'

const PLATFORM_LABELS: Record<SubscriptionPlatform, string> = {
  ios: 'iOS', android: 'Android', windows: 'Windows', macos: 'macOS', linux: 'Linux',
}

export function Settings({ language, data, api, onChanged }: { language: Language; data: DashboardData; api: ControlApi; onChanged: () => Promise<unknown> | void }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string>()
  async function action(run: () => Promise<unknown>) {
    setBusy(true); setError(undefined)
    try { await run(); await onChanged() } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) } finally { setBusy(false) }
  }
  return <>
    <PageHeader language={language} section="settings"/>
    {error ? <ErrorBanner onClose={() => setError(undefined)}>{error}</ErrorBanner> : null}
    <SubscriptionSystem service={data.subscriptionService} api={api} busy={busy} onAction={action}/>
    <ControlPlaneRuntime/>
    <AccessInventory data={data} api={api} busy={busy} onAction={action}/>
  </>
}

/**
 * Система подписок. Control-plane — владелец настроек, сервис subscribe знает только
 * свой токен и адрес хаба, всё остальное забирает сам. Пока сервис ни разу не пришёл
 * за конфигом, поля заблокированы: сохранять настройки некому.
 */
function SubscriptionSystem({ service, api, busy, onAction }: { service?: SubscriptionService; api: ControlApi; busy: boolean; onAction: (run: () => Promise<unknown>) => Promise<void> }) {
  const [issued, setIssued] = useState<IssuedServiceToken>()
  const [showToken, setShowToken] = useState(false)

  if (!service) {
    return <Panel title="Система подписок" meta="Отдаёт страницы подписок и ссылки на клиенты для пользователей.">
      <Empty title="Сервис подписок недоступен" text="Control-plane не отдаёт /api/v1/subscription-service. Обновите control-plane, чтобы управлять подписками из панели."/>
    </Panel>
  }

  const { settings, status } = service
  const connected = status.connected
  const state = !status.tokenIssued ? 'no-token' : connected ? 'connected' : 'waiting'

  return <>
    <Panel
      title="Система подписок"
      meta="Отдаёт страницы подписок и ссылки на клиенты для пользователей."
      action={<ServiceStateBadge state={state}/>}
    >
      {state === 'no-token' ? <div className="service-bootstrap">
        <p>Выпустите API-токен и впишите его в <code>SUBSCRIBE_CONTROL_PLANE_TOKEN</code> сервиса подписок. Как только сервис придёт за конфигом, настройки ниже разблокируются.</p>
        <button className="button primary" type="button" disabled={busy} onClick={() => setShowToken(true)}>
          <Icon name="plus" size={15}/> Выпустить токен
        </button>
      </div> : null}

      {state === 'waiting' ? <div className="service-bootstrap waiting">
        <span className="auth-spinner"/>
        <div>
          <strong>Ожидание подключения</strong>
          <p>Токен выпущен, но сервис подписок ещё ни разу не запрашивал конфигурацию. Проверьте <code>SUBSCRIBE_CONTROL_PLANE_URL</code> и <code>SUBSCRIBE_CONTROL_PLANE_TOKEN</code>, затем запустите контейнер.</p>
        </div>
        <button className="button secondary" type="button" disabled={busy} onClick={() => setShowToken(true)}>Перевыпустить токен</button>
      </div> : null}

      {connected ? <div className="detail-metrics service-metrics">
        <div><span>Версия сервиса</span><strong>{status.serviceVersion ?? '—'}</strong></div>
        <div><span>Применённая ревизия</span><strong className={status.appliedRevision === settings.revision ? undefined : 'warn'}>{status.appliedRevision ?? '—'} / {settings.revision}</strong></div>
        <div><span>Последний сигнал</span><strong>{ago(status.lastSeenAt)}</strong></div>
        <div><span>Яндекс-канал</span><strong>{status.recoveryWatcherReady === undefined ? '—' : status.recoveryWatcherReady ? 'готов' : 'недоступен'}</strong></div>
        <div><span>Recovery key</span><strong>{settings.recoveryKeyId || '—'}</strong></div>
        <div><span>Публичный ключ</span><strong>{short(status.recoveryPublicKey, 16)}</strong></div>
      </div> : null}

      <SubscriptionForm
        service={service}
        locked={!connected}
        busy={busy}
        onSave={patch => void onAction(() => api.saveSubscriptionService(patch, settings.revision))}
        onRestart={() => void onAction(() => api.restartSubscriptionService())}
      />
    </Panel>

    {showToken ? <Modal
      title="Токен сервиса подписок"
      hint="Токен показывается один раз. Перевыпуск немедленно отзывает предыдущий."
      busy={busy}
      submitLabel={issued ? undefined : 'Выпустить токен'}
      onClose={() => { setShowToken(false); setIssued(undefined) }}
      onSubmit={(event: FormEvent<HTMLFormElement>) => {
        event.preventDefault()
        void onAction(async () => { setIssued(await api.issueSubscriptionServiceToken()) })
      }}
    >
      {issued ? <div className="issued-results">
        <SecretResult label="SUBSCRIBE_CONTROL_PLANE_TOKEN" value={issued.secret}/>
        <div className="command-result">
          <span>Следующий шаг</span>
          <code>Впишите токен в .env сервиса подписок и выполните: docker compose --profile subscribe up -d subscribe</code>
        </div>
        <p className="form-note">Больше сервису ничего не нужно: публичный URL, ссылка на Яндекс Таблицу, recovery-ключ и ссылки на клиенты приезжают с этой страницы.</p>
      </div> : <p className="form-note">Токен получает роль SUBSCRIBER: он умеет резолвить подписки и забирать конфигурацию, но не видит каталог нод.</p>}
    </Modal> : null}
  </>
}

function ServiceStateBadge({ state }: { state: 'no-token' | 'waiting' | 'connected' }) {
  if (state === 'connected') return <Badge tone="ok">подключен</Badge>
  if (state === 'waiting') return <Badge tone="warn">ожидание</Badge>
  return <Badge tone="neutral">не настроен</Badge>
}

function SubscriptionForm({ service, locked, busy, onSave, onRestart }: {
  service: SubscriptionService; locked: boolean; busy: boolean
  onSave: (patch: Record<string, unknown>) => void; onRestart: () => void
}) {
  const { settings } = service
  return <form className="settings-form" onSubmit={(event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    onSave({
      enabled: form.get('enabled') === 'on',
      serviceName: String(form.get('serviceName')).trim(),
      icon: String(form.get('icon')).trim(),
      publicUrl: String(form.get('publicUrl')).trim(),
      yandexEditorUrl: String(form.get('yandexEditorUrl')).trim(),
      recoveryKeyId: String(form.get('recoveryKeyId')).trim(),
      apps: SUBSCRIPTION_PLATFORMS
        .map(platform => ({ platform, url: String(form.get(`app-${platform}`) ?? '').trim() }))
        .filter(app => app.url !== ''),
    })
  }}>
    <fieldset disabled={locked || busy}>
      <label className="check-field">
        <input type="checkbox" name="enabled" defaultChecked={settings.enabled}/>
        Выдавать пользователям ссылку подписки вместо отдельных keylink
      </label>

      <div className="field-row">
        <Field label="Название сервиса"><input name="serviceName" defaultValue={settings.serviceName} placeholder="BoardProxy"/></Field>
        <Field label="Иконка" hint="Одна-две emoji или буква"><input name="icon" maxLength={4} defaultValue={settings.icon} placeholder="◈"/></Field>
      </div>

      <Field label="URL страницы подписки" hint="Публичный HTTPS-адрес сервиса subscribe">
        <input name="publicUrl" type="url" defaultValue={settings.publicUrl} placeholder="https://subscribe.example.com"/>
      </Field>
      <Field label="URL редактирования подписки" hint="Общая редакторская ссылка Яндекс Таблицы для резервного канала">
        <input name="yandexEditorUrl" type="url" defaultValue={settings.yandexEditorUrl} placeholder="https://disk.yandex.ru/i/…"/>
      </Field>
      <Field label="Recovery key ID" hint="Приватный ключ генерирует control-plane; сервис получает его вместе с конфигурацией">
        <input name="recoveryKeyId" defaultValue={settings.recoveryKeyId} placeholder="recovery-2026-01"/>
      </Field>

      <div className="modal-section-title">Ссылки на клиенты</div>
      <div className="platform-links">
        {SUBSCRIPTION_PLATFORMS.map(platform => <Field key={platform} label={PLATFORM_LABELS[platform]}>
          <input name={`app-${platform}`} type="url" defaultValue={settings.apps.find(app => app.platform === platform)?.url ?? ''} placeholder="https://…"/>
        </Field>)}
      </div>

      <div className="drawer-actions">
        <button className="button primary" type="submit">Сохранить</button>
        <ConfirmButton className="button secondary" onConfirm={onRestart}>Перезапустить сервис</ConfirmButton>
      </div>
    </fieldset>
    {locked ? <p className="form-note">Настройки редактируются только при подключённом сервисе — иначе применять их некому.</p> : null}
  </form>
}

/**
 * Runtime панели пока приходит из .env и меняется передеплоем, поэтому блок только
 * показывает, где эти значения лежат, и не притворяется редактируемым.
 */
function ControlPlaneRuntime() {
  return <Panel title="Панель управления" meta="Эндпоинты хаба, хранение метрик и аккаунт оператора." className="section-gap">
    <Empty
      title="Настраивается через .env"
      text="Внешний URL, gRPC/HTTP listen, хранение метрик и аккаунт администратора задаются переменными CONTROL_* и применяются при перезапуске control-plane."
    />
  </Panel>
}

function AccessInventory({ data, api, busy, onAction }: { data: DashboardData; api: ControlApi; busy: boolean; onAction: (run: () => Promise<unknown>) => Promise<void> }) {
  const [issue, setIssue] = useState(false)
  const [issued, setIssued] = useState<IssuedApiToken>()
  return <>
    <Panel title="API-токены" className="section-gap" meta="Секрет возвращается ровно один раз" action={<button className="button secondary" type="button" onClick={() => setIssue(true)}><Icon name="plus" size={15}/> Выпустить API-токен</button>}>
      {data.tokens.length ? <div className="table-scroll"><table>
        <thead><tr><th>Название</th><th>Роль</th><th>Создан</th><th>Истекает</th><th>Последнее использование</th><th/></tr></thead>
        <tbody>{data.tokens.map(token => <tr key={token.id}>
          <td><strong>{token.name}</strong><small>{short(token.id)}</small></td>
          <td><span className={`role-chip ${token.role.toLowerCase()}`}>{token.role}</span></td>
          <td className="mono">{date(token.createdAt)}</td>
          <td className="mono">{date(token.expiresAt)}</td>
          <td className="mono">{date(token.lastUsedAt)}</td>
          <td className="row-actions">{token.revokedAt ? <Badge tone="bad">отозван</Badge> : <ConfirmButton onConfirm={() => void onAction(() => api.revokeToken(token.id))}>Отозвать</ConfirmButton>}</td>
        </tr>)}</tbody>
      </table></div> : <Empty title="Токенов нет" text="Панель работает по сессии, а не по API-токену — здесь появляются только машинные интеграции."/>}
    </Panel>

    <Panel title="Сертификаты нод" meta="Приватные ключи никогда не покидают ноду" className="section-gap">
      {data.certificates.length ? <div className="table-scroll"><table>
        <thead><tr><th>Нода</th><th>Серийный номер</th><th>Fingerprint</th><th>Истекает</th><th>Состояние</th><th/></tr></thead>
        <tbody>{data.certificates.map(cert => <tr key={cert.serialNumber}>
          <td>{cert.nodeId}</td>
          <td className="mono">{cert.serialNumber}</td>
          <td className="mono">{short(cert.fingerprintSha256, 28)}</td>
          <td className="mono">{date(cert.expiresAt)}</td>
          <td><Badge tone={cert.revokedAt ? 'bad' : 'ok'}>{cert.revokedAt ? 'отозван' : 'действует'}</Badge></td>
          <td className="row-actions">{cert.revokedAt ? null : <ConfirmButton onConfirm={() => void onAction(() => api.revokeCertificate(cert.nodeId, cert.serialNumber, 'Отозван из настроек панели'))}>Отозвать</ConfirmButton>}</td>
        </tr>)}</tbody>
      </table></div> : <Empty title="Сертификатов для выбранной ноды нет"/>}
    </Panel>

    {issue ? <Modal title="Выпуск API-токена" hint="Открытый токен показывается один раз; в базе остаётся только его SHA-256." busy={busy} submitLabel={issued ? undefined : 'Выпустить'} onClose={() => { setIssue(false); setIssued(undefined) }} onSubmit={(event: FormEvent<HTMLFormElement>) => {
      event.preventDefault()
      const form = new FormData(event.currentTarget)
      void onAction(async () => { setIssued(await api.issueToken({ name: String(form.get('name')), role: String(form.get('role')), ttlSeconds: form.get('ttl') ? Number(form.get('ttl')) : null })) })
    }}>
      {issued ? <SecretResult label="API-токен" value={issued.secret}/> : <>
        <Field label="Название"><input required name="name" placeholder="deploy-bot"/></Field>
        <Field label="Роль"><select name="role" defaultValue="VIEWER"><option>VIEWER</option><option>OPERATOR</option><option>ADMIN</option></select></Field>
        <Field label="TTL, секунд" hint="Пусто — без срока"><input name="ttl" type="number" min="60"/></Field>
      </>}
    </Modal> : null}
  </>
}
