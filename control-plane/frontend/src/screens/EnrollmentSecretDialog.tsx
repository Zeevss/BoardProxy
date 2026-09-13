import { useLanguage } from '@/app/language'
import { Button } from '@/components/ui/button'
import { Modal } from '@/components/ui/modal'
import { absoluteTime } from '@/lib/format'
import { NodeDeploy } from './NodeDeploy'

interface EnrollmentSecret {
  nodeSecret: string
  expiresAt: string
}

/**
 * Enrollment-секрет ноды: живёт пятнадцать минут и показывается один раз.
 *
 * Здесь не общий SecretDialog, а свой: вместе с секретом отдаём готовый
 * docker-compose с уже подставленным значением — на сервере не остаётся шага,
 * на котором секрет можно потерять или вписать с опечаткой.
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
    <Modal
      open={secret !== null}
      onOpenChange={(open) => !open && onClose()}
      title={t.secretTitle}
      className="max-w-[580px]"
      footer={
        <Button variant="primary" onClick={onClose}>
          {t.close}
        </Button>
      }
    >
      {secret ? (
        <>
          <NodeDeploy secret={secret.nodeSecret} />
          <p className="text-xs text-dim">
            {t.validUntil}: {absoluteTime(secret.expiresAt, language)}
          </p>
        </>
      ) : null}
    </Modal>
  )
}
