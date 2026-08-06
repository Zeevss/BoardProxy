import path from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// В production SPA и API обслуживает standalone Go gateway. Для разработки
// запустите gateway отдельно и укажите его адрес через VITE_API_TARGET.
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
      },
    },
  },
});
