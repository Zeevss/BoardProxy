import { useLanguage } from '@/app/language'
import { absoluteTime } from '@/lib/format'
import { SecretDialog } from './SecretDialog'

interface EnrollmentSecret {
  nodeSecret: string
  expiresAt: string
}

/** Enrollment-секрет ноды: живёт пятнадцать минут и показывается один раз. */
export function EnrollmentSecretDialog({
  secret,
  onClose,
}: {
  secret: EnrollmentSecret | null
  onClose: () => void
}) {
  const { t, language } = useLanguage()

  return (
    <SecretDialog
      title={t.secretTitle}
      hint={t.secretHint}
      label="BPROXY_NODE_SECRET"
      secret={secret?.nodeSecret ?? null}
      footnote={secret ? `${t.validUntil}: ${absoluteTime(secret.expiresAt, language)}` : undefined}
      onClose={onClose}
    />
  )
}
