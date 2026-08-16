import { useState, type FormEvent } from 'react'
import { PanelAuthApi } from '../api/controlApi'
import type { PanelSession } from '../types'

const api = new PanelAuthApi()

export function PanelAuthScreen({ mode, initialError, onAuthenticated, onRetry }: {
  mode: 'setup' | 'login'
  initialError?: string
  onAuthenticated: (session: PanelSession) => void
  onRetry: () => void
}) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(initialError)
  const setup = mode === 'setup'

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const form = new FormData(event.currentTarget)
    const username = String(form.get('username')).trim()
    const password = String(form.get('password'))
    if (setup && password !== String(form.get('passwordConfirmation'))) {
      setError('Пароли не совпадают')
      return
    }
    setBusy(true)
    setError(undefined)
    try {
      onAuthenticated(setup ? await api.setup(username, password) : await api.login(username, password))
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Не удалось войти')
    } finally {
      setBusy(false)
    }
  }

  return <main className="login-screen">
    <form className="login-panel" onSubmit={submit}>
      <div className="brand login-brand"><span className="brand-mark"><span/></span><div><strong>BoardProxy</strong><small>Control plane</small></div></div>
      <h1>{setup ? 'Создание администратора' : 'Вход в панель'}</h1>
      <p>{setup ? 'Панель запускается впервые. Задайте учётные данные владельца — повторная регистрация будет закрыта.' : 'Используйте имя администратора и пароль, созданные при первом запуске.'}</p>
      {error ? <div className="auth-error" role="alert"><span>{error}</span>{initialError ? <button type="button" onClick={onRetry}>Повторить</button> : null}</div> : null}
      <label className="field"><span>Имя пользователя</span><input autoFocus required name="username" minLength={3} maxLength={64} pattern="[A-Za-z0-9][A-Za-z0-9._]*(?:-[A-Za-z0-9._]+)*" autoComplete="username" placeholder="admin"/></label>
      <label className="field"><span>Пароль</span><input required name="password" type="password" minLength={10} maxLength={256} autoComplete={setup ? 'new-password' : 'current-password'} placeholder="Минимум 10 символов"/></label>
      {setup ? <label className="field"><span>Повторите пароль</span><input required name="passwordConfirmation" type="password" minLength={10} maxLength={256} autoComplete="new-password"/></label> : null}
      <button className="button primary login-submit" type="submit" disabled={busy}>{busy ? 'Подождите…' : setup ? 'Создать администратора' : 'Войти'}</button>
      <small className="login-note">Пароль хранится как BCrypt-хэш. Сессия действует только в текущей вкладке браузера.</small>
    </form>
  </main>
}
