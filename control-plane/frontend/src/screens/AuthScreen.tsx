import { useQuery } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { fetchAuthStatus, useAuth } from '@/app/auth'
import { useT } from '@/app/language'
import { Button } from '@/components/ui/button'
import { Field } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { ApiError } from '@/api/errors'

/**
 * Вход и первичная настройка — один экран.
 *
 * Режим выбирает не оператор, а хаб: `setupRequired` истинно ровно тогда, когда
 * администратора ещё нет. Ручное переключение позволило бы уйти на форму, где
 * сервер всё равно откажет.
 */
export function AuthScreen() {
  const t = useT()
  const { login, setup } = useAuth()
  const status = useQuery({ queryKey: ['auth', 'status'], queryFn: fetchAuthStatus })

  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [repeat, setRepeat] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const isSetup = status.data?.setupRequired === true

  async function onSubmit(event: FormEvent) {
    event.preventDefault()
    setError(null)
    if (isSetup && password !== repeat) {
      setError(t.errorPasswordMismatch)
      return
    }
    setBusy(true)
    try {
      await (isSetup ? setup(username, password) : login(username, password))
    } catch (cause) {
      setError(cause instanceof ApiError ? cause.message : t.errorOffline)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-full items-center justify-center bg-surface p-6">
      <div className="flex w-full max-w-sm flex-col gap-6">
        <div className="flex items-center justify-center gap-2.5">
          <div className="flex size-8 items-center justify-center rounded-lg bg-fg text-xs font-semibold text-surface">
            BP
          </div>
          <span className="text-sm font-medium">BoardProxy Control</span>
        </div>

        <form onSubmit={onSubmit} className="flex flex-col gap-4 rounded-xl border border-line bg-canvas p-5">
          <div className="flex flex-col gap-1">
            <h1 className="text-base font-medium">{isSetup ? t.authTitleSetup : t.authTitleLogin}</h1>
            <p className="text-xs leading-relaxed text-dim">
              {isSetup ? t.authHintSetup : t.authHintLogin}
            </p>
          </div>

          <Field label={t.username} htmlFor="username">
            <Input
              id="username"
              autoComplete="username"
              autoFocus
              value={username}
              onChange={(event) => setUsername(event.target.value)}
            />
          </Field>

          <Field label={t.password} htmlFor="password">
            <Input
              id="password"
              type="password"
              autoComplete={isSetup ? 'new-password' : 'current-password'}
              value={password}
              onChange={(event) => setPassword(event.target.value)}
            />
          </Field>

          {isSetup ? (
            <Field label={t.passwordRepeat} htmlFor="repeat">
              <Input
                id="repeat"
                type="password"
                autoComplete="new-password"
                value={repeat}
                onChange={(event) => setRepeat(event.target.value)}
              />
            </Field>
          ) : null}

          {error ? (
            <p role="alert" className="rounded-lg border border-danger-line bg-danger-bg px-3 py-2 text-xs text-danger">
              {error}
            </p>
          ) : null}

          <Button type="submit" variant="primary" disabled={busy || status.isLoading}>
            {isSetup ? t.actionSetup : t.actionLogin}
          </Button>
        </form>
      </div>
    </div>
  )
}
