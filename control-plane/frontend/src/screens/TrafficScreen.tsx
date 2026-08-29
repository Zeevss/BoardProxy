import { useMemo, useState } from 'react'
import { useNodes } from '@/api/nodes'
import { useTrafficByNode, useTrafficSeries, useTrafficTotals } from '@/api/traffic'
import { useUsers } from '@/api/users'
import type { TrafficPoint, TrafficTotal } from '@/api/types'
import { useLanguage } from '@/app/language'
import { EmptyState, ScreenHeader } from '@/components/ScreenHeader'
import { StatusDot } from '@/components/ui/status'
import { bytes, percent, plural } from '@/lib/format'
import { nodeHealth } from '@/lib/health'
import { cn } from '@/lib/utils'
import { UserSheet } from './UserSheet'

type Range = '24h' | '7d' | '31d'

/**
 * Окно и шаг гистограммы.
 *
 * Шаг подобран так, чтобы столбцов было два-три десятка: на сутках это час, на
 * неделе и месяце — сутки. Более мелкий шаг на месяце дал бы семьсот столбцов
 * шириной в пиксель, из которых ничего не прочитать.
 */
const RANGES: Record<Range, { days: number; bucketSeconds: number; label: string }> = {
  '24h': { days: 1, bucketSeconds: 3600, label: '24 ч / 24h' },
  '7d': { days: 7, bucketSeconds: 86_400, label: '7 д / 7d' },
  '31d': { days: 31, bucketSeconds: 86_400, label: '31 д / 31d' },
}

export function TrafficScreen() {
  const { t, language } = useLanguage()
  const [range, setRange] = useState<Range>('7d')
  const [scope, setScope] = useState<string>('all')
  const [openUser, setOpenUser] = useState<string | null>(null)

  const nodes = useNodes()
  const users = useUsers()

  const live = useMemo(
    () => nodes.data?.items.filter((node) => node.state !== 'revoked') ?? [],
    [nodes.data],
  )
  // Нода могла исчезнуть, пока выбор держался на ней: молча возвращаемся к флоту.
  const activeScope = scope !== 'all' && !live.some((node) => node.id === scope) ? 'all' : scope
  const nodeId = activeScope === 'all' ? undefined : activeScope

  const { days, bucketSeconds } = RANGES[range]
  const series = useTrafficSeries('user', days, bucketSeconds, nodeId)
  const byNode = useTrafficByNode('user', days)
  const byUser = useTrafficTotals('user', days, nodeId)

  /** Точки приходят по подписчикам; гистограмме нужен суммарный объём интервала. */
  const buckets = useMemo(() => aggregate(series.data ?? []), [series.data])
  const peak = buckets.reduce((max, item) => Math.max(max, item.rx + item.tx), 0)

  const rxTotal = buckets.reduce((sum, item) => sum + item.rx, 0)
  const txTotal = buckets.reduce((sum, item) => sum + item.tx, 0)
  const total = rxTotal + txTotal

  const nodeRows = useMemo(() => {
    const rows = nodeId
      ? (byNode.data ?? []).filter((row) => row.subject === nodeId)
      : (byNode.data ?? [])
    return sortByVolume(rows)
  }, [byNode.data, nodeId])
  const nodeTotal = nodeRows.reduce((sum, row) => sum + row.rxBytes + row.txBytes, 0)

  const userRows = useMemo(() => sortByVolume(byUser.data ?? []).slice(0, 6), [byUser.data])
  const userTotal = (byUser.data ?? []).reduce((sum, row) => sum + row.rxBytes + row.txBytes, 0)

  const userById = useMemo(
    () => new Map((users.data?.items ?? []).map((user) => [user.id, user])),
    [users.data],
  )
  const nodeById = useMemo(() => new Map(live.map((node) => [node.id, node])), [live])

  const scopeLabel = nodeId ? (nodeById.get(nodeId)?.name ?? nodeId) : t.wholeFleet
  const selected = users.data?.items.find((user) => user.id === openUser) ?? null
  const loading = series.isLoading || byNode.isLoading || byUser.isLoading

  return (
    <section className="mx-auto flex max-w-6xl flex-col gap-4">
      <ScreenHeader
        title={t.traffic}
        subtitle={t.trafficSub}
        actions={
          <div className="flex h-8.5 gap-[3px] rounded-[9px] border border-line bg-canvas p-[3px]">
            {(Object.keys(RANGES) as Range[]).map((key) => (
              <button
                key={key}
                type="button"
                aria-pressed={range === key}
                onClick={() => setRange(key)}
                className={cn(
                  'h-6.5 rounded-md px-3 text-[12.5px] font-medium whitespace-nowrap transition-colors',
                  range === key ? 'bg-line text-fg' : 'text-dim hover:text-soft',
                )}
              >
                {RANGES[key].label}
              </button>
            ))}
          </div>
        }
      />

      <div className="flex gap-1.75 overflow-x-auto pb-0.5">
        <ScopeChip
          label={t.allNodes}
          tone="muted"
          active={activeScope === 'all'}
          onPick={() => setScope('all')}
        />
        {live.map((node) => (
          <ScopeChip
            key={node.id}
            label={node.name}
            tone={nodeHealth(node, undefined, t).tone}
            active={activeScope === node.id}
            onPick={() => setScope(node.id)}
          />
        ))}
      </div>

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <Tile label={t.totalVolume} value={bytes(total)} hint={scopeLabel} />
        <Tile label="↓ RX" value={bytes(rxTotal)} hint={`${percent(rxTotal, total)}% ${t.ofVolume}`} />
        <Tile
          label="↑ TX"
          value={bytes(txTotal)}
          hint={`${percent(txTotal, total)}% ${t.ofVolume}`}
          dim
        />
        <Tile label={t.trafficPeak} value={bytes(peak)} hint={t.busiestBucket} />
      </div>

      <section className="rounded-xl border border-line bg-canvas px-4.5 pt-4 pb-3.5">
        <header className="flex flex-wrap items-baseline justify-between gap-3">
          <div className="flex min-w-0 items-baseline gap-2.5">
            <h2 className="text-[13.5px] font-semibold">{t.trafficFlow}</h2>
            <span className="font-mono text-[11.5px] text-muted">
              {range === '24h' ? t.perHour : t.perDay} · {scopeLabel}
            </span>
          </div>
          <div className="flex gap-3.25 text-xs text-dim">
            <Legend className="bg-fg">RX</Legend>
            <Legend className="bg-line-strong">TX</Legend>
          </div>
        </header>

        {loading ? (
          <p className="py-10 text-center text-xs text-dim">{t.loading}</p>
        ) : buckets.length === 0 ? (
          <p className="py-10 text-center text-xs text-dim">{t.noTraffic}</p>
        ) : (
          <>
            <div
              className={cn(
                'mt-3.75 flex h-40 items-end',
                buckets.length > 24 ? 'gap-0.5' : 'gap-1',
              )}
            >
              {buckets.map((bucket, index) => {
                const share = peak ? ((bucket.rx + bucket.tx) / peak) * 100 : 0
                const rxShare = bucket.rx + bucket.tx ? bucket.rx / (bucket.rx + bucket.tx) : 0
                return (
                  <div
                    key={bucket.bucket}
                    title={`${bucket.bucket} · ${bytes(bucket.rx + bucket.tx)}`}
                    className={cn(
                      'flex h-full min-w-0 flex-1 flex-col justify-end gap-0.5',
                      // Последний интервал ещё набирается — приглушаем, чтобы
                      // его незаполненность не читалась как спад.
                      index === buckets.length - 1 && 'opacity-50',
                    )}
                  >
                    <div
                      className="rounded-t-[3px] bg-fg"
                      style={{ height: `${share * rxShare}%` }}
                    />
                    <div
                      className="rounded-b-[3px] bg-line-strong"
                      style={{ height: `${share * (1 - rxShare)}%` }}
                    />
                  </div>
                )
              })}
            </div>

            <div className="mt-2.25 flex justify-between font-mono text-[11px] text-muted">
              <span>{axisLabel(buckets[0]?.bucket, range, language)}</span>
              <span>{axisLabel(buckets[Math.floor(buckets.length / 2)]?.bucket, range, language)}</span>
              <span>{axisLabel(buckets.at(-1)?.bucket, range, language)}</span>
            </div>
          </>
        )}
      </section>

      <div className="grid gap-3.5 lg:grid-cols-2">
        <section className="overflow-hidden rounded-xl border border-line bg-canvas">
          <header className="flex items-baseline justify-between gap-2.5 px-4 py-3.25">
            <h2 className="text-[13.5px] font-semibold">{t.byNode}</h2>
            <span className="font-mono text-[11.5px] text-muted">
              {nodeRows.length}{' '}
              {plural(nodeRows.length, { one: t.nodeOne, few: t.nodeFew, many: t.nodeMany }, language)}
            </span>
          </header>
          {nodeRows.length === 0 ? (
            <p className="border-t border-line-soft px-4 py-3.75 text-[12.5px] text-muted">
              {t.noTraffic}
            </p>
          ) : (
            nodeRows.map((row) => {
              const node = nodeById.get(row.subject)
              return (
                <ShareRow
                  key={row.subject}
                  name={node?.name ?? row.subject}
                  tone={node ? nodeHealth(node, undefined, t).tone : 'muted'}
                  volume={row.rxBytes + row.txBytes}
                  total={nodeTotal}
                />
              )
            })
          )}
        </section>

        <section className="overflow-hidden rounded-xl border border-line bg-canvas">
          <header className="flex items-baseline justify-between gap-2.5 px-4 py-3.25">
            <h2 className="text-[13.5px] font-semibold">{t.byUser}</h2>
            <span className="font-mono text-[11.5px] text-muted">
              {(byUser.data ?? []).length}{' '}
              {plural(
                (byUser.data ?? []).length,
                { one: t.userOne, few: t.userFew, many: t.userMany },
                language,
              )}
            </span>
          </header>
          {userRows.length === 0 ? (
            <p className="border-t border-line-soft px-4 py-3.75 text-[12.5px] text-muted">
              {t.noTraffic}
            </p>
          ) : (
            userRows.map((row) => {
              const user = userById.get(row.subject)
              const quota = user?.quota
              // Доля израсходованной квоты, а не доля в объёме: именно она
              // решает, окрасить ли строку тревожно.
              const spent = quota?.enabled ? percent(quota.usedBytes, quota.limitBytes) : 0
              return (
                <ShareRow
                  key={row.subject}
                  name={user?.name ?? row.subject}
                  volume={row.rxBytes + row.txBytes}
                  total={userTotal}
                  risk={spent >= 70 ? `${spent}%` : undefined}
                  bar={spent >= 90 ? 'bg-danger' : spent >= 70 ? 'bg-warn' : 'bg-soft'}
                  onOpen={user ? () => setOpenUser(user.id) : undefined}
                />
              )
            })
          )}
        </section>
      </div>

      {!loading && total === 0 && nodeRows.length === 0 ? (
        <EmptyState>{t.noTrafficHint}</EmptyState>
      ) : null}

      <UserSheet user={selected} onClose={() => setOpenUser(null)} />
    </section>
  )
}

interface Bucket {
  bucket: string
  rx: number
  tx: number
}

/** Свод точек по интервалам: подписчики внутри интервала складываются. */
function aggregate(points: TrafficPoint[]): Bucket[] {
  const byBucket = new Map<string, Bucket>()
  for (const point of points) {
    const current = byBucket.get(point.bucket) ?? { bucket: point.bucket, rx: 0, tx: 0 }
    current.rx += point.rxBytes
    current.tx += point.txBytes
    byBucket.set(point.bucket, current)
  }
  return [...byBucket.values()].sort((left, right) => left.bucket.localeCompare(right.bucket))
}

function sortByVolume(rows: TrafficTotal[]): TrafficTotal[] {
  return [...rows].sort(
    (left, right) => right.rxBytes + right.txBytes - (left.rxBytes + left.txBytes),
  )
}

function axisLabel(iso: string | undefined, range: Range, language: 'ru' | 'en'): string {
  if (!iso) return ''
  const timestamp = Date.parse(iso)
  if (Number.isNaN(timestamp)) return ''
  return new Intl.DateTimeFormat(
    language,
    range === '24h' ? { hour: '2-digit', minute: '2-digit' } : { day: '2-digit', month: '2-digit' },
  ).format(timestamp)
}

function Legend({ className, children }: { className: string; children: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className={cn('size-2.25 rounded-[3px]', className)} />
      {children}
    </span>
  )
}

function ScopeChip({
  label,
  tone,
  active,
  onPick,
}: {
  label: string
  tone: 'ok' | 'warn' | 'danger' | 'info' | 'muted'
  active: boolean
  onPick: () => void
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onPick}
      className={cn(
        'inline-flex h-7.5 shrink-0 items-center gap-1.75 rounded-full border px-3',
        'text-[12.5px] font-medium whitespace-nowrap transition-colors',
        active ? 'border-fg bg-line text-fg' : 'border-line bg-canvas text-soft hover:border-line-strong',
      )}
    >
      <StatusDot tone={tone} className="size-1.5" />
      {label}
    </button>
  )
}

function Tile({
  label,
  value,
  hint,
  dim,
}: {
  label: string
  value: string
  hint: string
  dim?: boolean
}) {
  return (
    <div className="flex flex-col gap-1.25 rounded-xl border border-line bg-canvas px-4 py-3.5">
      <span className="text-xs font-medium text-dim">{label}</span>
      <span
        className={cn(
          'text-[22px] font-semibold tracking-tight whitespace-nowrap tabular-nums',
          dim ? 'text-bright' : 'text-fg',
        )}
      >
        {value}
      </span>
      <span className="truncate text-[11.5px] text-muted">{hint}</span>
    </div>
  )
}

function ShareRow({
  name,
  tone,
  volume,
  total,
  risk,
  bar = 'bg-fg',
  onOpen,
}: {
  name: string
  tone?: 'ok' | 'warn' | 'danger' | 'info' | 'muted'
  volume: number
  total: number
  risk?: string
  bar?: string
  onOpen?: () => void
}) {
  const share = percent(volume, total)
  const body = (
    <>
      <div className="flex min-w-0 items-center gap-2.25">
        {tone ? <StatusDot tone={tone} className="size-1.75" /> : null}
        <span className="min-w-0 flex-1 truncate text-[13px] font-medium">{name}</span>
        {risk ? (
          <span className="inline-flex h-4.5 shrink-0 items-center rounded-[5px] border border-warn-line bg-warn-bg px-1.5 text-[10.5px] font-medium text-warn-fg">
            {risk}
          </span>
        ) : null}
        <span className="shrink-0 font-mono text-[11.5px] whitespace-nowrap text-soft tabular-nums">
          {bytes(volume)}
        </span>
        <span className="w-9.5 shrink-0 text-right text-[11px] whitespace-nowrap text-muted tabular-nums">
          {share}%
        </span>
      </div>
      <div className="h-1 overflow-hidden rounded-full bg-line">
        <div
          className={cn('h-full rounded-full transition-[width] duration-300', bar)}
          style={{ width: `${Math.max(2, share)}%` }}
        />
      </div>
    </>
  )

  if (!onOpen) {
    return (
      <div className="flex flex-col gap-1.75 border-t border-line-soft px-4 py-2.75">{body}</div>
    )
  }

  return (
    <button
      type="button"
      onClick={onOpen}
      className="flex w-full flex-col gap-1.75 border-t border-line-soft px-4 py-2.75 text-left transition-colors hover:bg-raised"
    >
      {body}
    </button>
  )
}
