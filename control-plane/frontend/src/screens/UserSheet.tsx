import { useState } from 'react'
import { KeyRound } from 'lucide-react'
import { useBoards, useNodes } from '@/api/nodes'
import { useSubscriptionLink, useUserSubscriptions } from '@/api/subscriptions'
import {
  useDeleteQuota,
  useDeleteUser,
  useGrants,
  useKeylinks,
  usePutQuota,
  useQuota,
  useReplaceGrants,
  useRotateKey,
  useUpdateUser,
} from '@/api/users'
import type { Grant, QuotaAction, QuotaPeriod, User } from '@/api/types'
import { ApiError, ConflictError, RateLimitedError } from '@/api/errors'
import { useLanguage } from '@/app/language'
import { Button } from '@/components/ui/button'
import { CopyButton } from '@/components/ui/copy'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { QrCode } from '@/components/ui/qr'
import { Select, type SelectOption } from '@/components/ui/select'
import { Sheet } from '@/components/ui/sheet'
import { Badge, StatusDot } from '@/components/ui/status'
import { Textarea } from '@/components/ui/textarea'
import { useToast } from '@/components/ui/toast'
import { absoluteTime, bytes, percent, relativeTime } from '@/lib/format'
import { userStatus } from '@/lib/health'
import { cn } from '@/lib/utils'

const GIGABYTE = 1_000_000_000

/** Правки полей пользователя. Гранты и квота приходят позже — они отдельно. */
interface Draft {
  name: string
  description: string
  devices: string
  pages: string
}

function draftOf(user: User): Draft {
  return {
    name: user.name,
    description: user.description,
    devices: String(user.maxSessions),
    pages: String(user.maxLanes),
  }
}

/**
 * Карточка пользователя.
 *
 * Правки всех четырёх блоков собираются в один черновик и уходят одной кнопкой
 * внизу — так задуман дизайн. За кнопкой при этом стоят три независимые записи
 * (пользователь, размещения, квота), поэтому сохранение идёт по очереди и
 * останавливается на первой ошибке: то, что уже записано, откатить нечем, и
 * делать вид, будто не сохранилось ничего, было бы враньём.
 */
export function UserSheet({ user, onClose }: { user: User | null; onClose: () => void }) {
  const { t, language } = useLanguage()
  const { toast } = useToast()
  const explain = useExplain()

  // Пока панель уезжает, `user` уже null. Держим последнего показанного, иначе
  // на время анимации закрытия шапка схлопывается в пустую полосу.
  const [shown, setShown] = useState<User | null>(user)
  if (user && user !== shown) setShown(user)

  const nodes = useNodes()
  const boards = useBoards()
  const grants = useGrants(user?.id ?? null)
  const quota = useQuota(user?.id ?? null)

  const update = useUpdateUser()
  const replace = useReplaceGrants()
  const putQuota = usePutQuota()
  const dropQuota = useDeleteQuota()

  // Черновик пересоздаётся при смене пользователя и сбрасывается при закрытии,
  // чтобы повторное открытие той же карточки не показывало чужие правки.
  const [draftFor, setDraftFor] = useState<string | null>(null)
  const [draft, setDraft] = useState<Draft | null>(null)
  if (user && user.id !== draftFor) {
    setDraftFor(user.id)
    setDraft(draftOf(user))
  }
  if (!user && draftFor !== null) setDraftFor(null)

  /**
   * Выбор нод живёт наложением поверх серверного набора, а не копией: копию
   * пришлось бы догонять при каждой подгрузке грантов, затирая несохранённый
   * выбор ровно в момент, когда SSE инвалидировал запрос.
   */
  const [nodesOverride, setNodesOverride] = useState<Set<string> | null>(null)
  const [quotaDraft, setQuotaDraft] = useState<{
    limitGb: string
    period: QuotaPeriod
    action: QuotaAction
  } | null>(null)

  const existingQuota = quota.data?.quota ?? null
  const serverNodes = new Set((grants.data ?? []).map((grant) => grant.nodeId))
  const selectedNodes = nodesOverride ?? serverNodes

  const form = draft ?? (shown ? draftOf(shown) : null)
  const limitGb =
    quotaDraft?.limitGb ??
    (existingQuota ? String(Math.max(1, Math.round(existingQuota.limitBytes / GIGABYTE))) : '0')
  const period = quotaDraft?.period ?? existingQuota?.period ?? 'MONTHLY'
  const action = quotaDraft?.action ?? existingQuota?.action ?? 'ALERT'

  function reset() {
    setNodesOverride(null)
    setQuotaDraft(null)
  }

  async function save() {
    if (!user || !form) return
    try {
      if (
        form.name !== user.name ||
        form.description !== user.description ||
        form.devices !== String(user.maxSessions) ||
        form.pages !== String(user.maxLanes)
      ) {
        await update.mutateAsync({
          user,
          patch: {
            name: form.name.trim() || user.name,
            description: form.description.trim(),
            maxSessions: Number(form.devices) || 0,
            maxLanes: Math.min(32, Math.max(1, Number(form.pages) || 4)),
          },
        })
      }

      if (nodesOverride) {
        // У нод, которые остаются, набор досок переносится как есть: у новых он
        // пустой, и хаб читает это как «все включённые доски на момент записи».
        const byNode = new Map((grants.data ?? []).map((grant) => [grant.nodeId, grant]))
        const next: Grant[] = [...selectedNodes].map(
          (nodeId) => byNode.get(nodeId) ?? { nodeId, boardIds: [] },
        )
        await replace.mutateAsync({ userId: user.id, grants: next })
      }

      const wanted = Number(limitGb) || 0
      if (wanted <= 0 && existingQuota) {
        // «Без лимита» — это отсутствие квоты: хаб требует положительный
        // limitBytes и нулём безлимитность выразить нельзя.
        await dropQuota.mutateAsync({ userId: user.id, version: existingQuota.version })
      } else if (wanted > 0 && (quotaDraft || !existingQuota)) {
        await putQuota.mutateAsync({
          userId: user.id,
          draft: { period, action, enabled: true, limitBytes: wanted * GIGABYTE },
          version: existingQuota?.version,
        })
      }

      reset()
      toast(t.saved)
      onClose()
    } catch (cause) {
      toast(explain(cause), 'danger')
    }
  }

  const busy =
    update.isPending || replace.isPending || putQuota.isPending || dropQuota.isPending

  return (
    <Sheet
      open={user !== null}
      onOpenChange={(open) => {
        if (!open) {
          reset()
          onClose()
        }
      }}
      label={shown?.name ?? t.user}
      title={shown ? <SheetTitle user={shown} /> : null}
      actions={shown ? <SheetActions user={shown} onClose={onClose} /> : null}
      footer={
        shown ? (
          <>
            <Button variant="outline" disabled={busy} onClick={onClose}>
              {t.cancel}
            </Button>
            <Button variant="primary" disabled={busy} onClick={() => void save()}>
              {t.save}
            </Button>
          </>
        ) : null
      }
    >
      {shown && form ? (
        <div className="flex flex-col gap-3.5 px-5.5 pt-4.5 pb-6">
          <BasicsCard
            user={shown}
            draft={form}
            onChange={(change) => setDraft({ ...form, ...change })}
          />

          <AccessCard
            selected={selectedNodes}
            nodes={nodes.data?.items ?? []}
            boardNodeIds={new Set((boards.data?.items ?? []).map((board) => board.nodeId))}
            loading={grants.isLoading || nodes.isLoading}
            onToggle={(nodeId) => {
              const next = new Set(selectedNodes)
              if (next.has(nodeId)) next.delete(nodeId)
              else next.add(nodeId)
              setNodesOverride(next)
            }}
          />

          <TrafficCard
            usage={quota.data}
            limitGb={limitGb}
            period={period}
            action={action}
            onChange={(change) => setQuotaDraft({ limitGb, period, action, ...change })}
            hint={
              existingQuota && quota.data
                ? `${t.periodEnds} ${absoluteTime(quota.data.periodEnd, language) ?? ''}`
                : t.trafficOff
            }
          />

          <AdvancedCard
            draft={form}
            onChange={(change) => setDraft({ ...form, ...change })}
          />

          <KeysCard user={shown} />
        </div>
      ) : null}
    </Sheet>
  )
}

function SheetTitle({ user }: { user: User }) {
  const { t, language } = useLanguage()
  const status = userStatus(user, t)
  const activity = user.activated
    ? `${t.activityPrefix} ${relativeTime(user.lastSeenAt, language) ?? t.never}`
    : t.uPending.toLowerCase()

  return (
    <div className="flex gap-3">
      <StatusDot tone={status.tone} live={status.live} className="mt-1.5 size-2.5" />
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2.5">
          <h2 className="truncate text-lg font-semibold tracking-tight">{user.name}</h2>
          <Badge tone={status.tone}>{status.label}</Badge>
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-2">
          <span className="font-mono text-xs text-dim">{user.id}</span>
          <span className="text-xs text-muted">· {activity}</span>
        </div>
      </div>
    </div>
  )
}

/** Приводит ошибку мутации к человеческому объяснению, а не к коду ответа. */
function useExplain() {
  const { t } = useLanguage()
  return (error: unknown): string => {
    if (error instanceof ConflictError) return t.errorConflict
    if (error instanceof RateLimitedError) return t.errorRateLimited
    if (error instanceof ApiError) return error.message
    return t.errorOffline
  }
}

function SheetActions({ user, onClose }: { user: User; onClose: () => void }) {
  const { t } = useLanguage()
  const { toast } = useToast()
  const explain = useExplain()
  const update = useUpdateUser()
  const rotate = useRotateKey()
  const remove = useDeleteUser()
  const [confirmRotate, setConfirmRotate] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)

  const busy = update.isPending || rotate.isPending || remove.isPending
  const on = user.state === 'enabled'

  return (
    <>
      <Button
        size="lg"
        variant="primary"
        disabled={busy}
        onClick={() =>
          update.mutate(
            { user, patch: { state: on ? 'disabled' : 'enabled' } },
            { onSuccess: () => toast(t.saved), onError: (error) => toast(explain(error), 'danger') },
          )
        }
      >
        <span
          aria-hidden
          className={cn('size-1.75 rounded-full', on ? 'bg-danger-solid' : 'bg-ok')}
        />
        {on ? t.disable : t.enable}
      </Button>

      <Button
        size="lg"
        variant={confirmRotate ? 'danger' : 'secondary'}
        // Ключ выпущен не хабом — приватной части у него нет, ротировать нечего.
        disabled={busy || !user.hubIssuedKey}
        title={user.hubIssuedKey ? undefined : t.noHubKey}
        onClick={() => {
          if (!confirmRotate) {
            setConfirmRotate(true)
            setTimeout(() => setConfirmRotate(false), 4000)
            return
          }
          rotate.mutate(user, {
            onSuccess: () => toast(t.rotateKey),
            onError: (error) => toast(explain(error), 'danger'),
          })
        }}
      >
        <KeyRound />
        {confirmRotate ? `${t.rotate}?` : t.rotateKey}
      </Button>

      <Button
        size="lg"
        variant={confirmDelete ? 'danger' : 'dangerGhost'}
        className="ml-auto"
        disabled={busy}
        onClick={() => {
          if (!confirmDelete) {
            setConfirmDelete(true)
            setTimeout(() => setConfirmDelete(false), 4000)
            return
          }
          remove.mutate(user, {
            onSuccess: () => {
              toast(`${t.delete} · ${user.id}`)
              onClose()
            },
            onError: (error) => toast(explain(error), 'danger'),
          })
        }}
      >
        {confirmDelete ? `${t.delete}?` : t.delete}
      </Button>

      {confirmRotate ? (
        <p className="w-full text-xs text-warn">{t.rotateWarning}</p>
      ) : confirmDelete ? (
        <p className="w-full text-xs text-danger">{t.deleteUserWarning}</p>
      ) : null}
    </>
  )
}

function Card({
  title,
  meta,
  hint,
  children,
}: {
  title: string
  /** Правый край заголовка: число, а не пояснение. */
  meta?: React.ReactNode
  hint?: string
  children: React.ReactNode
}) {
  return (
    <section className="flex flex-col gap-3 rounded-xl border border-line bg-canvas p-4">
      <div className="flex items-center justify-between gap-3">
        <div>
          <h3 className="text-[13px] font-semibold">{title}</h3>
          {hint ? <p className="mt-1 text-[11.5px] text-dim">{hint}</p> : null}
        </div>
        {meta}
      </div>
      {children}
    </section>
  )
}

function BasicsCard({
  user,
  draft,
  onChange,
}: {
  user: User
  draft: Draft
  onChange: (change: Partial<Draft>) => void
}) {
  const { t } = useLanguage()

  return (
    <Card title={t.secBasics}>
      <Field label={t.userNameLabel}>
        <Input value={draft.name} onChange={(event) => onChange({ name: event.target.value })} />
      </Field>

      <Field label={t.description}>
        <Textarea
          rows={3}
          placeholder={t.descriptionOpt}
          value={draft.description}
          onChange={(event) => onChange({ description: event.target.value })}
        />
      </Field>

      <SubscriptionBlock userId={user.id} />
    </Card>
  )
}

/**
 * Ссылка подписки пользователя.
 *
 * Блок появляется, только когда сервис подписок включён и ссылка собрана: при
 * выключенном сервисе хаб отдаёт `url: null`, и показывать пустую рамку не за чем
 * — доступ в этом случае выдаётся прямыми keylink'ами ниже.
 */
function SubscriptionBlock({ userId }: { userId: string }) {
  const { t } = useLanguage()
  const subscriptions = useUserSubscriptions(userId)
  const first = subscriptions.data?.items[0] ?? null
  const link = useSubscriptionLink(first?.id ?? null)
  const url = link.data?.url ?? null

  if (!url) return null

  return (
    <div className="flex flex-wrap items-center gap-3.5 rounded-xl border border-line bg-raised p-3.5">
      <div className="shrink-0 rounded-[10px] bg-fg p-2">
        <QrCode value={url} size={92} />
      </div>
      <div className="flex min-w-[200px] flex-1 flex-col gap-2.5">
        <div className="flex items-center gap-2">
          <StatusDot tone="ok" className="size-1.75" />
          <span className="text-[12.5px] font-semibold">{t.subLink}</span>
        </div>
        <p className="rounded-lg border border-line bg-surface px-3 py-2.5 font-mono text-[11.5px] break-all text-bright">
          {url}
        </p>
        <div className="flex flex-wrap gap-2">
          <CopyButton size="xs" variant="secondary" value={url} label={t.subLink}>
            {t.copy}
          </CopyButton>
        </div>
      </div>
    </div>
  )
}

function AccessCard({
  selected,
  nodes,
  boardNodeIds,
  loading,
  onToggle,
}: {
  selected: Set<string>
  nodes: Array<{ id: string; name: string }>
  boardNodeIds: Set<string>
  loading: boolean
  onToggle: (nodeId: string) => void
}) {
  const { t } = useLanguage()

  return (
    <Card title={t.secAccess} hint={t.accessHint}>
      {loading ? (
        <p className="text-xs text-dim">{t.loading}</p>
      ) : (
        <div className="flex flex-wrap gap-2">
          {nodes.map((node) => {
            const on = selected.has(node.id)
            // Ноду без досок хаб отвергает целиком (`node X has no boards`),
            // поэтому выбрать её нельзя: одна такая уронила бы всё сохранение.
            const usable = boardNodeIds.has(node.id) || on
            return (
              <button
                key={node.id}
                type="button"
                aria-pressed={on}
                disabled={!usable}
                title={usable ? undefined : t.nodeWithoutBoards}
                onClick={() => onToggle(node.id)}
                className={cn(
                  'flex h-8 items-center gap-2 rounded-lg border px-3 text-[12.5px] transition-colors',
                  on
                    ? 'border-ok-line bg-ok-bg text-fg'
                    : usable
                      ? 'border-line text-soft hover:border-line-strong'
                      : 'cursor-not-allowed border-line text-muted opacity-60',
                )}
              >
                <StatusDot tone={on ? 'ok' : 'muted'} className="size-1.75" />
                {node.name}
                {usable ? null : <span className="text-[10px]">· {t.nodeWithoutBoards}</span>}
              </button>
            )
          })}
        </div>
      )}

      {selected.size === 0 && !loading ? <p className="text-xs text-warn">{t.noAccess}</p> : null}
    </Card>
  )
}

function TrafficCard({
  usage,
  limitGb,
  period,
  action,
  hint,
  onChange,
}: {
  usage: { usedBytes: number; exceeded: boolean } | null | undefined
  limitGb: string
  period: QuotaPeriod
  action: QuotaAction
  hint: string
  onChange: (change: Partial<{ limitGb: string; period: QuotaPeriod; action: QuotaAction }>) => void
}) {
  const { t } = useLanguage()

  const used = usage?.usedBytes ?? 0
  const limitBytes = (Number(limitGb) || 0) * GIGABYTE
  const share = percent(used, limitBytes)
  const periods: Array<SelectOption<QuotaPeriod>> = [
    { value: 'MONTHLY', label: 'MONTHLY', hint: t.periodMonthly },
    { value: 'WEEKLY', label: 'WEEKLY', hint: t.periodWeekly },
    { value: 'DAILY', label: 'DAILY', hint: t.periodDaily },
    { value: 'NONE', label: 'NONE', hint: t.periodNone },
  ]
  const actions: Array<SelectOption<QuotaAction>> = [
    { value: 'ALERT', label: 'ALERT', hint: t.actionAlert },
    { value: 'RESET', label: 'RESET', hint: t.actionReset },
    { value: 'DISABLE', label: 'DISABLE', hint: t.actionDisable },
  ]

  return (
    <Card
      title={t.secTraffic}
      meta={
        <span className="font-mono text-[11.5px] text-dim">
          {limitBytes > 0 ? `${bytes(used)} / ${bytes(limitBytes)}` : `${bytes(used)} · ${t.trafficOff}`}
        </span>
      }
    >
      <div className="h-1.5 overflow-hidden rounded-full bg-line">
        <div
          className={cn(
            'h-full rounded-full transition-[width] duration-300',
            limitBytes === 0
              ? 'bg-line-strong'
              : usage?.exceeded || share >= 90
                ? 'bg-danger'
                : share >= 70
                  ? 'bg-warn'
                  : 'bg-fg',
          )}
          style={{ width: `${limitBytes === 0 ? 0 : Math.max(2, share)}%` }}
        />
      </div>

      <div className="grid gap-3 sm:grid-cols-3">
        <Field label={t.trafficLimit}>
          <Input
            inputMode="numeric"
            className="font-mono"
            value={limitGb}
            onChange={(event) => onChange({ limitGb: event.target.value.replace(/\D/g, '') })}
          />
        </Field>
        <Field label={t.resetPolicy}>
          <Select
            label={t.resetPolicy}
            value={period}
            options={periods}
            onChange={(next) => onChange({ period: next })}
          />
        </Field>
        <Field label={t.onExceed}>
          <Select
            label={t.onExceed}
            value={action}
            options={actions}
            onChange={(next) => onChange({ action: next })}
          />
        </Field>
      </div>

      <p className="text-[11.5px] text-muted">
        {hint} · {t.quotaZeroHint}
      </p>
    </Card>
  )
}

function AdvancedCard({
  draft,
  onChange,
}: {
  draft: Draft
  onChange: (change: Partial<Draft>) => void
}) {
  const { t } = useLanguage()

  return (
    <Card title={t.secAdvanced}>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label={t.maxDevices} hint={t.unlimitedHint}>
          <Input
            inputMode="numeric"
            className="font-mono"
            value={draft.devices}
            onChange={(event) => onChange({ devices: event.target.value.replace(/\D/g, '') })}
          />
        </Field>
        <Field label={t.maxPages} hint="1…32">
          <Input
            inputMode="numeric"
            className="font-mono"
            value={draft.pages}
            onChange={(event) => onChange({ pages: event.target.value.replace(/\D/g, '') })}
          />
        </Field>
      </div>
    </Card>
  )
}

function KeysCard({ user }: { user: User }) {
  const { t } = useLanguage()
  const keylinks = useKeylinks(user.id, user.hubIssuedKey)
  const [qr, setQr] = useState<string | null>(null)

  const links = (keylinks.data ?? []).filter((item) => item.keylink)

  return (
    <section className="overflow-hidden rounded-xl border border-line bg-canvas">
      <div className="flex items-center justify-between gap-3 border-b border-line-soft px-4 py-3.5">
        <h3 className="text-[13px] font-semibold">{t.secKeys}</h3>
        <p className="text-[11.5px] text-dim">{t.keylinksHint}</p>
      </div>

      {!user.hubIssuedKey ? (
        <p className="px-4 py-4.5 text-[12.5px] text-muted">{t.noHubKey}</p>
      ) : keylinks.isLoading ? (
        <p className="px-4 py-4.5 text-[12.5px] text-muted">{t.loading}</p>
      ) : links.length === 0 ? (
        <p className="px-4 py-4.5 text-[12.5px] text-muted">{t.noKeylinks}</p>
      ) : (
        links.map((item) => (
          <div key={item.nodeId} className="border-b border-line-soft px-4 py-3.5 last:border-b-0">
            <div className="flex items-center gap-3">
              <div className="min-w-0 flex-1">
                <p className="truncate text-[12.5px] font-medium">{item.nodeName}</p>
                <p className="mt-0.5 truncate font-mono text-[11.5px] text-dim">{item.keylink}</p>
              </div>
              <CopyButton size="xs" variant="raised" value={item.keylink!} label={item.nodeName}>
                {t.copy}
              </CopyButton>
              <Button
                size="xs"
                variant="raised"
                aria-expanded={qr === item.nodeId}
                onClick={() => setQr(qr === item.nodeId ? null : item.nodeId)}
              >
                {t.showQr}
              </Button>
            </div>
            {qr === item.nodeId ? (
              <div className="mt-3 flex justify-center rounded-lg bg-fg p-3">
                <QrCode value={item.keylink!} size={180} />
              </div>
            ) : null}
          </div>
        ))
      )}
    </section>
  )
}
