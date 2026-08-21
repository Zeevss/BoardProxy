import { Check, Copy } from 'lucide-react'
import { useState } from 'react'
import { Button, type ButtonProps } from './button'
import { useToast } from './toast'
import { useT } from '@/app/language'

/**
 * Копирование в буфер с подтверждением.
 *
 * `navigator.clipboard` доступен только в защищённом контексте, поэтому есть
 * запасной путь: без него оператор на http-стенде не смог бы скопировать
 * одноразовый секрет вовсе.
 */
async function toClipboard(text: string): Promise<boolean> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      /* падаем в запасной путь */
    }
  }
  try {
    const area = document.createElement('textarea')
    area.value = text
    area.setAttribute('readonly', '')
    area.style.position = 'fixed'
    area.style.opacity = '0'
    document.body.append(area)
    area.select()
    const ok = document.execCommand('copy')
    area.remove()
    return ok
  } catch {
    return false
  }
}

interface CopyButtonProps extends Omit<ButtonProps, 'onClick' | 'children'> {
  value: string
  label?: string
  children?: React.ReactNode
}

export function CopyButton({ value, label, children, ...props }: CopyButtonProps) {
  const t = useT()
  const { toast } = useToast()
  const [done, setDone] = useState(false)

  return (
    <Button
      {...props}
      onClick={async () => {
        const ok = await toClipboard(value)
        if (!ok) {
          toast(t.errorTitle, 'danger')
          return
        }
        setDone(true)
        toast(label ? `${label} · ${t.copied.toLowerCase()}` : t.copied)
        setTimeout(() => setDone(false), 1600)
      }}
    >
      {done ? <Check /> : <Copy />}
      {children}
    </Button>
  )
}
