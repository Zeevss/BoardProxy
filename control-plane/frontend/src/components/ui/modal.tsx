import * as Dialog from '@radix-ui/react-dialog'
import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

interface ModalProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  subtitle?: string
  footer?: ReactNode
  className?: string
  children: ReactNode
}

/**
 * Диалог по центру: шапка с пояснением, прокручиваемое тело, полоса действий.
 *
 * Прокрутка живёт на подложке, а не на теле: у окна нет заданной высоты, и при
 * прокрутке внутри длинная форма на невысоком экране обрезала бы себе шапку.
 */
export function Modal({
  open,
  onOpenChange,
  title,
  subtitle,
  footer,
  className,
  children,
}: ModalProps) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 overflow-y-auto bg-black/70 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=open]:fade-in data-[state=closed]:fade-out">
          <Dialog.Content
            className={cn(
              'relative mx-auto my-6 w-[calc(100%-2rem)] max-w-[600px] rounded-2xl',
              'border border-line bg-canvas shadow-[0_24px_60px_rgba(0,0,0,0.6)]',
              'data-[state=open]:animate-in data-[state=closed]:animate-out',
              'data-[state=open]:zoom-in-95 data-[state=closed]:zoom-out-95',
              className,
            )}
          >
            <div className="border-b border-line-soft px-5.5 pt-5 pb-4">
              <Dialog.Title className="text-[17px] font-semibold tracking-tight">
                {title}
              </Dialog.Title>
              {subtitle ? (
                <Dialog.Description className="mt-1.5 text-[12.5px] leading-relaxed text-soft">
                  {subtitle}
                </Dialog.Description>
              ) : null}
            </div>

            <div className="flex flex-col gap-3.5 px-5.5 py-4.5">{children}</div>

            {footer ? (
              <div className="flex justify-end gap-2 border-t border-line-soft px-5.5 pt-3.5 pb-4.5">
                {footer}
              </div>
            ) : null}
          </Dialog.Content>
        </Dialog.Overlay>
      </Dialog.Portal>
    </Dialog.Root>
  )
}

/** Блок формы внутри диалога: врезка с заголовком-рубрикой. */
export function ModalSection({
  title,
  hint,
  aside,
  children,
}: {
  title: string
  hint?: string
  aside?: ReactNode
  children: ReactNode
}) {
  return (
    <section className="flex flex-col gap-3 rounded-xl border border-line bg-raised p-4">
      <div className="flex items-center justify-between gap-3">
        <div>
          <h3 className="text-[12.5px] font-semibold tracking-[0.04em] uppercase">{title}</h3>
          {hint ? <p className="mt-1.5 text-[11.5px] normal-case text-dim">{hint}</p> : null}
        </div>
        {aside}
      </div>
      {children}
    </section>
  )
}
