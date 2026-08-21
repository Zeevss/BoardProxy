import { ChevronRight } from 'lucide-react'
import { useState } from 'react'
import { useBoards, useNodes } from '@/api/nodes'
import { useCreateUser, usePutQuota, useReplaceGrants } from '@/api/users'
import type { QuotaAction, QuotaPeriod } from '@/api/types'
import { ApiError } from '@/api/errors'
import { useLanguage } from '@/app/language'
import { Button } from '@/components/ui/button'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Modal, ModalSection } from '@/components/ui/modal'
import { Select, type SelectOption } from '@/components/ui/select'
import { StatusDot } from '@/components/ui/status'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { useToast } from '@/components/ui/toast'
import { slugify } from '@/lib/slug'
import { cn } from '@/lib/utils'

const GIGABYTE = 1_000_000_000

export function CreateUserDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useLanguage()
  const { toast } = useToast()
  const nodes = useNodes()
  const boards = useBoards()
  const create = useCreateUser()
  const grants = useReplaceGrants()
  const quota = usePutQuota()

  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [quotaOn, setQuotaOn] = useState(false)
  const [limitGb, setLimitGb] = useState('1000')
  const [period, setPeriod] = useState<QuotaPeriod>('MONTHLY')
  const [action, setAction] = useState<QuotaAction>('ALERT')
  const [advanced, setAdvanced] = useState(false)
  const [devices, setDevices] = useState('0')
  const [pages, setPages] = useState('4')
  const [error, setError] = useState<string | null>(null)

  // Ноду без досок хаб отвергает при выдаче гранта, поэтому выбрать её нельзя.
  const withBoards = new Set((boards.data?.items ?? []).map((board) => board.nodeId))
  const id = slugify(name, 'u-')
  const busy = create.isPending || grants.isPending || quota.isPending

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

  function reset() {
    setName('')
    setDescription('')
    setSelected(new Set())
    setQuotaOn(false)
    setLimitGb('1000')
    setPeriod('MONTHLY')
    setAction('ALERT')
    setAdvanced(false)
    setDevices('0')
    setPages('4')
    setError(null)
  }

  async function submit() {
    setError(null)
    if (!id) {
      setError(t.userIdHint)
      return
    }
    try {
      await create.mutateAsync({
        id,
        name: name.trim(),
        description: description.trim(),
        maxSessions: Number(devices) || 0,
        maxLanes: Math.min(32, Math.max(1, Number(pages) || 4)),
      })
      // Гранты и квота — отдельные подресурсы, поэтому идут своими запросами.
      // Если один не пройдёт, пользователь останется без него, но не потеряется.
      if (selected.size > 0) {
        await grants.mutateAsync({
          userId: id,
          grants: [...selected].map((nodeId) => ({ nodeId, boardIds: [] })),
        })
      }
      if (quotaOn && Number(limitGb) > 0) {
        await quota.mutateAsync({
          userId: id,
          draft: { period, action, enabled: true, limitBytes: Number(limitGb) * GIGABYTE },
        })
      }
      toast(`${t.newUser} · ${id}`)
      reset()
      onClose()
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : t.errorOffline)
    }
  }

  return (
    <Modal
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          reset()
          onClose()
        }
      }}
      title={t.newUserTitle}
      subtitle={t.newUserHint}
      footer={
        <>
          <Button variant="outline" disabled={busy} onClick={onClose}>
            {t.cancel}
          </Button>
          <Button variant="primary" disabled={busy || !name.trim()} onClick={() => void submit()}>
            {t.create}
          </Button>
        </>
      }
    >
      <ModalSection title={t.secBasics}>
        <Field label={t.userNameLabel} hint={id ? `id: ${id}` : t.userIdHint}>
          <Input
            autoFocus
            placeholder="Grace Hopper"
            className="bg-canvas"
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </Field>
        <Textarea
          rows={3}
          className="bg-canvas"
          placeholder={t.descriptionOpt}
          value={description}
          onChange={(event) => setDescription(event.target.value)}
        />
      </ModalSection>

      <ModalSection title={t.secAccess} hint={t.accessHint}>
        <div className="flex flex-wrap gap-2">
          {(nodes.data?.items ?? []).map((node) => {
            const on = selected.has(node.id)
            const usable = withBoards.has(node.id)
            return (
              <button
                key={node.id}
                type="button"
                aria-pressed={on}
                disabled={!usable}
                title={usable ? undefined : t.nodeWithoutBoards}
                onClick={() =>
                  setSelected((previous) => {
                    const next = new Set(previous)
                    if (next.has(node.id)) next.delete(node.id)
                    else next.add(node.id)
                    return next
                  })
                }
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
      </ModalSection>

      <ModalSection
        title={t.secTraffic}
        aside={<Switch label={t.quotaOn} checked={quotaOn} onCheckedChange={setQuotaOn} />}
      >
        {quotaOn ? (
          <div className="grid gap-3 sm:grid-cols-3">
            <Field label={t.trafficLimit}>
              <Input
                inputMode="numeric"
                placeholder="1000"
                className="bg-canvas font-mono"
                value={limitGb}
                onChange={(event) => setLimitGb(event.target.value.replace(/\D/g, ''))}
              />
            </Field>
            <Field label={t.resetPolicy}>
              <Select label={t.resetPolicy} value={period} options={periods} onChange={setPeriod} />
            </Field>
            <Field label={t.onExceed}>
              <Select label={t.onExceed} value={action} options={actions} onChange={setAction} />
            </Field>
          </div>
        ) : (
          <p className="text-xs text-muted">{t.trafficOff}</p>
        )}
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
            <Field label={t.maxDevices} hint={t.unlimitedHint}>
              <Input
                inputMode="numeric"
                className="bg-canvas font-mono"
                value={devices}
                onChange={(event) => setDevices(event.target.value.replace(/\D/g, ''))}
              />
            </Field>
            <Field label={t.maxPages} hint="1…32">
              <Input
                inputMode="numeric"
                className="bg-canvas font-mono"
                value={pages}
                onChange={(event) => setPages(event.target.value.replace(/\D/g, ''))}
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
