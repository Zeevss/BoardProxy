import { useState } from 'react'
import { Check } from 'lucide-react'
import { useAgents, useCreateBoard, useCreateNode, useIssueEnrollmentToken } from '@/api/nodes'
import type { Agent } from '@/api/types'
import { ApiError } from '@/api/errors'
import { useLanguage } from '@/app/language'
import { Button } from '@/components/ui/button'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Modal } from '@/components/ui/modal'
import { useToast } from '@/components/ui/toast'
import { boardHash, boardId } from '@/lib/board-link'
import { GRPC_PORT, hubAddressProblem, suggestHubAddress } from '@/lib/hub-address'
import { slugify } from '@/lib/slug'
import { NodeDeploy } from './NodeDeploy'
import { cn } from '@/lib/utils'

type Step = 1 | 2 | 3

/**
 * Мастер добавления ноды.
 *
 * Первые два шага ничего не записывают: имя, адрес хаба и доска живут в
 * черновике. Хаб трогается один раз — на переходе к проверке связи, где
 * подряд создаются нода, секрет и, если её заполнили, доска. Так брошенный на
 * полпути мастер не оставляет во флоте запись, которую никто не заводил.
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
    setError(null)
  }

  function close() {
    reset()
    onClose()
  }

  /** Проверки первого шага — синтаксические: хаб на этом этапе не трогается. */
  function checkKeyStep(): string | null {
    if (!id) return t.userIdHint
    const problem = hubAddressProblem(hubAddress)
    if (problem === 'scheme') return t.hubUrlScheme
    if (problem === 'port') return t.hubUrlPort
    if (problem) return t.hubUrlHint
    return null
  }

  /**
   * Единственная запись за весь мастер.
   *
   * Доска создаётся после секрета и её отказ не отменяет ноду: нода к этому
   * моменту уже существует, а откатить создание нечем. Доску тогда просто
   * добавят из карточки ноды, о чём и говорит показанная ошибка.
   */
  async function provision() {
    setError(null)
    try {
      await createNode.mutateAsync({ id, name: name.trim() })
      const issued = await issue.mutateAsync({ nodeId: id, hubUrl: hubAddress.trim() })
      setNodeId(id)
      setSecret(issued.nodeSecret)
      if (hash) {
        const title = boardName.trim() || hash
        await createBoard.mutateAsync({ id: boardId(title, hash), nodeId: id, name: title, hash })
      }
      setStep(3)
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : t.errorOffline)
    }
  }

  function next() {
    setError(null)
    if (step === 1) {
      const problem = checkKeyStep()
      if (problem) setError(problem)
      else setStep(2)
      return
    }
    if (step === 2) {
      // Ссылку либо не ввели вовсе, либо ввели так, что хэш не вытащить.
      if (boardUrl.trim() && !hash) {
        setError(t.boardLinkHint)
        return
      }
      void provision()
      return
    }
    toast(`${t.newNode} · ${nodeId}`)
    close()
  }

  const nextLabel = step === 3 ? t.done : step === 2 ? t.create : t.next

  return (
    <Modal
      open={open}
      onOpenChange={(value) => !value && close()}
      title={t.newNode}
      className="max-w-[580px]"
      footer={
        <div className="flex w-full items-center justify-between gap-2">
          <Button
            variant="raised"
            // Назад с третьего шага некуда: нода уже создана, а поля первых
            // двух шагов после записи ничего не меняют.
            disabled={step !== 2 || busy}
            className={step === 2 ? undefined : 'invisible'}
            onClick={() => setStep(1)}
          >
            {t.back}
          </Button>
          <div className="flex gap-2">
            {step === 2 ? (
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => {
                  setBoardName('')
                  setBoardUrl('')
                  void provision()
                }}
              >
                {t.skip}
              </Button>
            ) : (
              <Button variant="outline" disabled={busy} onClick={close}>
                {t.cancel}
              </Button>
            )}
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
          onName={setName}
          onHubAddress={setHubAddress}
        />
      ) : null}

      {step === 2 ? (
        <StepBoard
          name={boardName}
          url={boardUrl}
          hash={hash}
          onName={setBoardName}
          onUrl={setBoardUrl}
        />
      ) : null}

      {step === 3 && nodeId && secret ? <StepCheck nodeId={nodeId} secret={secret} /> : null}

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
  onName,
  onHubAddress,
}: {
  name: string
  id: string
  hubAddress: string
  onName: (next: string) => void
  onHubAddress: (next: string) => void
}) {
  const { t } = useLanguage()

  return (
    <div className="flex flex-col gap-4">
      <Field label={t.internalName} hint={id ? `${t.nodeIdIs} ${id}` : undefined}>
        <Input
          autoFocus
          placeholder="Frankfurt · AX41"
          className="bg-raised"
          value={name}
          onChange={(event) => onName(event.target.value)}
        />
      </Field>

      <Field label={t.hubUrl} hint={t.hubUrlHint}>
        <Input
          placeholder={`hub:${GRPC_PORT}`}
          className="bg-raised font-mono"
          value={hubAddress}
          onChange={(event) => onHubAddress(event.target.value)}
        />
      </Field>
    </div>
  )
}

function StepBoard({
  name,
  url,
  hash,
  onName,
  onUrl,
}: {
  name: string
  url: string
  hash: string
  onName: (next: string) => void
  onUrl: (next: string) => void
}) {
  const { t } = useLanguage()

  return (
    <div className="flex flex-col gap-3 rounded-xl border border-line bg-raised p-4">
      <Field label={t.boardNameOptional}>
        <Input
          placeholder="Main board"
          className="bg-canvas"
          value={name}
          onChange={(event) => onName(event.target.value)}
        />
      </Field>
      <Field label={t.boardLink} hint={hash ? `hash · ${hash}` : t.boardLinkHint}>
        <Input
          autoFocus
          placeholder="https://…/?hash=…"
          className="bg-canvas font-mono"
          value={url}
          onChange={(event) => onUrl(event.target.value)}
        />
      </Field>
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
 * Порядок причинный: сначала нода дозванивается, потом получает конфигурацию,
 * потом отчитывается ядро. Загорается шкала строго слева направо — признак
 * считается достигнутым, только если достигнуты все предыдущие. Иначе
 * «конфигурация применена» успевала загореться раньше «сертификат выдан», и
 * галочки прыгали по списку вразнобой.
 */
function StepCheck({ nodeId, secret }: { nodeId: string; secret: string }) {
  const { t } = useLanguage()
  // Частый опрос нужен, только пока чего-то ждут: дойдя до конца, шкала уже
  // ничего не покажет, а модалка может провисеть открытой сколько угодно.
  const [settled, setSettled] = useState(false)
  const agents = useAgents(!settled)
  const agent: Agent | undefined = (agents.data ?? []).find((item) => item.id === nodeId)

  const observed = [
    { label: t.check1, done: agent !== undefined && agent.lastReportAt !== null, meta: '' },
    { label: t.check2, done: agent?.online === true, meta: '' },
    {
      label: t.check4,
      done: agent !== undefined && agent.appliedRevision === agent.desiredRevision,
      meta: agent ? `${agent.appliedRevision} / ${agent.desiredRevision}` : '',
    },
    { label: t.check3, done: agent?.coreReporting === true, meta: '' },
  ]
  // Число подряд идущих достигнутых признаков — оно же индекс текущего.
  const reached = observed.findIndex((check) => !check.done)
  const current = reached === -1 ? observed.length : reached
  const ready = current === observed.length
  if (ready && !settled) setSettled(true)

  return (
    <div className="flex flex-col gap-4">
      <NodeDeploy secret={secret} />

      <div className="flex items-center gap-3">
        {ready ? (
          <span className="flex size-5 items-center justify-center rounded-full border border-ok-line bg-ok-bg text-ok-fg">
            <Check className="size-3" />
          </span>
        ) : (
          <Spinner className="size-5 border-2" />
        )}
        <p className={cn('text-[13.5px] font-semibold', ready ? 'text-ok-fg' : 'text-fg')}>
          {ready ? t.readyTitle : t.checkingTitle}
        </p>
      </div>

      <div className="overflow-hidden rounded-xl border border-line bg-raised">
        {observed.map((check, index) => {
          const done = index < current
          const active = index === current
          return (
            <div
              key={check.label}
              className="flex items-center gap-3 border-b border-line-soft px-3.5 py-3 last:border-b-0"
            >
              <span
                className={cn(
                  'flex size-4.5 shrink-0 items-center justify-center rounded-full',
                  done
                    ? 'border border-ok-line bg-ok-bg text-ok-fg'
                    : active
                      ? ''
                      : 'border border-line',
                )}
              >
                {done ? <Check className="size-2.5" /> : active ? <Spinner className="size-4.5" /> : null}
              </span>
              <span
                className={cn('text-[13px]', done ? 'text-fg' : active ? 'text-bright' : 'text-muted')}
              >
                {check.label}
              </span>
              <span className="ml-auto font-mono text-[11px] text-muted">
                {done ? check.meta || 'ok' : ''}
              </span>
            </div>
          )
        })}
      </div>
    </div>
  )
}

/** Один вид ожидания на весь шаг: и в заголовке, и в строке текущей проверки. */
function Spinner({ className }: { className?: string }) {
  return (
    <span
      aria-hidden
      className={cn('animate-spin rounded-full border border-line border-t-fg', className)}
    />
  )
}
