import { defineConfig } from 'vite'
import { tanstackStart } from '@tanstack/react-start/plugin/vite'
import viteReact from '@vitejs/plugin-react'

declare const process: { env: Record<string, string | undefined> }

const apiOrigin = process.env.IMS_API_ORIGIN ?? 'http://127.0.0.1:8080'

export default defineConfig({
  base: '/dev/',
  plugins: [
    tanstackStart({
      router: { basepath: '/dev' },
      spa: { enabled: true },
    }),
    viteReact(),
  ],
  server: {
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
    proxy: {
      '/dev/api': apiOrigin,
      '/api': apiOrigin,
      '/docs': apiOrigin,
      '/storage': apiOrigin,
      '/up': apiOrigin,
      // MCP connector (phase 22): the endpoint, its OAuth pages and the
      // discovery documents clients read first.
      '/mcp': apiOrigin,
      '/oauth': apiOrigin,
      '/.well-known': apiOrigin,
    },
  },
})
