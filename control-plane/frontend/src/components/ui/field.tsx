import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

interface FieldProps {
  label: string
  hint?: ReactNode
  htmlFor?: string
  className?: string
  children: ReactNode
}

/** Подпись, контрол и пояснение под ним — единственная раскладка форм в панели. */
export function Field({ label, hint, htmlFor, className, children }: FieldProps) {
  return (
    <div className={cn('flex flex-col gap-1.5', className)}>
      <label htmlFor={htmlFor} className="text-xs font-medium text-soft">
        {label}
      </label>
      {children}
      {hint ? <p className="text-xs text-muted">{hint}</p> : null}
    </div>
  )
}
