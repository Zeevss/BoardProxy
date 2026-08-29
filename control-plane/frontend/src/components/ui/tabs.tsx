import * as TabsPrimitive from '@radix-ui/react-tabs'
import type { ComponentProps } from 'react'
import { cn } from '@/lib/utils'

export const Tabs = TabsPrimitive.Root

/**
 * Сегментированный переключатель в общей рамке.
 *
 * Не подчёркивание: вкладки здесь делят одну панель на равные части, и рамка
 * показывает, что выбрана ровно одна из них.
 */
export function TabsList({ className, ...props }: ComponentProps<typeof TabsPrimitive.List>) {
  return (
    <TabsPrimitive.List
      className={cn(
        'flex gap-[3px] rounded-[9px] border border-line bg-canvas p-[3px]',
        className,
      )}
      {...props}
    />
  )
}

export function TabsTrigger({ className, ...props }: ComponentProps<typeof TabsPrimitive.Trigger>) {
  return (
    <TabsPrimitive.Trigger
      className={cn(
        'h-7 flex-1 rounded-md text-[12.5px] font-medium text-dim transition-colors',
        'hover:text-soft data-[state=active]:bg-line data-[state=active]:text-fg',
        className,
      )}
      {...props}
    />
  )
}

/** Отступы задаёт вмещающая панель: у вкладок своих полей нет. */
export function TabsContent({ className, ...props }: ComponentProps<typeof TabsPrimitive.Content>) {
  return <TabsPrimitive.Content className={cn('pt-4', className)} {...props} />
}
