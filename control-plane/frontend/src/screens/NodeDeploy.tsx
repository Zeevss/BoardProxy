import { useLanguage } from '@/app/language'
import { CopyButton } from '@/components/ui/copy'
import { Snippet } from '@/components/ui/snippet'
import { nodeCompose, RUN_COMMAND } from '@/lib/deploy'

/**
 * Что скопировать на сервер, чтобы нода поднялась.
 *
 * Раньше здесь была команда вида `BPROXY_NODE_SECRET=… docker compose --profile
 * node up -d --build node` — она требовала клонированного репозитория и сборки
 * образа на месте. Теперь образ забирается из GHCR, и на сервере достаточно
 * одного файла.
 *
 * Секрет показан и отдельной строкой: он уже подставлен в шаблон, но у ноды,
 * которую перевыпускают, compose на сервере обычно уже лежит — тогда менять
 * нужно ровно одно значение.
 */
export function NodeDeploy({ secret }: { secret: string }) {
  const { t } = useLanguage()
  const compose = nodeCompose({ secret })

  return (
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

      <Snippet title="docker-compose.yml" value={compose} />
      <Snippet title={t.composeLabel} value={RUN_COMMAND} />
    </>
  )
}
