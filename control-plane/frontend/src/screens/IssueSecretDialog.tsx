import { useState } from 'react'
import { useIssueEnrollmentToken } from '@/api/nodes'
import type { IssuedEnrollmentToken } from '@/api/types'
import { ApiError } from '@/api/errors'
import { useLanguage } from '@/app/language'
import { Button } from '@/components/ui/button'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Modal } from '@/components/ui/modal'
import { GRPC_PORT, hubAddressProblem, suggestHubAddress } from '@/lib/hub-address'

/**
 * Перевыпуск enrollment-секрета.
 *
 * Спрашиваем адрес хаба, а не подставляем `window.location.origin`: ноде он
 * уезжает как цель gRPC, и origin панели там не работает ни портом, ни схемой,
 * ни именем в сертификате.
 */
export function IssueSecretDialog({
  nodeId,
  onIssued,
  onClose,
}: {
  nodeId: string | null
  onIssued: (issued: IssuedEnrollmentToken) => void
  onClose: () => void
}) {
  const { t } = useLanguage()
  const issue = useIssueEnrollmentToken()

  const [openFor, setOpenFor] = useState<string | null>(null)
  const [address, setAddress] = useState('')
  const [error, setError] = useState<string | null>(null)

  if (nodeId && nodeId !== openFor) {
    setOpenFor(nodeId)
    setAddress(suggestHubAddress())
    setError(null)
  }
  if (!nodeId && openFor !== null) setOpenFor(null)

  function submit() {
    setError(null)
    const problem = hubAddressProblem(address)
    if (problem) {
      setError(problem === 'scheme' ? t.hubUrlScheme : problem === 'port' ? t.hubUrlPort : t.hubUrlHint)
      return
    }
    if (!nodeId) return
    issue.mutate(
      { nodeId, hubUrl: address.trim() },
      {
        onSuccess: (issued) => {
          onIssued(issued)
          onClose()
        },
        onError: (cause) => setError(cause instanceof ApiError ? cause.message : t.errorOffline),
      },
    )
  }

  return (
    <Modal
      open={nodeId !== null}
      onOpenChange={(next) => !next && onClose()}
      title={t.issueSecret}
      subtitle={t.secretHint}
      className="max-w-lg"
      footer={
        <>
          <Button variant="outline" disabled={issue.isPending} onClick={onClose}>
            {t.cancel}
          </Button>
          <Button variant="primary" disabled={issue.isPending} onClick={submit}>
            {t.issueToken}
          </Button>
        </>
      }
    >
      <Field label={t.hubUrl} hint={t.hubUrlHint}>
        <Input
          autoFocus
          placeholder={`hub:${GRPC_PORT}`}
          className="font-mono"
          value={address}
          onChange={(event) => setAddress(event.target.value)}
          onKeyDown={(event) => event.key === 'Enter' && submit()}
        />
      </Field>

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
