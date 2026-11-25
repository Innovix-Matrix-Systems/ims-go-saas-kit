import { useQuery } from '@tanstack/react-query'
import { api } from '../api/client'
import type { HealthCheck, HealthResults, SystemInfo } from '../api/types'
import { relativeTime } from '../format'
import { DatabasePoolsCard } from './DatabasePools'
import { MaintenanceCard } from './Maintenance'
import { Badge, Button, Card, EmptyState, ErrorBox, JsonView, LoadingBlock, type BadgeTone } from './ui'

type HealthTone = Extract<BadgeTone, 'ok' | 'warn' | 'danger'>

function healthTone(status: HealthCheck['status']): HealthTone {
  if (status === 'ok') return 'ok'
  if (status === 'warning') return 'warn'
  return 'danger'
}

/** 75s → "1m 15s", 93784s → "1d 2h 3m". */
export function formatUptime(seconds: number): string {
  const total = Math.max(0, Math.floor(seconds))
  const parts: [number, string][] = [
    [Math.floor(total / 86400), 'd'],
    [Math.floor((total % 86400) / 3600), 'h'],
    [Math.floor((total % 3600) / 60), 'm'],
    [total % 60, 's'],
  ]
  const first = parts.findIndex(([value]) => value > 0)
  if (first < 0) return '0s'
  return parts
    .slice(first, first + 3)
    .filter(([value]) => value > 0)
    .map(([value, unit]) => `${value}${unit}`)
    .join(' ')
}

async function fetchHealth(): Promise<HealthResults> {
  const response = await fetch('/api/health?fresh=1', { headers: { Accept: 'application/json' } })
  if (!response.ok) throw new Error(`Health check failed (${response.status})`)
  return (await response.json()) as HealthResults
}

/** The runtime identity as label/value pairs (Inspector → System, Overview). */
export function RuntimePairs({ info }: { info: SystemInfo }) {
  return (
    <dl className="runtime-pairs">
      <div><dt>App</dt><dd>{info.name}</dd></div>
      <div><dt>Env</dt><dd>{info.environment}</dd></div>
      <div><dt>Version</dt><dd>{info.version}</dd></div>
      {info.go_version ? <div><dt>Go</dt><dd>{info.go_version}</dd></div> : null}
      <div><dt>Timezone</dt><dd>{info.timezone}</dd></div>
      {info.uptime_seconds !== undefined ? (
        <div><dt>Uptime</dt><dd>{formatUptime(info.uptime_seconds)}</dd></div>
      ) : null}
    </dl>
  )
}

/** Inspector → System: runtime identity and the live health registry. */
export function SystemTab({ needle }: { needle: string }) {
  const health = useQuery({ queryKey: ['health'], queryFn: fetchHealth, refetchInterval: 30_000 })
  const info = useQuery({
    queryKey: ['system-info'],
    queryFn: ({ signal }) => api<SystemInfo>('/api/v1/system/info', { signal }),
    retry: false,
  })

  const checks = health.data?.checkResults ?? []
  const counts = { ok: 0, warning: 0, failed: 0 }
  for (const check of checks) {
    if (check.status === 'ok') counts.ok++
    else if (check.status === 'warning') counts.warning++
    else counts.failed++
  }
  const visible = checks.filter(
    (check) =>
      !needle ||
      [check.name, check.label, check.status, check.shortSummary].some((value) => value?.toLowerCase().includes(needle)),
  )
  const overall: HealthTone = counts.failed > 0 ? 'danger' : counts.warning > 0 ? 'warn' : 'ok'

  return (
    <>
      <MaintenanceCard />
      <Card tight className="system-strip">
        {info.data ? (
          <RuntimePairs info={info.data} />
        ) : (
          <span className="small dim">{info.isError ? 'Runtime info unavailable.' : 'Loading runtime…'}</span>
        )}
        <span className="spacer" />
        {health.data ? (
          <div className="flex health-summary">
            <Badge tone={overall}>{overall === 'ok' ? 'healthy' : overall === 'warn' ? 'degraded' : 'failing'}</Badge>
            <span className="small muted">
              {counts.ok} ok · {counts.warning} warning · {counts.failed} failed
            </span>
            <span className="small dim">checked {relativeTime(health.data.finishedAt * 1000)}</span>
          </div>
        ) : null}
        <Button icon="refresh" onClick={() => void health.refetch()} disabled={health.isFetching}>
          {health.isFetching ? 'Checking…' : 'Run checks'}
        </Button>
      </Card>

      {health.isLoading ? (
        <LoadingBlock label="Running health checks…" />
      ) : health.isError ? (
        <ErrorBox message={health.error instanceof Error ? health.error.message : 'Unable to run health checks.'} />
      ) : visible.length === 0 ? (
        <EmptyState title={checks.length === 0 ? 'No checks registered' : 'No checks match'} icon="system" />
      ) : (
        <div className="health-grid mt-16">
          {visible.map((check) => (
            <div key={check.name} className={`health-card ${healthTone(check.status)}`}>
              <div className="flex">
                <span className="health-name">{check.label || check.name}</span>
                <span className="spacer" />
                <Badge tone={healthTone(check.status)}>{check.status}</Badge>
              </div>
              <div className="small muted health-summary-line" title={check.shortSummary}>
                {check.shortSummary || '—'}
              </div>
              {check.notificationMessage ? <div className="small dim">{check.notificationMessage}</div> : null}
              {check.meta && Object.keys(check.meta).length > 0 ? (
                <details>
                  <summary className="small dim">Details</summary>
                  <div className="mt-8">
                    <JsonView value={check.meta} label="Metadata" maxHeight={200} />
                  </div>
                </details>
              ) : null}
            </div>
          ))}
        </div>
      )}
      <DatabasePoolsCard />
    </>
  )
}
