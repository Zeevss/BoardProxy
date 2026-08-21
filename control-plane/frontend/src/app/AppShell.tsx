import { NavLink, Outlet } from 'react-router'
import { useAuth } from './auth'
import { useLanguage } from './language'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import type { Language } from '@/lib/i18n'

const NAV = [
  { to: '/', key: 'overview' },
  { to: '/nodes', key: 'nodes' },
  { to: '/users', key: 'users' },
  { to: '/boards', key: 'boards' },
  { to: '/traffic', key: 'traffic' },
  { to: '/settings', key: 'settings' },
] as const

export function AppShell() {
  const { t, language, setLanguage } = useLanguage()
  const { logout } = useAuth()

  return (
    <div className="flex min-h-full flex-col">
      <header className="sticky top-0 z-20 border-b border-line bg-canvas/85 backdrop-blur">
        <div className="flex h-14 items-center gap-4 px-4 sm:px-6">
          <div className="flex items-center gap-2.5">
            <div className="flex size-7 items-center justify-center rounded-md bg-fg text-[11px] font-semibold text-surface">
              BP
            </div>
            <span className="hidden text-sm font-medium sm:inline">BoardProxy Control</span>
          </div>

          <nav className="flex flex-1 items-center gap-1 overflow-x-auto">
            {NAV.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.to === '/'}
                className={({ isActive }) =>
                  cn(
                    'rounded-lg px-3 py-1.5 text-sm whitespace-nowrap transition-colors',
                    isActive ? 'bg-line text-fg' : 'text-dim hover:text-soft',
                  )
                }
              >
                {t[item.key]}
              </NavLink>
            ))}
          </nav>

          <div className="flex items-center gap-1">
            <LanguageToggle language={language} onChange={setLanguage} />
            <Button variant="ghost" size="sm" onClick={() => void logout()}>
              {t.signOut}
            </Button>
          </div>
        </div>
      </header>

      <main className="flex-1 px-4 py-6 sm:px-6">
        <Outlet />
      </main>
    </div>
  )
}

function LanguageToggle({
  language,
  onChange,
}: {
  language: Language
  onChange: (next: Language) => void
}) {
  return (
    <div className="flex items-center rounded-lg border border-line p-0.5">
      {(['ru', 'en'] as const).map((code) => (
        <button
          key={code}
          type="button"
          onClick={() => onChange(code)}
          aria-pressed={language === code}
          className={cn(
            'rounded-md px-2 py-0.5 text-xs uppercase transition-colors',
            language === code ? 'bg-line text-fg' : 'text-dim hover:text-soft',
          )}
        >
          {code}
        </button>
      ))}
    </div>
  )
}
