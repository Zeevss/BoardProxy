import { useState } from 'react'
import { Plus, X } from 'lucide-react'
import {
  useApiTokens,
  useIssueApiToken,
  useIssueServiceToken,
  useRestartService,
  useRevokeApiToken,
  useSubscriptionService,
  useUpdateSubscriptionService,
  type ServiceDraft,
} from '@/api/settings'
import type { AccessRole, ApiToken, SubscriptionApp, SubscriptionService } from '@/api/types'
import { ApiError, ConflictError } from '@/api/errors'
import { useLanguage } from '@/app/language'
import { ScreenHeader } from '@/components/ScreenHeader'
import { Button } from '@/components/ui/button'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Modal } from '@/components/ui/modal'
import { Select, type SelectOption } from '@/components/ui/select'
import { StatusDot } from '@/components/ui/status'
import { Switch } from '@/components/ui/switch'
import { useToast } from '@/components/ui/toast'
import { absoluteTime, relativeTime } from '@/lib/format'
import { SecretDialog } from './SecretDialog'
import { SubscribeDeployDialog } from './SubscribeDeployDialog'

/**
 * Ровно те платформы, которые принимает хаб (`PLATFORMS` в
 * SubscriptionServiceService). В макете был ещё `web`, и я его перенёс не
 * сверившись: сохранение с ним падало на «unknown client platform web».
 */
const PLATFORMS = ['android', 'ios', 'macos', 'windows', 'linux'] as const

export function SettingsScreen() {
  const { t } = useLanguage()
  const service = useSubscriptionService()

  return (
    <section className="mx-auto flex max-w-[920px] flex-col gap-4.5">
      <ScreenHeader title={t.settings} />

      {service.isLoading ? (
        <p className="text-sm text-dim">{t.loading}</p>
      ) : service.data ? (
        <ServiceSection key={service.data.settings.revision} service={service.data} />
      ) : null}

      <TokensSection />
    </section>
  )
}

function draftOf(service: SubscriptionService): ServiceDraft {
  const { settings } = service
  return {
    enabled: settings.enabled,
    serviceName: settings.serviceName,
    icon: settings.icon,
    publicUrl: settings.publicUrl,
    yandexEditorUrl: settings.yandexEditorUrl,
    recoveryKeyId: settings.recoveryKeyId,
    apps: settings.apps,
  }
}

/**
 * Сервис подписок.
 *
 * `key` по ревизии в родителе пересоздаёт форму, когда хаб опубликовал новую
 * конфигурацию: догонять её эффектом значило бы затирать несохранённый ввод
 * ровно в момент, когда сервис отчитался.
 */
function ServiceSection({ service }: { service: SubscriptionService }) {
  const { t, language } = useLanguage()
  const { toast } = useToast()
  const update = useUpdateSubscriptionService()
  const issue = useIssueServiceToken()
  const restart = useRestartService()

  const [draft, setDraft] = useState<ServiceDraft>(() => draftOf(service))
  const [platform, setPlatform] = useState<string>('android')
  const [appUrl, setAppUrl] = useState('')
  const [secret, setSecret] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const { status, settings } = service
  const patch = (change: Partial<ServiceDraft>) => setDraft({ ...draft, ...change })

  // Пока правки не сохранены, выдавать токен рано: сервис заберёт с хаба то,
  // что там лежит, а не то, что видно на экране.
  const dirty = JSON.stringify(draft) !== JSON.stringify(draftOf(service))

  /**
   * Включённая доставка обязана быть работоспособной, поэтому хаб требует четыре
   * поля из пяти и проверяет, что адреса разбираются. Помечаем их здесь, иначе
   * узнать об этом можно только по отказу после «Сохранить».
   *
   * Подписи человеческие, а имя поля стоит рядом моноширинным: без него
   * невозможно соотнести отказ хаба («subscription public URL must be…») с тем,
   * что именно на экране надо править.
   */
  const fields: Array<{
    key: keyof ServiceDraft
    label: string
    placeholder: string
    hint?: string
    wide?: boolean
  }> = [
    { key: 'serviceName', label: t.fieldServiceName, placeholder: 'BoardProxy', hint: t.required },
    { key: 'recoveryKeyId', label: t.fieldRecoveryKeyId, placeholder: 'rk-2026-01', hint: t.required },
    {
      key: 'publicUrl',
      label: t.fieldPublicUrl,
      placeholder: 'https://sub.example.net',
      hint: `${t.required} · ${t.urlHint}`,
      wide: true,
    },
    {
      key: 'yandexEditorUrl',
      label: t.fieldYandexUrl,
      placeholder: 'https://disk.yandex.ru/edit/…',
      hint: t.yandexHostHint,
      wide: true,
    },
    { key: 'icon', label: t.fieldIcon, placeholder: 'https://…', wide: true },
  ]

  function addApp() {
    const url = appUrl.trim()
    if (!url) return
    patch({ apps: [...draft.apps, { platform, url }] })
    setAppUrl('')
  }

  function save() {
    setError(null)
    update.mutate(
      { draft, revision: settings.revision },
      {
        onSuccess: () => toast(t.saved),
        onError: (cause) =>
          setError(
            cause instanceof ConflictError
              ? t.errorConflict
              : cause instanceof ApiError
                ? cause.message
                : t.errorOffline,
          ),
      },
    )
  }

  const seen = relativeTime(status.lastSeenAt, language)

  return (
    <>
      <div className="rounded-xl border border-line bg-canvas">
        <header className="flex flex-wrap items-center justify-between gap-4 border-b border-line-soft px-5 py-4">
          <div className="flex items-center gap-3">
            <StatusDot tone={draft.enabled ? 'ok' : 'muted'} className="size-2.5" />
            <div>
              <h2 className="text-[14.5px] font-semibold">{t.subService}</h2>
              {draft.enabled ? null : (
                <p className="mt-0.75 text-[12.5px] text-dim">{t.subServiceHint}</p>
              )}
            </div>
          </div>
          <Switch
            label={t.subService}
            checked={draft.enabled}
            onCheckedChange={(next) => patch({ enabled: next })}
          />
        </header>

        <StepHeader index={1} title={t.stepConfigure} />

        <div className="grid gap-4 px-5 py-4.5 sm:grid-cols-2">
          {fields.map((field) => (
            <Field
              key={field.key}
              label={field.label}
              // Обязательность имеет смысл только при включённой доставке:
              // выключенный сервис хаб сохраняет с любыми полями.
              hint={draft.enabled ? field.hint : undefined}
              className={field.wide ? 'sm:col-span-2' : undefined}
            >
              <Input
                placeholder={field.placeholder}
                className={field.key === 'serviceName' ? undefined : 'font-mono'}
                value={draft[field.key] as string}
                onChange={(event) => patch({ [field.key]: event.target.value })}
              />
            </Field>
          ))}
        </div>

        <div className="px-5 pb-4.5">
          <p className="mb-2 text-[13px] font-medium text-bright">{t.apps}</p>
          <div className="overflow-hidden rounded-[10px] border border-line">
            {draft.apps.map((app, index) => (
              <AppRow
                key={`${app.platform}-${app.url}`}
                app={app}
                onRemove={() => patch({ apps: draft.apps.filter((_, at) => at !== index) })}
              />
            ))}
            <div className="flex gap-2 px-3.5 py-2.5">
              <div className="w-37 shrink-0">
                <Select
                  label={t.platform}
                  value={platform}
                  options={PLATFORMS.map((value): SelectOption<string> => ({ value, label: value }))}
                  onChange={setPlatform}
                />
              </div>
              <Input
                placeholder="https://…"
                className="font-mono"
                value={appUrl}
                onChange={(event) => setAppUrl(event.target.value)}
                onKeyDown={(event) => event.key === 'Enter' && addApp()}
              />
              <Button variant="outline" className="shrink-0" onClick={addApp}>
                <Plus />
                {t.add}
              </Button>
            </div>
          </div>
        </div>

        {error ? (
          <p role="alert" className="mx-5 mb-3.5 rounded-lg border border-danger-line bg-danger-bg px-3 py-2 text-xs text-danger">
            {error}
          </p>
        ) : null}

        <footer className="flex items-center justify-between gap-3 border-t border-line-soft px-5 py-3.5">
          <p className="font-mono text-xs text-dim">
            revision {settings.revision}
            {dirty ? <span className="ml-2 text-warn">· {t.unsaved}</span> : null}
          </p>
          <Button variant="primary" disabled={update.isPending} onClick={save}>
            {t.save}
          </Button>
        </footer>

        <StepHeader index={2} title={t.stepDeploy} />
        <div className="flex flex-wrap items-center justify-between gap-3 px-5 py-4">
          {/* Токен выдаётся после сохранения: сервис заберёт конфигурацию сразу
              после запуска, а несохранённые поля до него не доедут. */}
          <p className="text-[12.5px] text-dim">{dirty ? t.saveFirst : t.subIssueHint}</p>
          <Button
            size="sm"
            variant="secondary"
            disabled={issue.isPending || dirty}
            onClick={() =>
              issue.mutate(undefined, {
                onSuccess: (issued) => setSecret(issued.secret),
                onError: () => toast(t.errorOffline, 'danger'),
              })
            }
          >
            {t.serviceToken}
          </Button>
        </div>

        <StepHeader index={3} title={t.stepLink} />
        <div className="flex flex-wrap items-center justify-between gap-4 px-5 py-4">
          <div className="flex items-center gap-2.5">
            <StatusDot
              tone={status.connected ? 'ok' : 'muted'}
              live={status.connected}
              className="size-2"
            />
            <span className="text-[13px] font-medium">
              {/* «Не отвечает» у сервиса, который никогда не запускали, читается
                  как поломка. Это разные состояния, и говорим о них по-разному. */}
              {status.connected
                ? t.serviceConnected
                : status.lastSeenAt === null
                  ? t.serviceNeverSeen
                  : t.serviceOffline}
            </span>
            {status.lastSeenAt === null ? null : (
              <span className="font-mono text-[11.5px] text-dim">
                v{status.serviceVersion ?? '—'} · rev {status.appliedRevision ?? '—'} ·{' '}
                {seen ?? t.never}
              </span>
            )}
          </div>
          <Button
            size="sm"
            variant="outline"
            // Перезапускать нечего, пока сервис ни разу не выходил на связь.
            disabled={restart.isPending || status.lastSeenAt === null}
            onClick={() =>
              restart.mutate(undefined, {
                onSuccess: () => toast(t.restartRequested),
                onError: () => toast(t.errorOffline, 'danger'),
              })
            }
          >
            {t.restart}
          </Button>
        </div>
      </div>

      <SubscribeDeployDialog token={secret} onClose={() => setSecret(null)} />
    </>
  )
}

/**
 * Полоса-разделитель с номером шага.
 *
 * Три действия — настроить, развернуть, дождаться связи — идут строго по
 * очереди, но на экране выглядели равноправными: кнопка «Сервисный токен»
 * вообще стояла выше формы, то есть визуально раньше сохранения.
 */
function StepHeader({ index, title }: { index: number; title: string }) {
  return (
    <div className="flex items-center gap-2.5 border-y border-line-soft bg-inset px-5 py-2.5">
      <span className="flex size-5 items-center justify-center rounded-full border border-line font-mono text-[11px] text-dim">
        {index}
      </span>
      <span className="text-[12.5px] font-semibold tracking-[0.03em] uppercase">{title}</span>
    </div>
  )
}

function AppRow({ app, onRemove }: { app: SubscriptionApp; onRemove: () => void }) {
  return (
    <div className="grid grid-cols-[130px_minmax(0,1fr)_44px] items-center gap-3 border-b border-line-soft px-3.5 py-2.5">
      <span className="text-[13px] font-medium">{app.platform}</span>
      <span className="truncate font-mono text-xs text-dim">{app.url}</span>
      <button
        type="button"
        aria-label={`${app.platform} ×`}
        onClick={onRemove}
        className="justify-self-end text-dim transition-colors hover:text-danger"
      >
        <X className="size-4" />
      </button>
    </div>
  )
}

const ROLES: AccessRole[] = ['VIEWER', 'OPERATOR', 'ADMIN']

function TokensSection() {
  const { t, language } = useLanguage()
  const { toast } = useToast()
  const tokens = useApiTokens()
  const revoke = useRevokeApiToken()
  const [creating, setCreating] = useState(false)

  // Отозванные токены хаб продолжает отдавать: в списке они только шумят.
  const rows = (tokens.data ?? []).filter((token) => token.revokedAt === null)

  return (
    <>
      <div className="rounded-xl border border-line bg-canvas">
        <header className="flex items-center justify-between gap-4 border-b border-line-soft px-5 py-4">
          <div>
            <h2 className="text-[14.5px] font-semibold">{t.access}</h2>
            <p className="mt-0.75 text-[12.5px] text-soft">{t.accessSub}</p>
          </div>
          <Button size="sm" variant="raised" onClick={() => setCreating(true)}>
            {t.issueToken}
          </Button>
        </header>

        {tokens.isLoading ? (
          <p className="px-5 py-4 text-[12.5px] text-muted">{t.loading}</p>
        ) : rows.length === 0 ? (
          <p className="px-5 py-4 text-[12.5px] text-muted">{t.noTokens}</p>
        ) : (
          rows.map((token) => (
            <TokenRow
              key={token.id}
              token={token}
              language={language}
              busy={revoke.isPending}
              onRevoke={() =>
                revoke.mutate(token.id, {
                  onSuccess: () => toast(`${t.revoke} · ${token.name}`),
                  onError: () => toast(t.errorOffline, 'danger'),
                })
              }
            />
          ))
        )}
      </div>

      <IssueTokenDialog open={creating} onClose={() => setCreating(false)} />
    </>
  )
}

function TokenRow({
  token,
  language,
  busy,
  onRevoke,
}: {
  token: ApiToken
  language: 'ru' | 'en'
  busy: boolean
  onRevoke: () => void
}) {
  const { t } = useLanguage()
  const [confirm, setConfirm] = useState(false)
  const expires = token.expiresAt ? absoluteTime(token.expiresAt, language) : '—'

  return (
    <div className="grid grid-cols-[minmax(0,1.2fr)_110px_minmax(0,1fr)_96px] items-center gap-3 border-b border-line-soft px-5 py-3.25 last:border-b-0 sm:grid-cols-[minmax(0,1.2fr)_110px_repeat(3,minmax(0,1fr))_96px]">
      <div className="min-w-0">
        <p className="truncate text-[13.5px] font-medium">{token.name}</p>
        <p className="truncate font-mono text-[11.5px] text-muted">{token.id}</p>
      </div>
      <span className="inline-flex h-5.5 w-fit items-center rounded-md border border-line bg-raised px-2.25 font-mono text-[11px] font-medium text-soft">
        {token.role}
      </span>
      <span className="hidden text-[12.5px] text-soft sm:block">
        {absoluteTime(token.createdAt, language)}
      </span>
      <span className="text-[12.5px] text-dim">{expires}</span>
      <span className="hidden text-[12.5px] text-soft sm:block">
        {relativeTime(token.lastUsedAt, language) ?? t.never}
      </span>
      <Button
        size="xs"
        variant={confirm ? 'danger' : 'dangerGhost'}
        disabled={busy}
        className="justify-self-end"
        onClick={() => {
          if (!confirm) {
            setConfirm(true)
            setTimeout(() => setConfirm(false), 4000)
            return
          }
          onRevoke()
        }}
      >
        {confirm ? `${t.revoke}?` : t.revoke}
      </Button>
    </div>
  )
}

function IssueTokenDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useLanguage()
  const issue = useIssueApiToken()

  const [name, setName] = useState('')
  const [role, setRole] = useState<AccessRole>('OPERATOR')
  const [days, setDays] = useState('')
  const [secret, setSecret] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  function reset() {
    setName('')
    setRole('OPERATOR')
    setDays('')
    setError(null)
  }

  return (
    <>
      <Modal
        open={open}
        onOpenChange={(next) => {
          if (!next) {
            reset()
            onClose()
          }
        }}
        title={t.issueToken}
        footer={
          <>
            <Button variant="outline" disabled={issue.isPending} onClick={onClose}>
              {t.cancel}
            </Button>
            <Button
              variant="primary"
              disabled={issue.isPending || !name.trim()}
              onClick={() => {
                setError(null)
                issue.mutate(
                  {
                    name: name.trim(),
                    role,
                    // Пустой срок — бессрочный токен: хаб принимает null.
                    ttlSeconds: days.trim() ? Number(days) * 86_400 : undefined,
                  },
                  {
                    onSuccess: (issued) => {
                      setSecret(issued.secret)
                      reset()
                      onClose()
                    },
                    onError: (cause) =>
                      setError(cause instanceof ApiError ? cause.message : t.errorOffline),
                  },
                )
              }}
            >
              {t.issueToken}
            </Button>
          </>
        }
      >
        <Field label={t.name}>
          <Input
            autoFocus
            placeholder="ci-deploy"
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </Field>
        <Field label={t.role}>
          <Select
            label={t.role}
            value={role}
            options={ROLES.map((value): SelectOption<AccessRole> => ({
              value,
              label: value,
              hint: t[`role${value}` as 'roleVIEWER' | 'roleOPERATOR' | 'roleADMIN'],
            }))}
            onChange={setRole}
          />
        </Field>
        <Field label={t.tokenTtl} hint={t.tokenTtlHint}>
          <Input
            inputMode="numeric"
            className="font-mono"
            placeholder="—"
            value={days}
            onChange={(event) => setDays(event.target.value.replace(/\D/g, ''))}
          />
        </Field>

        {error ? (
          <p role="alert" className="rounded-lg border border-danger-line bg-danger-bg px-3 py-2 text-xs text-danger">
            {error}
          </p>
        ) : null}
      </Modal>

      <SecretDialog
        title={t.tokenIssued}
        label="Authorization: Bearer"
        secret={secret}
        onClose={() => setSecret(null)}
      />
    </>
  )
}
