import { defineConfig } from 'vite'
import { tanstackRouter } from '@tanstack/router-plugin/vite'
import viteReact from '@vitejs/plugin-react'

declare const process: { env: Record<string, string | undefined> }

// In development the API runs separately (make dev); everything the app
// calls on the server is proxied to it so the browser sees one origin.
const apiOrigin = process.env.IMS_API_ORIGIN ?? 'http://127.0.0.1:8081'

export default defineConfig({
  base: '/',
  plugins: [
    tanstackRouter({ target: 'react', autoCodeSplitting: true }),
    viteReact(),
  ],
  build: {
    outDir: 'dist',
    emptyOutDir: false,
    sourcemap: false,
  },
  server: {
    host: '127.0.0.1',
    port: 8082,
    strictPort: true,
    proxy: {
      '/api': apiOrigin,
      '/docs': apiOrigin,
      '/storage': apiOrigin,
      '/up': apiOrigin,
    },
  },
})
