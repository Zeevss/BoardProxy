import { QueryClientProvider } from '@tanstack/react-query'
import { useState } from 'react'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router'
import { useControlEvents } from './api/events'
import { AppShell } from './app/AppShell'
import { AuthProvider, useAuth } from './app/auth'
import { LanguageProvider, useT } from './app/language'
import { createQueryClient } from './app/query'
import { ToastProvider } from './components/ui/toast'
import { AuthScreen } from './screens/AuthScreen'
import { NodesScreen } from './screens/NodesScreen'
import { UsersScreen } from './screens/UsersScreen'

type TitleKey = 'overview' | 'boards' | 'traffic' | 'settings'
type SubKey = 'overviewSub' | 'boardsSub' | 'trafficSub' | 'settingsSub'

/** Заглушка экрана: маршруты ставятся раньше, чем сами экраны. */
function Placeholder({ titleKey, subKey }: { titleKey: TitleKey; subKey: SubKey }) {
  const t = useT()
  return (
    <section className="mx-auto flex max-w-6xl flex-col gap-1">
      <h1 className="text-xl font-medium">{t[titleKey]}</h1>
      <p className="text-sm text-dim">{t[subKey]}</p>
    </section>
  )
}

function Routed() {
  const { session } = useAuth()

  // Поток событий держим только за авторизованной сессией: без токена он всё
  // равно получит 401 и уйдёт в бесконечный цикл переподключений.
  useControlEvents(session !== null)

  if (!session) return <AuthScreen />

  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route index element={<Placeholder titleKey="overview" subKey="overviewSub" />} />
        <Route path="nodes" element={<NodesScreen />} />
        <Route path="users" element={<UsersScreen />} />
        <Route path="boards" element={<Placeholder titleKey="boards" subKey="boardsSub" />} />
        <Route path="traffic" element={<Placeholder titleKey="traffic" subKey="trafficSub" />} />
        <Route path="settings" element={<Placeholder titleKey="settings" subKey="settingsSub" />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}

export function App() {
  // Клиент создаётся один раз на монтирование: пересоздание в рендере сбрасывало
  // бы весь кэш на каждое обновление состояния.
  const [queryClient] = useState(createQueryClient)

  return (
    <QueryClientProvider client={queryClient}>
      <LanguageProvider>
        <ToastProvider>
          <AuthProvider>
            <BrowserRouter>
              <Routed />
            </BrowserRouter>
          </AuthProvider>
        </ToastProvider>
      </LanguageProvider>
    </QueryClientProvider>
  )
}
