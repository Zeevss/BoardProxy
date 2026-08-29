import { useLanguage } from '@/app/language'
import { Button } from '@/components/ui/button'
import { CopyButton } from '@/components/ui/copy'
import { Modal } from '@/components/ui/modal'

/**
 * Показ одноразового секрета.
 *
 * Хаб хранит только хеш, поэтому закрытие диалога равнозначно потере значения.
 * Отсюда модальность вместо тоста: оператор обязан заметить и скопировать.
 */
export function SecretDialog({
  title,
  hint,
  secret,
  label,
  footnote,
  onClose,
}: {
  title: string
  /** Что делать с секретом. Про одноразовость диалог говорит сам. */
  hint?: string
  secret: string | null
  /** Имя переменной или поля, под которым секрет пойдёт в конфигурацию. */
  label?: string
  footnote?: string
  onClose: () => void
}) {
  const { t } = useLanguage()

  return (
    <Modal
      open={secret !== null}
      onOpenChange={(open) => !open && onClose()}
      title={title}
      subtitle={hint}
      className="max-w-lg"
      footer={
        <Button variant="primary" onClick={onClose}>
          {t.close}
        </Button>
      }
    >
      <p className="rounded-lg border border-warn-line bg-warn-bg px-3 py-2 text-xs text-warn">
        {t.onceWarning}
      </p>

      {secret ? (
        <div className="flex flex-col gap-1.5">
          {label ? <span className="text-xs font-medium text-soft">{label}</span> : null}
          <div className="flex items-center gap-2">
            <code className="min-w-0 flex-1 overflow-x-auto rounded-lg border border-line bg-surface px-3 py-2 font-mono text-xs whitespace-nowrap">
              {secret}
            </code>
            <CopyButton variant="raised" className="shrink-0" value={secret} label={label ?? title} />
          </div>
        </div>
      ) : null}

      {footnote ? <p className="text-xs text-dim">{footnote}</p> : null}
    </Modal>
  )
}
