import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import type { ComponentProps } from 'react'
import { cn } from '@/lib/utils'

const button = cva(
  'inline-flex items-center justify-center gap-2 rounded-lg whitespace-nowrap font-medium ' +
    'transition-colors disabled:pointer-events-none disabled:opacity-50 ' +
    "[&_svg]:pointer-events-none [&_svg:not([class*='size-'])]:size-4 [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        primary: 'border border-fg bg-fg text-surface hover:opacity-85',
        secondary: 'border border-line-strong bg-line text-fg hover:bg-line-strong',
        outline: 'border border-line bg-transparent text-soft hover:bg-raised hover:text-fg',
        raised: 'border border-line bg-raised text-fg hover:bg-line',
        ghost: 'text-soft hover:bg-line hover:text-fg',
        danger: 'bg-danger-solid text-white hover:bg-danger-solid/85',
        dangerGhost: 'border border-danger-line bg-danger-bg text-danger hover:brightness-125',
      },
      size: {
        /** Кнопка внутри строки списка: не должна её распирать. */
        xs: 'h-7 px-2.5 text-xs',
        sm: 'h-8 px-3 text-xs',
        md: 'h-9 px-4 text-sm',
        /** Полоса действий в панели: кнопки заметно крупнее полей формы. */
        lg: 'h-9.5 px-4 text-[13.5px] font-semibold',
        icon: 'size-8',
      },
    },
    defaultVariants: { variant: 'secondary', size: 'md' },
  },
)

export type ButtonProps = ComponentProps<'button'> &
  VariantProps<typeof button> & { asChild?: boolean }

export function Button({ className, variant, size, asChild, ...props }: ButtonProps) {
  const Component = asChild ? Slot : 'button'
  return <Component className={cn(button({ variant, size }), className)} {...props} />
}
