import path from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Панель обслуживается за обратным прокси Caddy, который проксирует /api/* в
// core. В dev-режиме проксируем то же самое на локальный web-api сервера, чтобы
// `npm run dev` работал без Caddy (адрес переопределяется через VITE_API_TARGET).
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { "@": path.resolve(__dirname, "./src") },
  },
  server: {
    proxy: {
      "/api": {
        target: process.env.VITE_API_TARGET ?? "http://127.0.0.1:8080",
        changeOrigin: true,
        rewrite: (p) => p.replace(/^\/api/, ""),
      },
    },
  },
});
