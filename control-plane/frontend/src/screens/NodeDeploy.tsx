import { useLanguage } from '@/app/language'
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
 * Секрет отдельной строкой не показываем: он и так внутри шаблона, а вторая
 * копия на экране — это лишний повод оставить учётные данные в буфере обмена
 * или на скриншоте. Копируют целиком файл, а не значение из него.
 */
export function NodeDeploy({ secret }: { secret: string }) {
  const { t } = useLanguage()

  return (
    <>
      <p className="text-[11.5px] text-warn">{t.onceOnly}</p>
      <Snippet title="docker-compose.yml" value={nodeCompose({ secret })} />
      <Snippet title={t.composeLabel} value={RUN_COMMAND} />
    </>
  )
}
