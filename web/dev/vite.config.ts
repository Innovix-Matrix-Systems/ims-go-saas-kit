import { defineConfig } from 'vite'
import { tanstackRouter } from '@tanstack/router-plugin/vite'
import viteReact from '@vitejs/plugin-react'

declare const process: { env: Record<string, string | undefined> }

const apiOrigin = process.env.IMS_API_ORIGIN ?? 'http://127.0.0.1:8080'

export default defineConfig({
  base: '/dev/',
  plugins: [
    tanstackRouter({ target: 'react', autoCodeSplitting: true }),
    viteReact(),
  ],
  build: {
    // dist/client is what web/dev/embed.go embeds; its .gitkeep keeps the
    // directory (and so the go:embed pattern) valid before the first build.
    outDir: 'dist/client',
    emptyOutDir: false,
    sourcemap: false,
  },
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
      // MCP connector: the endpoint, its OAuth pages and the
      // discovery documents clients read first.
      '/mcp': apiOrigin,
      '/oauth': apiOrigin,
      '/.well-known': apiOrigin,
    },
  },
})
