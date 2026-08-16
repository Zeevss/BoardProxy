export function bytes(value = 0) {
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let amount = Math.max(0, value)
  let unit = 0
  while (amount >= 1000 && unit < units.length - 1) { amount /= 1000; unit++ }
  return `${amount >= 100 || unit === 0 ? amount.toFixed(0) : amount.toFixed(1)} ${units[unit]}`
}
export function rate(bytesPerBucket = 0, seconds = 300) { return `${bytes(bytesPerBucket / seconds)}/s` }
export function ago(value?: string) {
  if (!value) return '—'
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 1000))
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h`
  return `${Math.floor(seconds / 86400)}d`
}
export function date(value?: string) { return value ? new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) : '—' }
export function short(value?: string, length = 18) { return value && value.length > length ? `${value.slice(0, length / 2)}…${value.slice(-length / 2)}` : value ?? '—' }
export function eventKind(type: string) { return type.includes('runtime') || type.includes('session') || type.includes('board') ? 'runtime' : type.includes('cert') || type.includes('token') || type.includes('security') ? 'security' : type.includes('catalog') || type.includes('user') ? 'catalog' : 'status' }
export function message(payload: Record<string, unknown>) {
  const preferred = ['message', 'detail', 'error', 'reason']
  for (const key of preferred) if (typeof payload[key] === 'string') return payload[key] as string
  const entries = Object.entries(payload).slice(0, 4).map(([key, value]) => `${key}=${String(value)}`)
  return entries.join(' · ') || '—'
}
