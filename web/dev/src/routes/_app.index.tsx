import { useQuery } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { api } from '../api/client'
import type { CursorPage, DevAction, DevJob, DevOverview, DevPackagesReport, DevTrafficMinute, SystemInfo } from '../api/types'
import { poolsSummary, useDatabasePools } from '../components/DatabasePools'
import { Sparkline } from '../components/Sparkline'
import { TrafficChart } from '../components/TrafficChart'
import { formatUptime } from '../components/SystemTab'
import {
  Badge,
  Button,
  Card,
  Cluster,
  EmptyState,
  ErrorBox,
  Grid,
  LoadingBlock,
  PageHeader,
  Stack,
  StatTile,
  StatusDot,
  type StatTone,
} from '../components/ui'
import { formatDuration, relativeTime } from '../format'

export const Route = createFileRoute('/_app/')({
  component: OverviewPage,
})

const REFRESH_MS = 15_000

// "Is anything wrong right now?" first: six status tiles that stay grey
// while all is well, the last hour of traffic, every health check next to
// recent activity, errors and failed jobs when there are any, then the
// environment and the packages it runs on.
function OverviewPage() {
  const [live, setLive] = useState(true)
  const overview = useQuery({
    queryKey: ['overview'],
    queryFn: ({ signal }) => api<DevOverview>('/dev/api/overview', { signal }),
    refetchInterval: live ? REFRESH_MS : false,
  })
  const failedJobs = useQuery({
    queryKey: ['overview', 'failed-jobs'],
    queryFn: ({ signal }) =>
      api<CursorPage<DevJob>>('/dev/api/queue/jobs', { query: { state: 'discarded', limit: 5 }, signal, unwrap: false }),
    enabled: Boolean(overview.data?.queue.available && overview.data.queue.failed > 0),
    refetchInterval: live ? REFRESH_MS : false,
    retry: false,
  })
  const actions = useQuery({
    queryKey: ['overview', 'actions'],
    queryFn: ({ signal }) => api<CursorPage<DevAction>>('/dev/api/actions', { query: { limit: 8 }, signal, unwrap: false }),
    refetchInterval: live ? REFRESH_MS : false,
  })
  const info = useQuery({
    queryKey: ['system-info'],
    queryFn: ({ signal }) => api<SystemInfo>('/api/v1/system/info', { signal }),
    staleTime: 5 * 60_000,
    retry: false,
  })
  const packages = useQuery({
    queryKey: ['inspector', 'packages'],
    queryFn: ({ signal }) => api<DevPackagesReport>('/dev/api/inspector/packages', { signal }),
    staleTime: 10 * 60_000,
    retry: false,
  })

  const data = overview.data
  return (
    <>
      <PageHeader
        title="Overview"
        description="Whether anything needs you right now, and what the API did in the last hour."
        meta={<RefreshMeta updatedAt={overview.dataUpdatedAt} live={live} onToggle={() => setLive((value) => !value)} />}
      />
      {overview.isLoading ? (
        <LoadingBlock label="Loading the overview…" />
      ) : overview.isError || !data ? (
        <ErrorBox
          message={overview.error instanceof Error ? overview.error.message : 'Unable to load the overview.'}
          onRetry={() => void overview.refetch()}
        />
      ) : (
        <Stack gap={5}>
          <StatusTiles data={data} />
          <TrafficCard traffic={data.traffic} />
          <div className="overview-split">
            <HealthChecks health={data.health} />
            <RecentActivity actions={actions.data?.data ?? []} loading={actions.isLoading} />
          </div>
          <ErrorsAndFailedJobs data={data} failedJobs={failedJobs.data?.data ?? []} />
          <Grid min="lg" gap={4}>
            <EnvironmentCard info={info.data} data={data} />
            <PackagesCard report={packages.data} loading={packages.isLoading} />
          </Grid>
        </Stack>
      )}
    </>
  )
}

function RefreshMeta({ updatedAt, live, onToggle }: { updatedAt: number; live: boolean; onToggle: () => void }) {
  // Re-render every few seconds so "updated 12 s ago" stays true.
  const [, tick] = useState(0)
  useEffect(() => {
    const timer = window.setInterval(() => tick((value) => value + 1), 5_000)
    return () => window.clearInterval(timer)
  }, [])
  return (
    <>
      <span>{updatedAt ? `Updated ${relativeTime(updatedAt)}` : 'Loading…'}</span>
      <span>·</span>
      <span>{live ? `refreshes every ${REFRESH_MS / 1000} s` : 'refresh paused'}</span>
      <Button size="sm" variant="ghost" icon={live ? 'pause' : 'play'} onClick={onToggle}>
        {live ? 'Pause' : 'Resume'}
      </Button>
    </>
  )
}

function StatusTiles({ data }: { data: DevOverview }) {
  const { health, queue, exceptions, backup, drift } = data
  // Migrations has its own tile.
  const problems = health.problems.filter((check) => check.name !== 'Migrations')
  const failing = problems.filter((check) => check.status !== 'warning' && !check.advisory).length
  const healthTone: StatTone | undefined = failing > 0 ? 'danger' : problems.length > 0 ? 'warn' : undefined
  const backupTone: StatTone | undefined = backup.status === 'failed' ? 'danger' : backup.status === 'warning' ? 'warn' : undefined
  const missing = drift.settings + drift.flags

  return (
    <Grid min="sm" gap={3} className="overview-tiles">
      <StatTile
        label="Health"
        value={failing > 0 ? `${failing} failing` : problems.length > 0 ? `${problems.length} to check` : 'All OK'}
        tone={healthTone}
        hint={`${health.checks.length} checks · ${relativeTime(health.checked_at)}`}
        to="/inspector"
        search={{ tab: 'system' }}
      />
      <StatTile
        label="Migrations"
        value={migrationsValue(health.migrations)}
        tone={health.migrations.status === 'ok' ? undefined : health.migrations.pending > 0 ? 'warn' : 'danger'}
        hint={
          health.migrations.status === 'ok'
            ? 'database and River schema'
            : health.migrations.pending > 0
              ? 'run ims migrate'
              : health.migrations.summary
        }
        to="/inspector"
        search={{ tab: 'system' }}
      />
      <StatTile
        label="Queue"
        value={queue.available ? queue.backlog : 'Sync driver'}
        tone={queue.failed > 0 ? 'danger' : undefined}
        hint={
          queue.available
            ? `waiting or running · ${queue.failed} failed${queue.paused ? ` · ${queue.paused} paused` : ''}`
            : 'jobs run inline; nothing to inspect'
        }
        to="/queue"
        search={queue.failed > 0 ? { state: 'discarded' } : undefined}
      />
      <StatTile
        label="Errors, last 24 h"
        value={exceptions.last_24h}
        tone={exceptions.last_24h > 0 ? 'danger' : undefined}
        hint={exceptions.last_24h > 0 ? `${exceptions.groups.length} kind${exceptions.groups.length === 1 ? '' : 's'}` : 'no exceptions captured'}
        to="/exceptions"
      />
      <StatTile
        label="Last backup"
        value={!backup.enabled ? 'Off' : backup.last_success_at ? relativeTime(backup.last_success_at) : 'Never'}
        tone={backup.enabled ? backupTone : undefined}
        hint={backup.enabled ? backup.summary : 'BACKUP_ENABLED is false'}
        to="/backups"
      />
      <StatTile
        label="Settings and flags"
        value={missing === 0 ? 'In sync' : `${missing} missing`}
        tone={missing > 0 ? 'warn' : undefined}
        hint={missing === 0 ? 'every catalog key is stored' : `${drift.settings} settings, ${drift.flags} flags not stored: sync them`}
        to={drift.settings === 0 && drift.flags > 0 ? '/flags' : '/settings'}
      />
    </Grid>
  )
}

function migrationsValue(migrations: DevOverview['health']['migrations']): string {
  if (migrations.pending > 0) return `${migrations.pending} pending`
  if (migrations.status === 'ok') return 'Up to date'
  return migrations.status ? 'Check failed' : 'Unknown'
}

/** Every minute of the window, oldest first; minutes without traffic count 0. */
function byMinute(minutes: DevTrafficMinute[], windowMinutes: number): (DevTrafficMinute | null)[] {
  const found = new Map(minutes.map((minute) => [Math.floor(Date.parse(minute.minute) / 60_000), minute]))
  const now = Math.floor(Date.now() / 60_000)
  return Array.from({ length: windowMinutes }, (_, index) => found.get(now - windowMinutes + 1 + index) ?? null)
}

function TrafficCard({ traffic }: { traffic: DevOverview['traffic'] }) {
  if (!traffic.capturing) {
    return (
      <Card title="Traffic, last hour" flush>
        <EmptyState
          icon="activity"
          title="Request capture is off"
          description="Set DEV_DASHBOARD_ENABLED=true with http in DEV_DASHBOARD_CAPTURE to see requests, errors and latency here."
        />
      </Card>
    )
  }
  const slots = byMinute(traffic.minutes, traffic.window_minutes)
  const stamp = (index: number) => new Date((Math.floor(Date.now() / 60_000) - traffic.window_minutes + 1 + index) * 60_000).toISOString()
  const errorRate = slots.map((slot, index) => ({
    at: stamp(index),
    value: slot && slot.requests > 0 ? ((slot.client_errors + slot.server_errors) / slot.requests) * 100 : 0,
  }))
  const latency = slots.flatMap((slot, index) => (slot ? [{ at: stamp(index), value: slot.p95_ms }] : []))
  const { totals } = traffic
  const errors = totals.client_errors + totals.server_errors
  const percent = (value: number) => `${value.toFixed(value > 0 && value < 10 ? 1 : 0)}%`
  const sampled = traffic.sample_rate < 1 ? `${Math.round(traffic.sample_rate * 100)}% of requests sampled, so counts are a trend` : 'every request captured'

  return (
    <Card
      title="Traffic, last hour"
      description={sampled}
      actions={
        <Link to="/requests" search={{ request_id: undefined }} className="btn ghost sm">
          Requests
        </Link>
      }
    >
      <div className="traffic-layout">
        <TrafficChart slots={slots} end={new Date(Math.floor(Date.now() / 60_000) * 60_000)} />
        <div className="traffic-side">
          <div className="traffic-metric">
            <div className="stat-label">Requests</div>
            <div className="traffic-value num">
              {totals.requests.toLocaleString()}
              <span className="traffic-sub">{(totals.requests / traffic.window_minutes).toFixed(1)} a minute</span>
            </div>
          </div>
          <div className="traffic-metric">
            <div className="stat-label">Error rate (4xx and 5xx)</div>
            <div className="traffic-value num">
              {totals.requests > 0 ? percent((errors / totals.requests) * 100) : '—'}
              <span className="traffic-sub">
                {totals.client_errors} 4xx · {totals.server_errors} 5xx
              </span>
            </div>
            <Sparkline points={errorRate} label="Error rate per minute" format={percent} />
          </div>
          <div className="traffic-metric">
            <div className="stat-label">Latency, p95</div>
            <div className="traffic-value num">{totals.requests > 0 ? formatDuration(totals.p95_ms) : '—'}</div>
            <Sparkline points={latency} label="p95 latency per minute" format={(value) => formatDuration(value)} />
          </div>
        </div>
      </div>
    </Card>
  )
}

/** Failed jobs and the newest kinds of error; not shown while there are none. */
function ErrorsAndFailedJobs({ data, failedJobs }: { data: DevOverview; failedJobs: DevJob[] }) {
  const groups = data.exceptions.groups
  if (groups.length === 0 && failedJobs.length === 0) return null
  return (
    <Card title="Errors and failed jobs" description="The newest kinds of error today and jobs River gave up on." flush>
      <ul className="attention-list">
        {failedJobs.map((job) => (
          <li key={job.id}>
            <Link to="/jobs/$id" params={{ id: String(job.id) }} className="attention-row">
              <StatusDot tone="danger" />
              <span className="attention-main">
                <span className="attention-title mono">{job.kind}</span>
                <span className="attention-sub">
                  {job.errors?.at(-1)?.error ?? `Failed job #${job.id} on ${job.queue}`}
                  {job.finalized_at ? ` · ${relativeTime(job.finalized_at)}` : ''}
                </span>
              </span>
              <Badge tone="danger">failed</Badge>
            </Link>
          </li>
        ))}
        {groups.map((group) => (
          <li key={group.class}>
            <Link to="/exceptions/$id" params={{ id: String(group.latest_id) }} className="attention-row">
              <StatusDot tone="danger" />
              <span className="attention-main">
                <span className="attention-title mono">{group.class}</span>
                <span className="attention-sub">
                  {group.latest_message} · last {relativeTime(group.last_seen)}
                </span>
              </span>
              <Badge tone="neutral">×{group.occurrences}</Badge>
            </Link>
          </li>
        ))}
      </ul>
    </Card>
  )
}

function RecentActivity({ actions, loading }: { actions: DevAction[]; loading: boolean }) {
  return (
    <Card
      title="Recent activity"
      description="What developers did here, newest first."
      flush
      actions={
        <Link to="/actions" className="btn ghost sm">
          Audit log
        </Link>
      }
    >
      {loading ? (
        <LoadingBlock />
      ) : actions.length === 0 ? (
        <EmptyState icon="actions" title="No activity yet" description="Every change made in the dashboard is recorded here." />
      ) : (
        <ul className="attention-list">
          {actions.map((action) => (
            <li key={action.id}>
              <Link to="/actions" search={{ action: action.action }} className="attention-row">
                <StatusDot tone={action.status === 'ok' ? 'ok' : 'danger'} title={action.status} />
                <span className="attention-main">
                  <span className="attention-title mono">{action.action}</span>
                  <span className="attention-sub">
                    {[action.actor_name, action.target].filter(Boolean).join(' · ') || '—'}
                  </span>
                </span>
                <Cluster gap={2}>
                  {action.status !== 'ok' ? <Badge tone="danger">failed</Badge> : null}
                  <span className="small dim nowrap">{relativeTime(action.created_at)}</span>
                </Cluster>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </Card>
  )
}

/** Every health check, the ones that need a look first. */
function HealthChecks({ health }: { health: DevOverview['health'] }) {
  // Pool counts are dashboard-only (the public health report leaves them out).
  const pools = useDatabasePools()
  return (
    <Card
      title="Health checks"
      description={`Last run ${relativeTime(health.checked_at)}`}
      flush
      actions={
        <Link to="/inspector" search={{ tab: 'system' }} className="btn ghost sm">
          Details
        </Link>
      }
    >
      <ul className="attention-list">
        {health.checks.map((check) => {
          const tone = check.status === 'ok' ? 'ok' : check.status === 'warning' || check.advisory ? 'warn' : 'danger'
          return (
            <li key={check.name} className="attention-row check-row">
              <StatusDot tone={tone} />
              <span className="attention-main">
                <span className="attention-title">{check.label}</span>
                {check.status !== 'ok' || check.summary ? <span className="attention-sub">{check.summary}</span> : null}
              </span>
              {check.name === 'Database' && pools.data && pools.data.pools.length > 0 ? (
                <span className="small muted mono" title="Connections in use / pool maximum">
                  {poolsSummary(pools.data.pools)}
                </span>
              ) : null}
              {check.advisory && check.status !== 'ok' ? <Badge tone="warn">advisory</Badge> : null}
              <Badge tone={tone === 'ok' ? 'neutral' : tone} mono>
                {check.status}
              </Badge>
            </li>
          )
        })}
      </ul>
    </Card>
  )
}

/** The running instance: what, where, since when, and the drivers in use. */
function EnvironmentCard({ info, data }: { info?: SystemInfo; data: DevOverview }) {
  return (
    <Card title="Environment" flush>
      {info ? (
        <dl className="kv detail-kv">
          <dt>Application</dt>
          <dd>{info.name}</dd>
          <dt>Environment</dt>
          <dd>{info.environment}</dd>
          <dt>Version</dt>
          <dd>{info.version}</dd>
          {info.go_version ? (
            <>
              <dt>Go</dt>
              <dd>{info.go_version}</dd>
            </>
          ) : null}
          <dt>Timezone</dt>
          <dd>{info.timezone}</dd>
          {info.uptime_seconds !== undefined ? (
            <>
              <dt>Up for</dt>
              <dd>{formatUptime(info.uptime_seconds)}</dd>
            </>
          ) : null}
          <dt>Queue</dt>
          <dd>{data.queue.available ? `River · ${data.queue.queues} queues` : 'sync (jobs run inline)'}</dd>
          <dt>Capture</dt>
          <dd>{data.traffic.capturing ? `${Math.round(data.traffic.sample_rate * 100)}% of requests` : 'off'}</dd>
          <dt>Backups</dt>
          <dd>{data.backup.enabled ? 'on' : 'off'}</dd>
        </dl>
      ) : (
        <LoadingBlock />
      )}
    </Card>
  )
}

const PACKAGES_SHOWN = 10

/** The direct Go dependencies with their versions. */
function PackagesCard({ report, loading }: { report?: DevPackagesReport; loading: boolean }) {
  const direct = (report?.packages ?? []).filter((item) => item.runtime === 'go' && item.kind === 'require')
  return (
    <Card
      title="Packages"
      description={report ? `${report.module} · ${report.go_version}` : undefined}
      flush
      actions={
        <Link to="/inspector" search={{ tab: 'packages' }} className="btn ghost sm">
          All {report ? report.packages.length : ''}
        </Link>
      }
    >
      {loading ? (
        <LoadingBlock />
      ) : direct.length === 0 ? (
        <EmptyState icon="package" title="No packages listed" />
      ) : (
        <ul className="attention-list">
          {direct.slice(0, PACKAGES_SHOWN).map((item) => (
            <li key={item.name} className="attention-row package-row">
              <span className="mono attention-main truncate">{item.name}</span>
              <Badge mono>{item.version}</Badge>
            </li>
          ))}
        </ul>
      )}
    </Card>
  )
}

