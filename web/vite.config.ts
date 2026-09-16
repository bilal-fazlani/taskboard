/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      '/api': {
        // Defaults to the `make dev` backend so the dev UI never writes to the live board.
        target: `http://localhost:${process.env.TASKBOARD_API_PORT ?? '3011'}`,
        ws: true,
        // Do not set changeOrigin: true. The backend's cross-origin write
        // check (rejectCrossOriginWrites, internal/server/server.go)
        // compares the browser's Origin header against the Host the
        // request arrives with; rewriting Host to the backend's own port
        // would make it look cross-origin and 403 every write from the dev
        // UI.
        // Verified empirically: Vite's proxy (node-http-proxy) pipes the
        // /api/events SSE response through as it arrives, with no extra
        // buffering or compression here in dev mode, so `changed` events
        // reach the browser on the backend's own watch interval (~250ms).
        // No configure() hook is needed to disable buffering.
      },
    },
  },
  test: {
    // Unit tests run in Node without a DOM. `vitest run` starts no dev server, so
    // the proxy above is never used; tests mock the API rather than reach a server.
    environment: 'node',
    include: ['src/**/*.test.{ts,tsx}'],
  },
})
