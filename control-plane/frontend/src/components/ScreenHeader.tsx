import { ChevronRight } from 'lucide-react'
import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

interface ScreenHeaderProps {
  title: string
  subtitle: string
  /** Поиск и основное действие экрана: в дизайне они стоят рядом справа. */
  actions?: ReactNode
}

export function ScreenHeader({ title, subtitle, actions }: ScreenHeaderProps) {
  return (
    <header className="flex flex-wrap items-end justify-between gap-4">
      <div className="flex min-w-0 flex-col gap-1.5">
        <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
        <p className="max-w-2xl text-[13.5px] text-soft">{subtitle}</p>
      </div>
      {actions ? <div className="flex shrink-0 items-center gap-2">{actions}</div> : null}
    </header>
  )
}

/** Строка под заголовком: фильтры слева, сводка справа от них. */
export function FilterRow({ children }: { children: ReactNode }) {
  return <div className="flex flex-wrap items-center gap-3">{children}</div>
}

export function FilterMeta({ children }: { children: ReactNode }) {
  return <p className="text-[12.5px] text-muted">{children}</p>
}

interface FilterTabsProps<T extends string> {
  value: T
  options: Array<{ key: T; label: string; count: number }>
  onChange: (next: T) => void
}

/**
 * Фильтры со счётчиками: сколько записей попадёт в каждую вкладку.
 *
 * Сегментированный переключатель в общей рамке, а не отдельные кнопки: так
 * видно, что выбор один из нескольких, а не набор независимых тумблеров.
 */
export function FilterTabs<T extends string>({ value, options, onChange }: FilterTabsProps<T>) {
  return (
    <div className="flex h-8.5 gap-[3px] rounded-[9px] border border-line bg-canvas p-[3px]">
      {options.map((option) => {
        const active = option.key === value
        return (
          <button
            key={option.key}
            type="button"
            onClick={() => onChange(option.key)}
            aria-pressed={active}
            className={cn(
              'inline-flex h-6.5 items-center gap-1.5 rounded-md px-2.5 text-[12.5px] font-medium',
              'whitespace-nowrap transition-colors',
              active ? 'bg-line text-fg' : 'text-soft hover:bg-line/50',
            )}
          >
            {option.label}
            <span className={cn('font-mono text-[11px]', active ? 'text-soft' : 'text-muted')}>
              {option.count}
            </span>
          </button>
        )
      })}
    </div>
  )
}

/** Список записей одним полотном: строки разделены, а не разложены карточками. */
export function RowList({ children }: { children: ReactNode }) {
  return (
    <ul className="overflow-hidden rounded-xl border border-line bg-canvas">{children}</ul>
  )
}

export function Row({ onOpen, children }: { onOpen: () => void; children: ReactNode }) {
  return (
    <li className="border-b border-line-soft last:border-b-0">
      <button
        type="button"
        onClick={onOpen}
        className="flex w-full items-center gap-3.5 px-4 py-3.5 text-left transition-colors hover:bg-raised"
      >
        {children}
        <ChevronRight className="size-4 shrink-0 text-muted" />
      </button>
    </li>
  )
}

/** Пусто — с предложением создать первую запись, а не с одним лишь текстом. */
export function EmptyState({ action, children }: { action?: ReactNode; children: ReactNode }) {
  return (
    <div className="flex flex-col items-center gap-3 rounded-xl border border-line bg-canvas px-4 py-11 text-center">
      <p className="text-sm font-medium text-soft">{children}</p>
      {action}
    </div>
  )
}
