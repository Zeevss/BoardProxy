import * as SwitchPrimitive from '@radix-ui/react-switch'
import { cn } from '@/lib/utils'

export function Switch({
  checked,
  onCheckedChange,
  label,
  disabled,
}: {
  checked: boolean
  onCheckedChange: (next: boolean) => void
  /** Читалке нужно имя: рядом с переключателем в дизайне стоит заголовок секции. */
  label: string
  disabled?: boolean
}) {
  return (
    <SwitchPrimitive.Root
      checked={checked}
      onCheckedChange={onCheckedChange}
      disabled={disabled}
      aria-label={label}
      className={cn(
        'flex h-5.5 w-9.5 shrink-0 items-center rounded-full p-0.5 transition-colors',
        'disabled:cursor-not-allowed disabled:opacity-50',
        checked ? 'bg-ok-fg' : 'bg-line-strong',
      )}
    >
      <SwitchPrimitive.Thumb className="size-4.5 rounded-full bg-fg shadow-sm transition-transform data-[state=checked]:translate-x-4" />
    </SwitchPrimitive.Root>
  )
}
