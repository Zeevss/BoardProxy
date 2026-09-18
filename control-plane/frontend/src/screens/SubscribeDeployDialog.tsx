import { useLanguage } from '@/app/language'
import { Button } from '@/components/ui/button'
import { CopyButton } from '@/components/ui/copy'
import { Modal } from '@/components/ui/modal'
import { Snippet } from '@/components/ui/snippet'
import { RUN_COMMAND, subscribeCompose, suggestControlPlaneUrl } from '@/lib/deploy'

/**
 * Сервисный токен вместе с готовым шаблоном развёртывания.
 *
 * Адрес хаба не берётся из origin панели напрямую: на `localhost` он указывал
 * бы внутрь контейнера самого сервиса, и тот бесконечно повторял бы
 * «connect: connection refused». Для локальной панели подставляем имя из
 * compose хаба и сразу подключаем сервис к его сети.
 */
export function SubscribeDeployDialog({
  token,
  onClose,
}: {
  token: string | null
  onClose: () => void
}) {
  const { t } = useLanguage()
  const hub = suggestControlPlaneUrl(window.location)
  const compose = token
    ? subscribeCompose({ token, controlPlaneUrl: hub.url, sameHost: hub.sameHost })
    : ''

  return (
    <Modal
      open={token !== null}
      onOpenChange={(open) => !open && onClose()}
      title={t.serviceTokenIssued}
      className="max-w-[580px]"
      footer={
        <Button variant="primary" onClick={onClose}>
          {t.close}
        </Button>
      }
    >
      {token ? (
        <>
          <div className="flex flex-col gap-1.5">
            <div className="flex items-center justify-between gap-3">
              <span className="text-[13px] font-medium text-bright">{t.serviceToken}</span>
              <span className="text-[11.5px] text-warn">{t.onceOnly}</span>
            </div>
            <div className="flex gap-2">
              <p className="min-w-0 flex-1 rounded-lg border border-line bg-raised px-3 py-2.5 font-mono text-xs break-all text-bright">
                {token}
              </p>
              <CopyButton
                variant="raised"
                className="shrink-0"
                value={token}
                label={t.serviceToken}
              >
                {t.copy}
              </CopyButton>
            </div>
          </div>

          <Snippet title="docker-compose.yml" value={compose} />
          <Snippet title={t.composeLabel} value={RUN_COMMAND} />
        </>
      ) : null}
    </Modal>
  )
}
