import type { ReactNode } from 'react'
import { Icon } from './Icon'

export function Panel({ title, meta, action, children, className = '' }: { title?: string; meta?: string; action?: ReactNode; children: ReactNode; className?: string }) {
  return <section className={`panel ${className}`}>{title || action ? <header className="panel-header"><div>{title ? <h2>{title}</h2> : null}{meta ? <span>{meta}</span> : null}</div>{action}</header> : null}{children}</section>
}
export function Empty({ title = 'No data yet', text }: { title?: string; text?: string }) { return <div className="empty"><span className="empty-mark">◇</span><strong>{title}</strong>{text ? <p>{text}</p> : null}</div> }
export function Badge({ children, tone = 'neutral' }: { children: ReactNode; tone?: 'ok' | 'warn' | 'bad' | 'info' | 'neutral' }) { return <span className={`badge ${tone}`}><span className="dot"/>{children}</span> }
export function ErrorBanner({ children, onClose }: { children: ReactNode; onClose?: () => void }) { return <div className="error-banner"><Icon name="warning" size={17}/><span>{children}</span>{onClose ? <button type="button" onClick={onClose}><Icon name="close" size={15}/></button> : null}</div> }
export function ConfirmButton({ children, className = 'text-button danger', onConfirm }: { children: ReactNode; className?: string; onConfirm: () => void }) {
  return <button type="button" className={className} onClick={() => { if (window.confirm('Are you sure?')) onConfirm() }}>{children}</button>
}
