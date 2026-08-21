import { cva, type VariantProps } from 'class-variance-authority'
import type { ComponentProps } from 'react'
import { cn } from '@/lib/utils'

/**
 * Тон состояния задаётся одним словом и разворачивается в пару «точка + бейдж».
 * Держать их вместе важно: разъехавшись, они начинают противоречить друг другу.
 */
const dot = cva('inline-block size-2 shrink-0 rounded-full', {
  variants: {
    tone: {
      ok: 'bg-ok',
      warn: 'bg-warn',
      danger: 'bg-danger',
      info: 'bg-info',
      muted: 'bg-line-strong',
    },
    live: { true: 'animate-pulse', false: '' },
  },
  defaultVariants: { tone: 'muted', live: false },
})

export type Tone = NonNullable<VariantProps<typeof dot>['tone']>

export function StatusDot({ tone, live, className }: VariantProps<typeof dot> & { className?: string }) {
  return <span aria-hidden className={cn(dot({ tone, live }), className)} />
}

const badge = cva(
  'inline-flex items-center gap-1.5 rounded-md border px-2 py-0.5 text-xs font-medium',
  {
    variants: {
      tone: {
        ok: 'border-ok-line bg-ok-bg text-ok-fg',
        warn: 'border-warn-line bg-warn-bg text-warn-fg',
        danger: 'border-danger-line bg-danger-bg text-danger',
        info: 'border-info-line bg-info-bg text-info',
        muted: 'border-line bg-raised text-soft',
      },
    },
    defaultVariants: { tone: 'muted' },
  },
)

export type BadgeProps = ComponentProps<'span'> & VariantProps<typeof badge>

export function Badge({ className, tone, ...props }: BadgeProps) {
  return <span className={cn(badge({ tone }), className)} {...props} />
}
