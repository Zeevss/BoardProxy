import { useState } from 'react'
import { Check } from 'lucide-react'
import { useAgents, useCreateBoard, useCreateNode, useIssueEnrollmentToken } from '@/api/nodes'
import type { Agent } from '@/api/types'
import { ApiError } from '@/api/errors'
import { useLanguage } from '@/app/language'
import { Button } from '@/components/ui/button'
import { CopyButton } from '@/components/ui/copy'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Modal } from '@/components/ui/modal'
import { useToast } from '@/components/ui/toast'
import { boardHash, boardId } from '@/lib/board-link'
import { GRPC_PORT, hubAddressProblem, suggestHubAddress } from '@/lib/hub-address'
import { slugify } from '@/lib/slug'
import { cn } from '@/lib/utils'

type Step = 1 | 2 | 3

/**
 * Мастер добавления ноды.
 *
 * Расхождение с дизайном: там нода создаётся последней кнопкой, а секрет виден
 * с первого шага. У хаба секрет выдаётся **существующей** ноде
 * (`POST /nodes/{id}/enrollment-tokens`), поэтому запись появляется в конце
 * первого шага. Отсюда и последняя кнопка называется «Готово», а не
 * «Добавить ноду»: добавлять на этом месте уже нечего.
 *
 * Закрытие после первого шага оставляет созданную ноду во флоте — она честно
 * покажется как «ни разу не выходила на связь» и удаляется из своей карточки.
 */
export function NodeWizard({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useLanguage()
  const { toast } = useToast()

  const createNode = useCreateNode()
  const issue = useIssueEnrollmentToken()
  const createBoard = useCreateBoard()

  const [step, setStep] = useState<Step>(1)
  const [name, setName] = useState('')
  const [hubAddress, setHubAddress] = useState(suggestHubAddress)
  const [nodeId, setNodeId] = useState<string | null>(null)
  const [secret, setSecret] = useState<string | null>(null)
  const [boardName, setBoardName] = useState('')
  const [boardUrl, setBoardUrl] = useState('')
  const [boardDone, setBoardDone] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const id = slugify(name)
  const hash = boardHash(boardUrl)
  const busy = createNode.isPending || issue.isPending || createBoard.isPending

  function reset() {
    setStep(1)
    setName('')
    setHubAddress(suggestHubAddress())
    setNodeId(null)
    setSecret(null)
    setBoardName('')
    setBoardUrl('')
    setBoardDone(false)
    setError(null)
  }

  function close() {
    reset()
    onClose()
  }

  async function issueSecret() {
    setError(null)
    if (!id) {
      setError(t.userIdHint)
      return
    }
    // Адрес проверяем до создания ноды: иначе неверное значение оставило бы
    // во флоте запись, к которой уже не выдать секрет с тем же id.
    const problem = hubAddressProblem(hubAddress)
    if (problem) {
      setError(problem === 'scheme' ? t.hubUrlScheme : problem === 'port' ? t.hubUrlPort : t.hubUrlHint)
      return
    }
    try {
      await createNode.mutateAsync({ id, name: name.trim() })
      const issued = await issue.mutateAsync({ nodeId: id, hubUrl: hubAddress.trim() })
      setNodeId(id)
      setSecret(issued.nodeSecret)
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : t.errorOffline)
    }
  }

  async function attachBoard() {
    setError(null)
    if (!nodeId || !hash) {
      setError(t.boardLinkHint)
      return
    }
    try {
      const name = boardName.trim() || hash
      await createBoard.mutateAsync({ id: boardId(name, hash), nodeId, name, hash })
      setBoardDone(true)
      setStep(3)
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : t.errorOffline)
    }
  }

  function next() {
    if (step === 1) {
      if (!secret) void issueSecret()
      else setStep(2)
      return
    }
    if (step === 2) {
      if (!boardDone) void attachBoard()
      else setStep(3)
      return
    }
    toast(`${t.newNode} · ${nodeId}`)
    close()
  }

  const nextLabel = step === 3 ? t.done : step === 1 && !secret ? t.issueSecret : t.next

  return (
    <Modal
      open={open}
      onOpenChange={(value) => !value && close()}
      title={t.newNode}
      subtitle={undefined}
      className="max-w-[580px]"
      footer={
        <div className="flex w-full items-center justify-between gap-2">
          <Button
            variant="raised"
            disabled={step === 1 || busy}
            className={step === 1 ? 'invisible' : undefined}
            onClick={() => setStep((step - 1) as Step)}
          >
            {t.back}
          </Button>
          <div className="flex gap-2">
            <Button variant="outline" disabled={busy} onClick={close}>
              {t.cancel}
            </Button>
            <Button variant="primary" disabled={busy} onClick={next}>
              {nextLabel}
            </Button>
          </div>
        </div>
      }
    >
      <Steps step={step} />

      {step === 1 ? (
        <StepKey
          name={name}
          id={id}
          hubAddress={hubAddress}
          secret={secret}
          locked={nodeId !== null}
          onName={setName}
          onHubAddress={setHubAddress}
        />
      ) : null}

      {step === 2 ? (
        <StepBoard
          name={boardName}
          url={boardUrl}
          hash={hash}
          done={boardDone}
          onName={setBoardName}
          onUrl={setBoardUrl}
        />
      ) : null}

      {step === 3 && nodeId ? <StepCheck nodeId={nodeId} /> : null}

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

function Steps({ step }: { step: Step }) {
  const { t } = useLanguage()
  const labels = [t.wizStep1, t.wizStep2, t.wizStep3]

  return (
    <div className="flex flex-col gap-3.5">
      <p className="text-right font-mono text-[11.5px] text-muted">
        {t.step} {step} {t.of} 3
      </p>
      <div className="flex gap-1.5">
        {labels.map((label, index) => (
          <div key={label} className="flex flex-1 flex-col gap-1.75">
            <div
              className={cn(
                'h-[3px] rounded-full transition-colors duration-300',
                step > index ? 'bg-fg' : 'bg-line',
              )}
            />
            <span
              className={cn(
                'text-[11.5px] font-medium transition-colors duration-300',
                step === index + 1 ? 'text-fg' : step > index ? 'text-soft' : 'text-muted',
              )}
            >
              {label}
            </span>
          </div>
        ))}
      </div>
    </div>
  )
}

function StepKey({
  name,
  id,
  hubAddress,
  secret,
  locked,
  onName,
  onHubAddress,
}: {
  name: string
  id: string
  hubAddress: string
  secret: string | null
  locked: boolean
  onName: (next: string) => void
  onHubAddress: (next: string) => void
}) {
  const { t } = useLanguage()
  const compose = secret
    ? `BPROXY_NODE_SECRET=${secret} \\\n  docker compose --profile node up -d --build node`
    : ''

  return (
    <div className="flex flex-col gap-4">
      <Field label={t.internalName} hint={id ? `${t.nodeIdIs} ${id}` : t.nodeIdDerived}>
        <Input
          autoFocus
          // После выпуска секрета имя правится в карточке: смена id здесь
          // означала бы вторую ноду, а секрет уже выдан первой.
          disabled={locked}
          placeholder="Frankfurt · AX41"
          className="bg-raised"
          value={name}
          onChange={(event) => onName(event.target.value)}
        />
      </Field>

      <Field label={t.hubUrl} hint={t.hubUrlHint}>
        <Input
          disabled={locked}
          placeholder={`hub:${GRPC_PORT}`}
          className="bg-raised font-mono"
          value={hubAddress}
          onChange={(event) => onHubAddress(event.target.value)}
        />
      </Field>

      {secret ? (
        <>
          <div className="flex flex-col gap-1.5">
            <div className="flex items-center justify-between gap-3">
              <span className="text-[13px] font-medium text-bright">BPROXY_NODE_SECRET</span>
              <span className="text-[11.5px] text-warn">{t.onceOnly}</span>
            </div>
            <div className="flex gap-2">
              <p className="min-w-0 flex-1 rounded-lg border border-line bg-raised px-3 py-2.5 font-mono text-xs break-all text-bright">
                {secret}
              </p>
              <CopyButton
                variant="raised"
                className="shrink-0"
                value={secret}
                label="BPROXY_NODE_SECRET"
              >
                {t.copy}
              </CopyButton>
            </div>
          </div>

          <div className="flex flex-col gap-1.5">
            <span className="text-[13px] font-medium text-bright">{t.composeLabel}</span>
            <div className="overflow-hidden rounded-[10px] border border-line bg-sheet">
              <pre className="overflow-x-auto px-3.5 py-3 font-mono text-[11.5px] leading-relaxed text-soft">
                {compose}
              </pre>
              <div className="flex justify-end border-t border-line-soft px-3 py-2">
                <CopyButton size="xs" variant="raised" value={compose} label="docker compose">
                  {t.copyCompose}
                </CopyButton>
              </div>
            </div>
          </div>
        </>
      ) : (
        <p className="text-[12.5px] leading-relaxed text-dim">{t.secretStepHint}</p>
      )}
    </div>
  )
}

function StepBoard({
  name,
  url,
  hash,
  done,
  onName,
  onUrl,
}: {
  name: string
  url: string
  hash: string
  done: boolean
  onName: (next: string) => void
  onUrl: (next: string) => void
}) {
  const { t } = useLanguage()

  return (
    <div className="flex flex-col gap-3">
      <p className="text-[13px] leading-relaxed text-soft">{t.boardStepHint}</p>

      <div className="flex flex-col gap-3 rounded-xl border border-line bg-raised p-4">
        <Field label={t.boardNameOptional}>
          <Input
            disabled={done}
            placeholder="Main board"
            className="bg-canvas"
            value={name}
            onChange={(event) => onName(event.target.value)}
          />
        </Field>
        <Field label={t.boardLink} hint={hash ? `hash · ${hash}` : t.boardLinkHint}>
          <Input
            autoFocus
            disabled={done}
            placeholder="https://…/?hash=…"
            className="bg-canvas font-mono"
            value={url}
            onChange={(event) => onUrl(event.target.value)}
          />
        </Field>
      </div>
    </div>
  )
}

/**
 * Проверка связи.
 *
 * Дизайн показывал четыре шага рукопожатия, но панель их по отдельности не
 * видит: она читает только отчёт агента. Поэтому шкала выведена из
 * наблюдаемого — из тех же полей, по которым считается здоровье ноды.
 *
 * Порядок причинный, а не из дизайна: конфигурация доставляется сразу при
 * подключении, а первый снимок ядра приходит следом. С обратным порядком
 * шкала загоралась снизу вверх и выглядела сломанной.
 */
function StepCheck({ nodeId }: { nodeId: string }) {
  const { t } = useLanguage()
  const agents = useAgents()
  const agent: Agent | undefined = (agents.data ?? []).find((item) => item.id === nodeId)

  const checks = [
    { label: t.check1, done: agent !== undefined && agent.lastReportAt !== null },
    { label: t.check2, done: agent?.online === true },
    {
      label: t.check4,
      done: agent !== undefined && agent.appliedRevision === agent.desiredRevision,
      meta: agent ? `${agent.appliedRevision} / ${agent.desiredRevision}` : '',
    },
    { label: t.check3, done: agent?.coreReporting === true },
  ]
  const ready = checks.every((check) => check.done)
  const current = checks.findIndex((check) => !check.done)

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-3">
        {ready ? (
          <span className="flex size-5 items-center justify-center rounded-full border border-ok-line bg-ok-bg text-ok-fg">
            <Check className="size-3" />
          </span>
        ) : (
          <span className="size-5 animate-spin rounded-full border-2 border-line border-t-fg" />
        )}
        <div>
          <p className={cn('text-[13.5px] font-semibold', ready ? 'text-ok-fg' : 'text-fg')}>
            {ready ? t.readyTitle : t.checkingTitle}
          </p>
          <p className="mt-0.5 text-xs text-dim">{ready ? t.readySub : t.checkingSub}</p>
        </div>
      </div>

      <div className="overflow-hidden rounded-xl border border-line bg-raised">
        {checks.map((check, index) => (
          <div
            key={check.label}
            className="flex items-center gap-3 border-b border-line-soft px-3.5 py-3 last:border-b-0"
          >
            <span
              className={cn(
                'flex size-4.5 shrink-0 items-center justify-center rounded-full border text-[10px]',
                check.done
                  ? 'border-ok-line bg-ok-bg text-ok-fg'
                  : index === current
                    ? 'border-muted text-soft'
                    : 'border-line text-muted',
              )}
            >
              {check.done ? <Check className="size-2.5" /> : index === current ? '·' : ''}
            </span>
            <span
              className={cn(
                'text-[13px]',
                check.done ? 'text-fg' : index === current ? 'text-bright' : 'text-muted',
              )}
            >
              {check.label}
            </span>
            <span className="ml-auto font-mono text-[11px] text-muted">
              {check.done ? check.meta ?? 'ok' : index === current ? '…' : ''}
            </span>
          </div>
        ))}
      </div>
    </div>
  )
}
