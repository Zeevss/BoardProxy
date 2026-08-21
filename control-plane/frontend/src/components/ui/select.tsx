import * as Menu from '@radix-ui/react-dropdown-menu'
import { Check, ChevronDown } from 'lucide-react'
import { cn } from '@/lib/utils'

export interface SelectOption<T extends string> {
  value: T
  label: string
  /** Что произойдёт при выборе. В дизайне подпись есть у каждого варианта. */
  hint?: string
}

interface SelectProps<T extends string> {
  value: T
  options: ReadonlyArray<SelectOption<T>>
  onChange: (next: T) => void
  /** Подпись поля: у меню нет связанного `<label>`, читалке нужно имя. */
  label: string
  disabled?: boolean
}

/**
 * Выбор одного значения из короткого списка.
 *
 * Не `<select>`: у нативного списка нельзя показать пояснение под названием
 * варианта, а именно оно отличает `RESET` от `DISABLE` для того, кто видит эти
 * слова впервые. Radix-меню даёт то же поведение с клавиатуры (стрелки, Esc,
 * возврат фокуса), которое пришлось бы писать руками.
 */
export function Select<T extends string>({
  value,
  options,
  onChange,
  label,
  disabled,
}: SelectProps<T>) {
  const current = options.find((option) => option.value === value)

  return (
    <Menu.Root>
      <Menu.Trigger
        disabled={disabled}
        aria-label={label}
        className={cn(
          'group flex h-9 w-full items-center justify-between gap-2 rounded-lg border border-line',
          'bg-raised px-3 text-sm text-fg transition-colors',
          'hover:border-line-strong data-[state=open]:border-line-strong',
          'disabled:cursor-not-allowed disabled:opacity-50',
        )}
      >
        <span className="truncate">{current?.label ?? value}</span>
        <ChevronDown className="size-3.5 shrink-0 text-dim transition-transform duration-200 group-data-[state=open]:rotate-180" />
      </Menu.Trigger>

      <Menu.Portal>
        <Menu.Content
          align="start"
          sideOffset={6}
          className={cn(
            'z-50 max-h-58 w-[var(--radix-dropdown-menu-trigger-width)] overflow-y-auto',
            'rounded-xl border border-line-strong bg-pop p-1 shadow-2xl shadow-black/55',
            'data-[state=open]:animate-in data-[state=closed]:animate-out',
            'data-[state=open]:fade-in data-[state=closed]:fade-out data-[state=open]:zoom-in-95',
          )}
        >
          <Menu.RadioGroup value={value} onValueChange={(next) => onChange(next as T)}>
            {options.map((option) => (
              <Menu.RadioItem
                key={option.value}
                value={option.value}
                className={cn(
                  'flex cursor-pointer items-center justify-between gap-2.5 rounded-md px-2.5 py-1.5',
                  'text-sm text-soft outline-none transition-colors',
                  'focus:bg-line focus:text-fg data-[state=checked]:text-fg',
                )}
              >
                <span className="flex min-w-0 flex-col gap-0.5">
                  <span className="truncate">{option.label}</span>
                  {option.hint ? (
                    <span className="truncate text-[11px] text-dim">{option.hint}</span>
                  ) : null}
                </span>
                <Menu.ItemIndicator className="shrink-0">
                  <Check className="size-3.5 text-ok-fg" />
                </Menu.ItemIndicator>
              </Menu.RadioItem>
            ))}
          </Menu.RadioGroup>
        </Menu.Content>
      </Menu.Portal>
    </Menu.Root>
  )
}
