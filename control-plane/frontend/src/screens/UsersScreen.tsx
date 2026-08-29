import { useMemo, useState } from 'react'
import { Plus } from 'lucide-react'
import { useUsers } from '@/api/users'
import type { User } from '@/api/types'
import { useLanguage } from '@/app/language'
import {
  EmptyState,
  FilterMeta,
  FilterRow,
  FilterTabs,
  Row,
  RowList,
  ScreenHeader,
} from '@/components/ScreenHeader'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge, StatusDot } from '@/components/ui/status'
import { bytes, percent, plural, relativeTime } from '@/lib/format'
import { userStatus, type UserStatusKey } from '@/lib/health'
import { cn } from '@/lib/utils'
import { CreateUserDialog } from './CreateUserDialog'
import { UserSheet } from './UserSheet'

type Filter = 'all' | UserStatusKey

export function UsersScreen() {
  const { t, language } = useLanguage()
  const [filter, setFilter] = useState<Filter>('all')
  const [search, setSearch] = useState('')
  const [openUser, setOpenUser] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)

  const users = useUsers()

  const rows = useMemo(() => {
    const query = search.trim().toLowerCase()
    return (users.data?.items ?? [])
      .map((user) => ({ user, status: userStatus(user, t) }))
      .filter(({ user }) =>
        !query || `${user.id} ${user.name} ${user.description}`.toLowerCase().includes(query),
      )
      .filter(({ status }) => filter === 'all' || status.key === filter)
  }, [users.data, t, search, filter])

  const buckets = useMemo(() => {
    const counts: Record<UserStatusKey, number> = { active: 0, pending: 0, off: 0 }
    for (const user of users.data?.items ?? []) counts[userStatus(user, t).key] += 1
    return counts
  }, [users.data, t])

  const total = users.data?.items.length ?? 0
  const selected = users.data?.items.find((user) => user.id === openUser) ?? null

  const create = (
    <Button variant="primary" onClick={() => setCreating(true)}>
      <Plus />
      {t.newUser}
    </Button>
  )

  return (
    <section className="mx-auto flex max-w-6xl flex-col gap-4.5">
      <ScreenHeader
        title={t.users}
        subtitle={t.usersSub}
        actions={
          <>
            <Input
              type="search"
              placeholder={t.search}
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              className="w-40 sm:w-55"
            />
            {create}
          </>
        }
      />

      <FilterRow>
        <FilterTabs
          value={filter}
          onChange={setFilter}
          options={[
            { key: 'all', label: t.filterAll, count: total },
            { key: 'active', label: t.filterActive, count: buckets.active },
            { key: 'pending', label: t.filterPending, count: buckets.pending },
            { key: 'off', label: t.filterOffU, count: buckets.off },
          ]}
        />
        <FilterMeta>
          {total} {plural(total, { one: t.userOne, few: t.userFew, many: t.userMany }, language)} ·{' '}
          {t.metaPending} {buckets.pending}
        </FilterMeta>
      </FilterRow>

      {users.isLoading ? (
        <EmptyState>{t.loading}</EmptyState>
      ) : rows.length === 0 ? (
        <EmptyState action={create}>{total === 0 ? t.usersEmpty : t.nothingFound}</EmptyState>
      ) : (
        <RowList>
          {rows.map(({ user, status }) => (
            <UserRow
              key={user.id}
              user={user}
              status={status}
              language={language}
              onOpen={() => setOpenUser(user.id)}
            />
          ))}
        </RowList>
      )}

      <UserSheet user={selected} onClose={() => setOpenUser(null)} />
      <CreateUserDialog open={creating} onClose={() => setCreating(false)} />
    </section>
  )
}

interface UserRowProps {
  user: User
  status: ReturnType<typeof userStatus>
  language: 'ru' | 'en'
  onOpen: () => void
}

function UserRow({ user, status, language, onOpen }: UserRowProps) {
  const { t } = useLanguage()
  const seen = relativeTime(user.lastSeenAt, language)
  const nodes =
    user.nodeIds.length > 0
      ? user.nodeIds.slice(0, 2).join(', ') +
        (user.nodeIds.length > 2 ? ` +${user.nodeIds.length - 2}` : '')
      : t.noNodes

  return (
    <Row onOpen={onOpen}>
      <StatusDot tone={status.tone} live={status.live} className="size-2.25" />

      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-[13.5px] font-medium">{user.name}</span>
          <Badge tone={status.tone} className="shrink-0 px-1.75 py-0 text-[11px]">
            {status.label}
          </Badge>
        </div>
        <div className="flex min-w-0 items-center gap-1.75 text-[11.5px] text-dim">
          <span className="truncate">
            {user.description ? `${user.description} · ` : ''}
            {user.id}
          </span>
          <span className="shrink-0 text-line-strong">·</span>
          <span className="shrink-0 font-mono text-[10.5px] text-muted">{nodes}</span>
        </div>
      </div>

      <div className="hidden w-33 shrink-0 flex-col gap-1.25 sm:flex">
        <QuotaBar user={user} />
      </div>

      <span
        className={cn(
          'hidden w-19.5 shrink-0 text-right text-xs whitespace-nowrap sm:block',
          seen ? 'text-soft' : 'text-info',
        )}
      >
        {seen ?? t.never}
      </span>
    </Row>
  )
}

/**
 * Полоса расхода квоты.
 *
 * Отсутствие квоты — не ноль, а «без ограничения»: лимит в базе всегда
 * положительный, поэтому безлимитного пользователя выражает именно её
 * отсутствие. Полосе тогда нечего показывать.
 */
function QuotaBar({ user }: { user: User }) {
  const { t } = useLanguage()
  const quota = user.quota

  if (!quota || !quota.enabled) {
    return <p className="font-mono text-[10.5px] text-dim">{t.trafficOff}</p>
  }

  const filled = percent(quota.usedBytes, quota.limitBytes)

  return (
    <>
      <div className="h-1 overflow-hidden rounded-full bg-line">
        <div
          className={cn(
            'h-full rounded-full',
            filled >= 90 ? 'bg-danger' : filled >= 70 ? 'bg-warn' : 'bg-fg',
          )}
          style={{ width: `${Math.max(2, filled)}%` }}
        />
      </div>
      <span className="font-mono text-[10.5px] whitespace-nowrap text-dim">
        {bytes(quota.usedBytes)} / {bytes(quota.limitBytes)}
      </span>
    </>
  )
}
