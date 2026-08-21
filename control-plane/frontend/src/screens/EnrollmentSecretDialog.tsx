import * as Dialog from '@radix-ui/react-dialog'
import { useLanguage } from '@/app/language'
import { Button } from '@/components/ui/button'
import { CopyButton } from '@/components/ui/copy'
import { absoluteTime } from '@/lib/format'

interface EnrollmentSecret {
  nodeSecret: string
  expiresAt: string
}

/**
 * Показ одноразового enrollment-секрета.
 *
 * Хаб хранит только его хеш, а живёт секрет пятнадцать минут, поэтому закрытие
 * диалога равнозначно потере значения. Отсюда и модальность вместо тоста:
 * оператор обязан заметить и скопировать.
 */
export function EnrollmentSecretDialog({
  secret,
  onClose,
}: {
  secret: EnrollmentSecret | null
  onClose: () => void
}) {
  const { t, language } = useLanguage()

  return (
    <Dialog.Root open={secret !== null} onOpenChange={(open) => !open && onClose()}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 bg-surface/80 backdrop-blur-sm" />
        <Dialog.Content className="fixed top-1/2 left-1/2 z-50 w-[calc(100%-2rem)] max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-xl border border-line bg-canvas p-5">
          <Dialog.Title className="text-base font-medium">{t.secretTitle}</Dialog.Title>
          <Dialog.Description className="mt-1 text-xs leading-relaxed text-dim">
            {t.secretHint}
          </Dialog.Description>

          <div className="mt-4 rounded-lg border border-warn-line bg-warn-bg px-3 py-2 text-xs text-warn">
            {t.onceWarning}
          </div>

          {secret ? (
            <div className="mt-4 flex flex-col gap-3">
              <div className="flex flex-col gap-1.5">
                <span className="text-xs font-medium text-soft">BPROXY_NODE_SECRET</span>
                <div className="flex items-center gap-2">
                  <code className="min-w-0 flex-1 overflow-x-auto rounded-lg border border-line bg-surface px-3 py-2 font-mono text-xs whitespace-nowrap text-fg">
                    {secret.nodeSecret}
                  </code>
                  <CopyButton
                    size="sm"
                    variant="outline"
                    value={secret.nodeSecret}
                    label="BPROXY_NODE_SECRET"
                  />
                </div>
              </div>

              <p className="text-xs text-dim">
                {t.validUntil}: {absoluteTime(secret.expiresAt, language)}
              </p>
            </div>
          ) : null}

          <div className="mt-5 flex justify-end">
            <Dialog.Close asChild>
              <Button variant="primary" size="sm">
                {t.close}
              </Button>
            </Dialog.Close>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
