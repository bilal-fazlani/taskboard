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
})
