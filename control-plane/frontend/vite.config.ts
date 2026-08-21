import { defineConfig } from 'vitest/config'
import type { ProxyOptions } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath } from 'node:url'

const HUB = process.env.CONTROL_API ?? 'http://localhost:8080'

/**
 * Убирает `Origin` с проксируемых запросов.
 *
 * В продакшне панель и API отдаёт один хаб, то есть запросы same-origin и CORS
 * не участвует вовсе. Прокси же подменяет только Host, оставляя Origin от
 * дев-сервера, — хаб видит cross-origin, не находит его в пустом списке
 * разрешённых и отвечает 403 на каждый POST. GET при этом проходит: браузер не
 * шлёт Origin на простых запросах, поэтому расхождение всплывает не сразу.
 */
const sameOrigin: NonNullable<ProxyOptions['configure']> = (proxy) => {
  proxy.on('proxyReq', (proxyReq) => proxyReq.removeHeader('origin'))
}

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  build: { sourcemap: true },
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    globals: true,
  },
  server: {
    // Явный IPv4: по умолчанию Vite поднимается только на [::1], и всё, что
    // резолвит localhost в 127.0.0.1, до него не достучится.
    host: '127.0.0.1',
    port: 5174,
    // Панель ходит по относительным путям, поэтому в разработке хватает прокси —
    // CORS на хабе включать не нужно.
    //
    // Адрес хаба вынесен в переменную: на машине разработчика рядом вполне может
    // крутиться собственный стек, занявший 8080.
    proxy: {
      '/api': { target: HUB, changeOrigin: true, configure: sameOrigin },
      '/actuator': { target: HUB, changeOrigin: true, configure: sameOrigin },
    },
  },
})
