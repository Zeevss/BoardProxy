import * as Dialog from '@radix-ui/react-dialog'
import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

interface SheetProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: ReactNode
  /** Читалке нужен текстовый заголовок, даже когда видимый собран из вёрстки. */
  label: string
  description?: ReactNode
  actions?: ReactNode
  /** Закреплённая полоса действий над нижним краем: «Отмена» и «Сохранить». */
  footer?: ReactNode
  children: ReactNode
}

/**
 * Боковая панель поверх списка.
 *
 * Radix Dialog, а не своя разметка: фокус-ловушка, Esc, возврат фокуса и
 * блокировка прокрутки под оверлеем — ровно то, что при ручной реализации
 * забывают и что ломает работу с клавиатуры.
 */
export function Sheet({
  open,
  onOpenChange,
  title,
  label,
  description,
  actions,
  footer,
  children,
}: SheetProps) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/60 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=open]:fade-in data-[state=closed]:fade-out" />
        <Dialog.Content
          className={cn(
            'fixed inset-y-0 right-0 z-50 flex w-full max-w-[660px] flex-col',
            'border-l border-line bg-sheet shadow-[-24px_0_60px_rgba(0,0,0,0.5)]',
            'data-[state=open]:animate-in data-[state=closed]:animate-out',
            'data-[state=open]:slide-in-from-right data-[state=closed]:slide-out-to-right',
          )}
        >
          <Dialog.Title className="sr-only">{label}</Dialog.Title>
          {description ? null : <Dialog.Description className="sr-only">{label}</Dialog.Description>}

          <header className="flex flex-col gap-3.5 border-b border-line-soft px-5.5 pt-5 pb-4">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0 flex-1">{title}</div>
              <Dialog.Close
                aria-label="Закрыть"
                className="flex size-7.5 shrink-0 items-center justify-center rounded-lg border border-line bg-raised text-base leading-none text-soft transition-colors hover:bg-line hover:text-fg"
              >
                ×
              </Dialog.Close>
            </div>

            {/* Действия живут во врезке: они меняют запись целиком, а не поле в
                форме ниже, и путать их с редактированием не должно. */}
            {actions ? (
              <div className="flex flex-wrap items-center gap-2 rounded-xl border border-line bg-inset p-2.5">
                {actions}
              </div>
            ) : null}
          </header>

          <div className="min-h-0 flex-1 overflow-y-auto">{children}</div>

          {footer ? (
            <div className="flex justify-end gap-2 border-t border-line-soft px-5.5 pt-3.5 pb-4.5">
              {footer}
            </div>
          ) : null}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
