import { useEffect, type FormEvent, type ReactNode } from 'react'
import { Icon } from './Icon'

export function Modal({ title, hint, children, submitLabel, busy, onClose, onSubmit }: {
  title: string; hint?: string; children: ReactNode; submitLabel?: string; busy?: boolean; onClose: () => void; onSubmit?: (event: FormEvent<HTMLFormElement>) => void
}) {
  useEffect(() => {
    const close = (event: KeyboardEvent) => { if (event.key === 'Escape') onClose() }
    window.addEventListener('keydown', close)
    return () => window.removeEventListener('keydown', close)
  }, [onClose])
  return <div className="modal-backdrop" onMouseDown={event => { if (event.target === event.currentTarget) onClose() }}>
    <form className="modal" onSubmit={onSubmit}>
      <header><div><h2>{title}</h2>{hint ? <p>{hint}</p> : null}</div><button type="button" className="icon-button" aria-label="Close" onClick={onClose}><Icon name="close" /></button></header>
      <div className="modal-body">{children}</div>
      <footer><button type="button" className="button ghost" onClick={onClose}>Cancel</button>{submitLabel ? <button className="button primary" disabled={busy} type="submit">{busy ? '…' : submitLabel}</button> : null}</footer>
    </form>
  </div>
}

export function Field({ label, children, hint }: { label: string; children: ReactNode; hint?: string }) {
  return <label className="field"><span>{label}</span>{children}{hint ? <small>{hint}</small> : null}</label>
}

export function SecretResult({ label, value }: { label: string; value: string }) {
  return <div className="secret-result"><span>{label}</span><code>{value}</code><button type="button" className="button secondary" onClick={() => void navigator.clipboard.writeText(value)}><Icon name="copy" size={15}/> Copy</button></div>
}
