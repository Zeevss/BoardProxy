import type { ComponentProps } from 'react'
import { cn } from '@/lib/utils'

export function Textarea({ className, ...props }: ComponentProps<'textarea'>) {
  return (
    <textarea
      className={cn(
        'min-h-[74px] w-full resize-y rounded-lg border border-line bg-raised px-3 py-2.5',
        'text-sm leading-relaxed text-fg transition-colors',
        'placeholder:text-muted focus-visible:border-muted',
        'disabled:cursor-not-allowed disabled:opacity-50',
        className,
      )}
      {...props}
    />
  )
}
