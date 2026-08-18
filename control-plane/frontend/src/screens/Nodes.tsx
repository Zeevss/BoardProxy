import { useDeferredValue, useEffect, useState, type FormEvent } from 'react'
import type { AppliedConfig, Catalog, DashboardData, Language, NodeSummary } from '../types'
import type { ControlApi } from '../api/controlApi'
import { PageHeader } from '../components/AppShell'
import { Modal, Field, SecretResult } from '../components/Modal'
import { Badge, ConfirmButton, Empty, ErrorBanner, Panel } from '../components/UI'
import { Icon } from '../components/Icon'
import { ago, date, message, short } from '../lib/format'

type Tab = 'overview' | 'runtime' | 'config' | 'toml' | 'certificate' | 'logs'

export function Nodes({ language, data, search, api, onChanged }: { language: Language; data: DashboardData; search: string; api: ControlApi; onChanged: () => Promise<unknown> | void }) {
  const query = useDeferredValue(search.toLowerCase())
  const rows = data.nodes.filter(node => `${node.name} ${node.nodeId} ${node.state}`.toLowerCase().includes(query))
  const [openNode, setOpenNode] = useState<string>()
  const [create, setCreate] = useState(false)
  const [enroll, setEnroll] = useState<string>()
  const [secret, setSecret] = useState<string>()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string>()
  const selected = data.nodes.find(node => node.nodeId === openNode)
  async function action(run: () => Promise<unknown>) { setBusy(true); setError(undefined); try { await run() } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) } finally { await onChanged(); setBusy(false) } }
  const drift = data.nodes.filter(node => { const status = data.statuses[node.nodeId]; return status && status.desiredRevision !== status.appliedRevision })
  return <>
    <PageHeader language={language} section="nodes" action={<div className="header-actions">{data.nodes.length ? <button className="button secondary" type="button" onClick={() => setEnroll(data.nodes[0]?.nodeId)}>Выпустить секрет</button> : null}<button className="button primary" type="button" onClick={() => setCreate(true)}><Icon name="plus" size={15}/> Создать ноду</button></div>}/>
    {error ? <ErrorBanner onClose={() => setError(undefined)}>{error}</ErrorBanner> : null}
    {drift.length ? <div className="warning-banner"><Icon name="warning" size={16}/><span>{drift.length} node(s) have not applied the latest desired revision.</span></div> : null}
    <Panel>{rows.length ? <div className="table-scroll"><table className="nodes-table"><thead><tr><th>Node</th><th>State</th><th>Session</th><th>Revision</th><th>Boards</th><th>Users</th><th>Sessions</th><th>Last seen</th><th/></tr></thead><tbody>{rows.map(node => {
      const status = data.statuses[node.nodeId]
      const runtime = data.runtimes[node.nodeId]
      const isDrift = status && status.desiredRevision !== status.appliedRevision
      return <tr key={node.nodeId} className="clickable" onClick={() => setOpenNode(node.nodeId)}><td><strong>{node.name}</strong><small>{node.nodeId}</small></td><td><Badge tone={node.state === 'enabled' ? 'ok' : node.state === 'revoked' ? 'bad' : 'neutral'}>{node.state}</Badge></td><td><span className={`connection ${status?.connected ? 'ok' : 'bad'}`}><span className="dot"/>{status?.connected ? 'streaming' : 'no stream'}</span></td><td className={isDrift ? 'mono warn-text' : 'mono'}>{status ? `${status.appliedRevision} / ${status.desiredRevision}` : '—'}</td><td className="mono">{node.boards}</td><td className="mono">{node.users}</td><td className="mono">{runtime?.sessions.length ?? 0}</td><td className="mono">{ago(status?.lastSeen)}</td><td><button className="icon-button" type="button"><Icon name="chevron" size={15}/></button></td></tr>
    })}</tbody></table></div> : <Empty title={data.nodes.length ? 'No matching nodes' : 'Нод пока нет'} text={data.nodes.length ? undefined : 'Создайте desired-state каталог и первую доску, затем подключите node-agent.'}/>}</Panel>
    {selected ? <NodeDrawer node={selected} data={data} api={api} busy={busy} onClose={() => setOpenNode(undefined)} onAction={action} onEnroll={() => setEnroll(selected.nodeId)}/> : null}
    {enroll ? <Modal title="Issue one-time node secret" hint="The node creates its private key locally and uses this secret only for enrollment." busy={busy} submitLabel={secret ? undefined : 'Issue secret'} onClose={() => { setEnroll(undefined); setSecret(undefined) }} onSubmit={event => { event.preventDefault(); const form = new FormData(event.currentTarget); void action(async () => { const result = await api.issueEnrollment(String(form.get('nodeId')), String(form.get('hubUrl')), Number(form.get('ttl'))); setSecret(result.nodeSecret) }) }}>
      {secret ? <EnrollmentResult secret={secret}/> : <><Field label="Node"><select name="nodeId" defaultValue={enroll}>{data.nodes.map(node => <option key={node.nodeId} value={node.nodeId}>{node.name} · {node.nodeId}</option>)}</select></Field><Field label="Hub URL"><input required name="hubUrl" defaultValue="hub:8443"/></Field><Field label="TTL, seconds"><input required min="60" max="86400" name="ttl" type="number" defaultValue="900"/></Field></>}
    </Modal> : null}
    {create ? <Modal title="Создание ноды" hint="Панель создаст первый каталог и сразу выпустит enrollment-секрет. Серверный приватный ключ генерируется локально в браузере и не отображается." busy={busy} submitLabel={secret ? undefined : 'Создать и выпустить секрет'} onClose={() => { setCreate(false); setSecret(undefined) }} onSubmit={(event: FormEvent<HTMLFormElement>) => { event.preventDefault(); const form = new FormData(event.currentTarget); void action(async () => {
      const nodeId = String(form.get('nodeId')).trim()
      const hubUrl = String(form.get('hubUrl')).trim()
      await api.createCatalog(initialCatalog({
        nodeId,
        nodeName: String(form.get('nodeDisplayName')).trim(),
        boardId: String(form.get('boardId')).trim(),
        boardName: String(form.get('boardDisplayName')).trim(),
        boardHash: String(form.get('boardHash')).trim(),
        maxLanes: Number(form.get('maxLanes')),
        allowPrivateEgress: form.get('allowPrivateEgress') === 'on',
        idleTimeout: String(form.get('idleTimeout')).trim(),
        grpcListen: String(form.get('grpcListen')).trim(),
        httpListen: String(form.get('httpListen')).trim() || null,
        logLevel: String(form.get('logLevel')),
      }))
      const result = await api.issueEnrollment(nodeId, hubUrl, 900)
      setSecret(result.nodeSecret)
    }) }}>
      {secret ? <EnrollmentResult secret={secret}/> : <>
        <div className="field-row"><Field label="ID ноды"><input required name="nodeId" pattern="[A-Za-z0-9][A-Za-z0-9._]*(?:-[A-Za-z0-9._]+)*" defaultValue="node-1"/></Field><Field label="Название"><input required name="nodeDisplayName" defaultValue="Primary node"/></Field></div>
        <div className="modal-section-title">Первая доска</div>
        <div className="field-row"><Field label="ID доски"><input required name="boardId" pattern="[A-Za-z0-9][A-Za-z0-9._]*(?:-[A-Za-z0-9._]+)*" defaultValue="primary"/></Field><Field label="Название"><input required name="boardDisplayName" defaultValue="Primary board"/></Field></div>
        <Field label="Board hash" hint="Идентификатор подключения, выданный доской"><input required name="boardHash" placeholder="Вставьте hash доски"/></Field>
        <div className="field-row"><Field label="Полос на сессию"><input required name="maxLanes" type="number" min="1" max="32" defaultValue="2"/></Field><Field label="Hub URL"><input required name="hubUrl" defaultValue="hub:8443"/></Field></div>
        <div className="modal-section-title">Параметры core <span className="optional-mark">пустое поле — значение по умолчанию</span></div>
        <div className="field-row"><Field label="Idle timeout"><input name="idleTimeout" defaultValue="PT1M30S"/></Field><Field label="Уровень логов"><select name="logLevel" defaultValue="info"><option>trace</option><option>debug</option><option>info</option><option>warn</option><option>error</option></select></Field></div>
        <div className="field-row"><Field label="gRPC listen"><input name="grpcListen" defaultValue="unix:///run/bproxy/control.sock"/></Field><Field label="HTTP listen"><input name="httpListen" placeholder="127.0.0.1:8081"/></Field></div>
        <label className="check-field"><input type="checkbox" name="allowPrivateEgress"/> Разрешить приватный egress</label>
      </>}
    </Modal> : null}
  </>
}

function EnrollmentResult({ secret }: { secret: string }) {
  // Команду показываем целиком и копируемой: её переносят на другую машину руками.
  const command = `BPROXY_NODE_SECRET=${secret} \\\n  docker compose --profile node up -d --build node`
  return <div className="issued-results">
    <SecretResult label="BPROXY_NODE_SECRET" value={secret}/>
    <SecretResult label="Команда запуска" value={command}/>
    <p className="form-note">
      Показывается один раз. Нода создаёт приватный ключ локально и отправляет CSR с этим секретом;
      после регистрации весь обмен идёт только по mTLS.
    </p>
  </div>
}

function initialCatalog(input: {
  nodeId: string; nodeName: string; boardId: string; boardName: string; boardHash: string
  maxLanes: number; allowPrivateEgress: boolean
  idleTimeout: string; grpcListen: string; httpListen: string | null; logLevel: string
}) {
  return {
    node: {
      id: input.nodeId,
      name: input.nodeName,
      state: 'enabled',
      core: {
        serverPrivateKey: randomPrivateKey(),
        idleTimeout: input.idleTimeout || 'PT1M30S',
        allowPrivateEgress: input.allowPrivateEgress,
        grpcListen: input.grpcListen || 'unix:///run/bproxy/control.sock',
        httpListen: input.httpListen,
        observabilityEnabled: true,
        logLevel: input.logLevel,
      },
    },
    boards: [{ id: input.boardId, name: input.boardName, hash: input.boardHash, state: 'enabled', maxLanes: input.maxLanes }],
    users: [],
    assignment: { boardIds: [input.boardId], users: [] },
  }
}

function randomPrivateKey() {
  const bytes = crypto.getRandomValues(new Uint8Array(32))
  let binary = ''
  bytes.forEach(value => { binary += String.fromCharCode(value) })
  return `base64:${btoa(binary)}`
}

function NodeDrawer({ node, data, api, busy, onClose, onAction, onEnroll }: { node: NodeSummary; data: DashboardData; api: ControlApi; busy: boolean; onClose: () => void; onAction: (run: () => Promise<unknown>) => Promise<void>; onEnroll: () => void }) {
  const [tab, setTab] = useState<Tab>('overview')
  const status = data.statuses[node.nodeId]
  const runtime = data.runtimes[node.nodeId]
  const catalog = data.catalog?.node.id === node.nodeId ? data.catalog : undefined
  const certs = data.certificates.filter(cert => cert.nodeId === node.nodeId)
  const drift = status && status.desiredRevision !== status.appliedRevision
  const tabs: Tab[] = ['overview', 'runtime', 'config', 'toml', 'certificate', 'logs']
  return <div className="drawer-backdrop" onMouseDown={event => { if (event.target === event.currentTarget) onClose() }}><aside className="drawer">
    <header className="drawer-header"><div><span className={`dot ${status?.connected ? drift ? 'warn' : 'ok' : 'bad'}`}/><h2>{node.name}</h2><Badge tone={node.state === 'enabled' ? 'ok' : 'neutral'}>{node.state}</Badge><p>{node.nodeId} · fence {status?.fencingToken ?? '—'} · {status?.connected ? 'stream open' : 'no stream'}</p></div><button type="button" className="icon-button" onClick={onClose}><Icon name="close"/></button></header>
    <nav className="drawer-tabs">{tabs.map(item => <button type="button" key={item} className={tab === item ? 'active' : ''} onClick={() => setTab(item)}>{item}</button>)}</nav>
    <div className="drawer-body">
      {tab === 'overview' ? <NodeOverview node={node} status={status} runtime={runtime} drift={Boolean(drift)} api={api} catalog={catalog} busy={busy} onAction={onAction} onEnroll={onEnroll}/> : null}
      {tab === 'runtime' ? <RuntimeView node={node} runtime={runtime} api={api} onAction={onAction}/> : null}
      {tab === 'config' ? <ConfigView catalog={catalog} api={api} onAction={onAction}/> : null}
      {tab === 'toml' ? <AppliedTomlView node={node} api={api}/> : null}
      {tab === 'certificate' ? <CertificateView node={node} certs={certs} api={api} onAction={onAction}/> : null}
      {tab === 'logs' ? <Logs events={data.events}/> : null}
    </div>
  </aside></div>
}

function NodeOverview({ node, status, runtime, drift, api, catalog, busy, onAction, onEnroll }: { node: NodeSummary; status?: DashboardData['statuses'][string]; runtime?: DashboardData['runtimes'][string]; drift: boolean; api: ControlApi; catalog?: Catalog; busy: boolean; onAction: (run: () => Promise<unknown>) => Promise<void>; onEnroll: () => void }) {
  const previous = catalog ? Math.max(0, catalog.version - 1) : 0
  return <><div className="detail-metrics"><DetailMetric label="Desired revision" value={status?.desiredRevision}/><DetailMetric label="Applied revision" value={status?.appliedRevision} tone={drift ? 'warn' : undefined}/><DetailMetric label="Sessions" value={runtime?.sessions.length}/><DetailMetric label="Users" value={runtime?.users.length}/><DetailMetric label="Boards" value={runtime?.boards.length}/><DetailMetric label="Last seen" value={ago(status?.lastSeen)}/></div>
    {drift ? <div className="warning-banner">Node reports an older applied revision. Desired state is waiting for the next apply.</div> : null}
    {status?.lastError ? <ErrorBanner>{status.lastError}</ErrorBanner> : null}
    <h3>Управление нодой</h3>
    <div className="action-list">
      <ManagementAction
        title="Одноразовый секрет" hint="Нода создаёт приватный ключ локально и использует секрет только для регистрации."
        cta="Выпустить" disabled={busy} onRun={onEnroll}
      />
      <ManagementAction
        title="Откатить ревизию" hint={previous > 0 ? `Вернуть ноду к применённому снапшоту ${previous}.` : 'Предыдущей ревизии пока нет.'}
        cta="Откатить" confirm disabled={busy || !catalog || previous <= 0}
        onRun={() => void onAction(() => api.rollback(node.nodeId, previous, catalog!.version))}
      />
      <ManagementAction
        title={node.state === 'enabled' ? 'Выключить ноду' : 'Включить ноду'}
        hint={node.state === 'enabled' ? 'Запись останется, новые сессии не принимаются.' : 'Нода снова начнёт принимать сессии.'}
        cta={node.state === 'enabled' ? 'Выключить' : 'Включить'} confirm={node.state === 'enabled'}
        disabled={busy || !catalog}
        onRun={() => void onAction(() => api.updateNode(node.nodeId, catalog!.version, { state: node.state === 'enabled' ? 'disabled' : 'enabled' }))}
      />
    </div>
    <h3>User sessions</h3>{runtime?.sessions.length ? <div className="compact-list">{runtime.sessions.map(session => <div key={session.bundleId}><strong>{session.userTag}</strong><code>{short(session.bundleId)}</code><span>{session.boardTag}</span><time>{ago(session.openedAt)}</time></div>)}</div> : <Empty title="No active sessions"/>}
    <h3>Board lifecycle</h3>{runtime?.boards.length ? <div className="compact-list">{runtime.boards.map(board => <div key={board.boardTag}><strong>{board.boardTag}</strong><Badge tone={board.state === 'running' ? 'ok' : board.error ? 'warn' : 'info'}>{board.state}</Badge><span>{board.error || 'healthy'}</span></div>)}</div> : <Empty title="No runtime boards"/>}
  </>
}

function ManagementAction({ title, hint, cta, confirm = false, disabled, onRun }: {
  title: string; hint: string; cta: string; confirm?: boolean; disabled?: boolean; onRun: () => void
}) {
  return <div className="action-row">
    <div><strong>{title}</strong><small>{hint}</small></div>
    {confirm
      ? <ConfirmButton className="button secondary" onConfirm={() => { if (!disabled) onRun() }}>{cta}</ConfirmButton>
      : <button className="button secondary" type="button" disabled={disabled} onClick={onRun}>{cta}</button>}
  </div>
}

function RuntimeView({ node, runtime, api, onAction }: { node: NodeSummary; runtime?: DashboardData['runtimes'][string]; api: ControlApi; onAction: (run: () => Promise<unknown>) => Promise<void> }) {
  return <><div className="runtime-summary"><div><span>Core boot</span><code>{short(runtime?.coreBootId, 26)}</code></div><div><span>Last sequence</span><code>{runtime?.lastSequence ?? '—'}</code></div><div><span>Projection revision</span><code>{runtime?.runtimeRevision ?? '—'}</code></div><div><span>Journal continuity</span>{runtime?.gapDetected ? <Badge tone="bad">sequence gap</Badge> : <Badge tone="ok">no gaps</Badge>}</div></div><button className="button secondary" type="button" onClick={() => void onAction(() => api.rebuildRuntime(node.nodeId))}>Rebuild projection</button><h3>Runtime users</h3>{runtime?.users.length ? <div className="table-scroll"><table><thead><tr><th>User</th><th>State</th><th>Sessions</th><th>Snapshot rx</th><th>Snapshot tx</th></tr></thead><tbody>{runtime.users.map(user => <tr key={user.userTag}><td>{user.userTag}</td><td><Badge tone={user.enabled ? 'ok' : 'neutral'}>{user.enabled ? 'enabled' : 'disabled'}</Badge></td><td>{user.activeSessions}</td><td className="mono">{user.rxBytesAtSnapshot}</td><td className="mono">{user.txBytesAtSnapshot}</td></tr>)}</tbody></table></div> : <Empty/>}</>
}

function ConfigView({ catalog, api, onAction }: { catalog?: Catalog; api: ControlApi; onAction: (run: () => Promise<unknown>) => Promise<void> }) {
  if (!catalog) return <Empty title="Catalog unavailable"/>
  const core = catalog.node.core
  return <form className="config-form" onSubmit={(event: FormEvent<HTMLFormElement>) => { event.preventDefault(); const form = new FormData(event.currentTarget); const body = catalogRequest(catalog, {
    idleTimeout: String(form.get('idleTimeout')), allowPrivateEgress: form.get('allowPrivateEgress') === 'on', grpcListen: String(form.get('grpcListen')), httpListen: String(form.get('httpListen')) || null, logLevel: String(form.get('logLevel')), observabilityEnabled: form.get('observabilityEnabled') === 'on',
  }); void onAction(() => api.replaceCatalog(catalog.node.id, catalog.version, body)) }}>
    <Field label="Idle timeout"><input name="idleTimeout" required defaultValue={core.idleTimeout}/></Field><Field label="gRPC listen"><input name="grpcListen" required defaultValue={core.management.grpcListen}/></Field><Field label="HTTP listen"><input name="httpListen" defaultValue={core.management.httpListen}/></Field><Field label="Log level"><select name="logLevel" defaultValue={core.observability.logLevel}><option>trace</option><option>debug</option><option>info</option><option>warn</option><option>error</option></select></Field><label className="check-field"><input type="checkbox" name="allowPrivateEgress" defaultChecked={core.allowPrivateEgress}/> Allow private egress</label><label className="check-field"><input type="checkbox" name="observabilityEnabled" defaultChecked={core.observability.enabled}/> Enable observability</label><p className="form-note">Mutation sends If-Match with catalog version “{catalog.version}”. Existing server private key remains write-only.</p><button className="button primary" type="submit">Save revision</button>
  </form>
}

/** TOML показывается уже без секретов: вырезание делает control-plane, не панель. */
function AppliedTomlView({ node, api }: { node: NodeSummary; api: ControlApi }) {
  const [config, setConfig] = useState<AppliedConfig>()
  const [missing, setMissing] = useState(false)
  useEffect(() => {
    const controller = new AbortController()
    void api.appliedConfig(node.nodeId)
      .then(result => {
        if (controller.signal.aborted) return
        if (result) setConfig(result); else setMissing(true)
      })
      .catch(() => { if (!controller.signal.aborted) setMissing(true) })
    return () => controller.abort()
  }, [api, node.nodeId])

  if (missing) return <Empty title="Конфигурация ещё не собрана" text="Нода не получила ни одной ревизии."/>
  if (!config) return <div className="auth-spinner"/>
  return <>
    <div className="runtime-summary">
      <div><span>Ревизия</span><code>{config.revision}</code></div>
      <div><span>Версия каталога</span><code>{config.catalogVersion}</code></div>
      <div><span>SHA-256</span><code>{short(config.configSha256, 20)}</code></div>
      <div><span>Собрана</span><code>{date(config.createdAt)}</code></div>
    </div>
    <div className="toml-actions">
      <button className="button secondary" type="button" onClick={() => void navigator.clipboard.writeText(config.toml)}>
        <Icon name="copy" size={14}/> Копировать
      </button>
    </div>
    <pre className="toml-view">{config.toml}</pre>
    <p className="form-note">
      Идентификаторы клиентов, их ключи и персональные лимиты здесь не отображаются — панель показывает
      только конфигурацию ноды. SHA-256 посчитан по полному TOML, который применяет сама нода.
    </p>
  </>
}

function CertificateView({ node, certs, api, onAction }: { node: NodeSummary; certs: DashboardData['certificates']; api: ControlApi; onAction: (run: () => Promise<unknown>) => Promise<void> }) {
  return certs.length ? <div className="certificate-list">{certs.map(cert => <article key={cert.serialNumber}><div><Badge tone={cert.revokedAt ? 'bad' : 'ok'}>{cert.revokedAt ? 'revoked' : 'valid'}</Badge><code>{cert.serialNumber}</code></div><dl><div><dt>Fingerprint</dt><dd>{short(cert.fingerprintSha256, 40)}</dd></div><div><dt>Issued</dt><dd>{date(cert.issuedAt)}</dd></div><div><dt>Expires</dt><dd>{date(cert.expiresAt)}</dd></div><div><dt>Last seen</dt><dd>{date(cert.lastSeenAt)}</dd></div></dl>{!cert.revokedAt ? <ConfirmButton onConfirm={() => void onAction(() => api.revokeCertificate(node.nodeId, cert.serialNumber, 'Revoked from control UI'))}>Revoke certificate</ConfirmButton> : null}</article>)}</div> : <Empty title="No certificates"/>
}

function Logs({ events }: { events: DashboardData['events'] }) { return events.length ? <div className="log-list">{events.map(event => <div key={event.eventId}><time>{new Date(event.occurredAt).toLocaleTimeString()}</time><span>{event.type}</span><code>{message(event.payload)}</code></div>)}</div> : <Empty title="No runtime events"/> }
function DetailMetric({ label, value, tone }: { label: string; value: unknown; tone?: 'warn' }) { return <div><span>{label}</span><strong className={tone}>{String(value ?? '—')}</strong></div> }

function catalogRequest(catalog: Catalog, patch: { idleTimeout: string; allowPrivateEgress: boolean; grpcListen: string; httpListen: string | null; logLevel: string; observabilityEnabled: boolean }) {
  const core = catalog.node.core
  return {
    node: { id: catalog.node.id, name: catalog.node.name, state: catalog.node.state, core: { idleTimeout: patch.idleTimeout, allowPrivateEgress: patch.allowPrivateEgress, window: core.transport.window, maxFramePayload: core.transport.maxFramePayload, streamWindow: core.transport.streamWindow, maxStreamWindow: core.transport.maxStreamWindow, ackTimeout: core.transport.ackTimeout, coalesceTarget: core.transport.coalesceTarget, streamIdleTimeout: core.transport.streamIdleTimeout, grpcListen: patch.grpcListen, httpListen: patch.httpListen, observabilityEnabled: patch.observabilityEnabled, logLevel: patch.logLevel } },
    boards: catalog.boards.map(board => ({ id: board.id, name: board.name, hash: board.hash, hubSlide: board.hubSlide, apiBase: board.apiBase, guestName: board.guestName, state: board.state, maxLanes: board.maxLanes })),
    users: catalog.users.map(user => ({ id: user.id, name: user.name, publicKey: user.publicKey, state: user.state, maxSessions: user.maxSessions, maxLanes: user.maxLanes })),
    assignment: { boardIds: catalog.assignment.boardIds, users: catalog.assignment.users },
  }
}
