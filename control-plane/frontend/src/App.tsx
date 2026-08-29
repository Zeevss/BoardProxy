import { QueryClientProvider } from '@tanstack/react-query'
import { useState } from 'react'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router'
import { useControlEvents } from './api/events'
import { AppShell } from './app/AppShell'
import { AuthProvider, useAuth } from './app/auth'
import { LanguageProvider } from './app/language'
import { createQueryClient } from './app/query'
import { ToastProvider } from './components/ui/toast'
import { AuthScreen } from './screens/AuthScreen'
import { BoardsScreen } from './screens/BoardsScreen'
import { NodesScreen } from './screens/NodesScreen'
import { OverviewScreen } from './screens/OverviewScreen'
import { SettingsScreen } from './screens/SettingsScreen'
import { TrafficScreen } from './screens/TrafficScreen'
import { UsersScreen } from './screens/UsersScreen'

function Routed() {
  const { session } = useAuth()

  // Поток событий держим только за авторизованной сессией: без токена он всё
  // равно получит 401 и уйдёт в бесконечный цикл переподключений.
  useControlEvents(session !== null)

  if (!session) return <AuthScreen />

  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route index element={<OverviewScreen />} />
        <Route path="nodes" element={<NodesScreen />} />
        <Route path="users" element={<UsersScreen />} />
        <Route path="boards" element={<BoardsScreen />} />
        <Route path="traffic" element={<TrafficScreen />} />
        <Route path="settings" element={<SettingsScreen />} />
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
