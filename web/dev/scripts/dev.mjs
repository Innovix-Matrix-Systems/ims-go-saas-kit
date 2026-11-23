import { spawn, spawnSync } from 'node:child_process'
import { existsSync } from 'node:fs'
import path from 'node:path'
import process from 'node:process'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const root = path.resolve(here, '../../..')
const webDir = path.join(root, 'web', 'dev')
const appDir = path.join(root, 'web', 'app')
const apiPort = process.env.IMS_API_PORT ?? '8081'
const webPort = process.env.IMS_WEB_PORT ?? '8080'
const appPort = process.env.IMS_APP_PORT ?? '8082'
const apiOrigin = `http://127.0.0.1:${apiPort}`
const apiEnv = { ...process.env, APP_LISTEN_ADDR: `:${apiPort}` }
const webEnv = { ...process.env, IMS_API_ORIGIN: apiOrigin }
const children = []
let shuttingDown = false

function start(name, command, args, cwd, env = process.env) {
  const child = spawn(command, args, {
    cwd,
    stdio: 'inherit',
    env,
  })
  children.push({ name, child })
  child.on('exit', (code, signal) => {
    if (shuttingDown) return
    console.log(`\n[dev] ${name} exited (${code ?? signal}) — stopping the other process.`)
    shutdown(typeof code === 'number' ? code : 1)
  })
  child.on('error', (error) => {
    console.error(`[dev] failed to start ${name}: ${error.message}`)
    shutdown(1)
  })
  return child
}

function shutdown(code = 0) {
  if (shuttingDown) return
  shuttingDown = true
  for (const { child } of children) {
    if (child.exitCode !== null) continue
    if (process.platform === 'win32') {
      spawnSync('taskkill', ['/pid', String(child.pid), '/T', '/F'], { stdio: 'ignore' })
    } else {
      try {
        child.kill('SIGTERM')
      } catch {
        /* already gone */
      }
    }
  }
  setTimeout(() => process.exit(code), 250)
}

process.on('SIGINT', () => shutdown(0))
process.on('SIGTERM', () => shutdown(0))

const isWindows = process.platform === 'win32'
const hasAir = spawnSync(isWindows ? 'where' : 'which', ['air'], { stdio: 'ignore' }).status === 0

console.log(`[dev] dashboard (Vite HMR): http://127.0.0.1:${webPort}/dev/`)
console.log(`[dev] API (air): ${apiOrigin} - /api, /dev/api, /storage and /docs are proxied through ${webPort}.`)
start(
  'web',
  process.execPath,
  [path.join(webDir, 'node_modules', 'vite', 'bin', 'vite.js'), 'dev', '--port', webPort],
  webDir,
  webEnv,
)

// The client app (web/app) runs next to the dashboard once its packages are
// installed (`npm --prefix web/app ci`).
const appVite = path.join(appDir, 'node_modules', 'vite', 'bin', 'vite.js')
if (existsSync(appVite)) {
  console.log(`[dev] client app (Vite HMR): http://127.0.0.1:${appPort}/`)
  start('app', process.execPath, [appVite, '--port', appPort, '--strictPort'], appDir, webEnv)
} else {
  console.log('[dev] client app skipped: run `npm --prefix web/app ci` to start it with the dashboard.')
}

if (hasAir) {
  start('api', isWindows ? 'air.exe' : 'air', [], root, apiEnv)
} else {
  console.log('[dev] air is not installed; starting the API with `go run` (no hot reload).')
  console.log('[dev] run `make tools` to enable API hot reload.')
  start('api', isWindows ? 'go.exe' : 'go', ['run', './cmd/app', 'serve'], root, apiEnv)
}
