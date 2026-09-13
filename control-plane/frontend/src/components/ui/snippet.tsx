import { useT } from '@/app/language'
import { CopyButton } from './copy'
import { cn } from '@/lib/utils'

/**
 * Блок кода, который целиком копируют.
 *
 * Кнопка стоит в шапке, а не под текстом: шаблон compose занимает пол-экрана,
 * и до кнопки внизу пришлось бы прокручивать ровно то, что и так собираются
 * скопировать не читая.
 */
export function Snippet({
  title,
  value,
  className,
  maxHeight = 320,
}: {
  /** Имя файла или назначение — подпись слева в шапке. */
  title: string
  value: string
  className?: string
  maxHeight?: number
}) {
  const t = useT()

  return (
    <section className={cn('overflow-hidden rounded-[10px] border border-line bg-sheet', className)}>
      <header className="flex items-center justify-between gap-3 border-b border-line-soft py-2 pr-2 pl-3.5">
        <span className="truncate font-mono text-[11.5px] text-dim">{title}</span>
        <CopyButton size="xs" variant="raised" className="shrink-0" value={value} label={title}>
          {t.copy}
        </CopyButton>
      </header>
      <pre
        className="overflow-auto px-3.5 py-3 font-mono text-[11.5px] leading-relaxed text-soft"
        style={{ maxHeight }}
      >
        {value}
      </pre>
    </section>
  )
}
