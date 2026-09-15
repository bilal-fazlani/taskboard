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
