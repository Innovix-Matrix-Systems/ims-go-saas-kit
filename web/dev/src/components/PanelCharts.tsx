import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { api } from '../api/client'
import type { DevDeliveryStats, DevExceptionStats, DevFileStats, DevQueue, DevRequestStats, DevSessionStats } from '../api/types'
import { formatBytes, formatDuration } from '../format'
import { DonutChart, HBarChart, TimeBars, type BarPart } from './charts'
import { Card, ErrorBox, LoadingBlock } from './ui'

// The charts each panel shows above its list. Each reads its own stats
// endpoint (or the data the panel already has), so a panel's list and its
// charts load independently.

/** A small segmented control for a chart window. */
function WindowPicker<T extends number>({
  value,
  options,
  onChange,
  label,
}: {
  value: T
  options: { value: T; label: string }[]
  onChange: (value: T) => void
  label: string
}) {
  return (
    <div className="segmented" role="group" aria-label={label}>
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          className={value === option.value ? 'active' : ''}
          aria-pressed={value === option.value}
          onClick={() => onChange(option.value)}
        >
          {option.label}
        </button>
      ))}
    </div>
  )
}

function errorText(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback
}

const hourLabel = (at: string) => new Date(at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
const dayLabel = (at: string) => new Date(at).toLocaleDateString([], { month: 'short', day: 'numeric', timeZone: 'UTC' })
const dayTitle = (at: string) => new Date(at).toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric', timeZone: 'UTC' })

// ── Requests ────────────────────────────────────────────

const requestWindows = [
  { value: 1, label: '1h' },
  { value: 24, label: '24h' },
  { value: 168, label: '7d' },
]

const routeParts: BarPart[] = [
  { key: 'ok', label: 'Succeeded', tone: 'accent' },
  { key: 'errors', label: '4xx and 5xx', tone: 'danger' },
]

/** Requests: status mix, and the busiest or slowest routes, for a window. */
export function RequestCharts({ live }: { live: boolean }) {
  const [hours, setHours] = useState(1)
  const [view, setView] = useState<'busiest' | 'slowest'>('busiest')
  const stats = useQuery({
    queryKey: ['requests', 'stats', hours],
    queryFn: ({ signal }) => api<DevRequestStats>('/dev/api/requests/stats', { query: { hours }, signal }),
    refetchInterval: live ? 5_000 : false,
  })
  const data = stats.data
  const picker = <WindowPicker value={hours} options={requestWindows} onChange={setHours} label="Chart window" />

  if (stats.isError) return <ErrorBox message={errorText(stats.error, 'Unable to load request charts.')} onRetry={() => void stats.refetch()} />
  return (
    <div className="chart-row narrow-first mb-16">
      <Card title="Status" description="Captured requests by status class" actions={picker}>
        {!data ? (
          <LoadingBlock />
        ) : (
          <DonutChart
            label="Requests by status class"
            totalLabel="requests"
            slices={[
              { key: '2xx', label: '1xx and 2xx', value: data.status.success, tone: 'accent' },
              { key: '3xx', label: '3xx', value: data.status.redirect, tone: 'series-3' },
              { key: '4xx', label: '4xx', value: data.status.client_errors, tone: 'warn' },
              { key: '5xx', label: '5xx', value: data.status.server_errors, tone: 'danger' },
            ]}
          />
        )}
      </Card>
      <Card
        title="Routes"
        description={view === 'busiest' ? 'Requests per route, errors stacked on top' : 'p95 duration per route'}
        actions={
          <WindowPicker
            value={view === 'busiest' ? 0 : 1}
            options={[
              { value: 0, label: 'Busiest' },
              { value: 1, label: 'Slowest' },
            ]}
            onChange={(value) => setView(value === 0 ? 'busiest' : 'slowest')}
            label="Routes"
          />
        }
      >
        {!data ? (
          <LoadingBlock />
        ) : view === 'busiest' ? (
          <HBarChart
            label="Busiest routes"
            parts={routeParts}
            rows={data.busiest.map((route) => ({
              key: `${route.method} ${route.route}`,
              label: `${route.method} ${route.route}`,
              values: { ok: route.requests - route.errors, errors: route.errors },
            }))}
          />
        ) : (
          <HBarChart
            label="Slowest routes by p95"
            parts={[{ key: 'p95', label: 'p95', tone: 'warn' }]}
            format={(value) => formatDuration(value)}
            rows={data.slowest.map((route) => ({
              key: `${route.method} ${route.route}`,
              label: `${route.method} ${route.route}`,
              title: `${route.method} ${route.route} · ${route.requests} requests`,
              values: { p95: route.p95_ms },
            }))}
          />
        )}
      </Card>
    </div>
  )
}

// ── Exceptions ──────────────────────────────────────────

/** Exceptions: per hour over the last day, and the classes behind them. */
export function ExceptionCharts() {
  const stats = useQuery({
    queryKey: ['exceptions', 'stats'],
    queryFn: ({ signal }) => api<DevExceptionStats>('/dev/api/exceptions/stats', { signal }),
    refetchInterval: 30_000,
  })
  const data = stats.data
  if (stats.isError) return <ErrorBox message={errorText(stats.error, 'Unable to load exception charts.')} onRetry={() => void stats.refetch()} />
  return (
    <div className="chart-row wide-first mb-16">
      <Card title="Last 24 hours" description="Exceptions per hour">
        {!data ? (
          <LoadingBlock />
        ) : (
          <TimeBars
            label="Exceptions per hour, last 24 hours"
            parts={[{ key: 'occurrences', label: 'Exceptions', tone: 'danger' }]}
            points={data.hours.map((hour) => ({ at: hour.hour, values: { occurrences: hour.occurrences } }))}
            tick={hourLabel}
            title={hourLabel}
          />
        )}
      </Card>
      <Card title="By class" description="The same 24 hours">
        {!data ? (
          <LoadingBlock />
        ) : (
          <DonutChart
            label="Exceptions by class, last 24 hours"
            totalLabel="exceptions"
            slices={data.classes.map((group) => ({ key: group.class, label: group.class, value: group.occurrences }))}
          />
        )}
      </Card>
    </div>
  )
}

// ── Deliveries ──────────────────────────────────────────

const deliveryWindows = [
  { value: 7, label: '7d' },
  { value: 14, label: '14d' },
  { value: 30, label: '30d' },
]

const deliveryParts: BarPart[] = [
  { key: 'sent', label: 'Sent', tone: 'ok' },
  { key: 'failed', label: 'Failed', tone: 'danger' },
  { key: 'queued', label: 'Queued', tone: 'series-1' },
  { key: 'skipped', label: 'Skipped', tone: 'muted' },
]

/** Deliveries: outcomes per day, and the channels they went through. */
export function DeliveryCharts() {
  const [days, setDays] = useState(7)
  const stats = useQuery({
    queryKey: ['deliveries', 'stats', days],
    queryFn: ({ signal }) => api<DevDeliveryStats>('/dev/api/deliveries/stats', { query: { days }, signal }),
  })
  const data = stats.data
  if (stats.isError) return <ErrorBox message={errorText(stats.error, 'Unable to load delivery charts.')} onRetry={() => void stats.refetch()} />
  const total = (row: DevDeliveryStats['channels'][number]) => row.sent + row.failed + row.queued + row.skipped
  return (
    <div className="chart-row wide-first mb-16">
      <Card
        title="Per day"
        description="Deliveries by outcome (UTC days)"
        actions={<WindowPicker value={days} options={deliveryWindows} onChange={setDays} label="Chart window" />}
      >
        {!data ? (
          <LoadingBlock />
        ) : (
          <TimeBars
            label={`Deliveries per day, last ${days} days`}
            parts={deliveryParts}
            points={data.days.map((day) => ({
              at: day.day ?? '',
              values: { sent: day.sent, failed: day.failed, queued: day.queued, skipped: day.skipped },
            }))}
            tick={dayLabel}
            title={dayTitle}
          />
        )}
      </Card>
      <Card title="By channel" description={`The same ${days} days`}>
        {!data ? (
          <LoadingBlock />
        ) : (
          <DonutChart
            label="Deliveries by channel"
            totalLabel="deliveries"
            slices={data.channels.map((row) => ({ key: row.channel ?? '', label: row.channel ?? '', value: total(row) }))}
          />
        )}
      </Card>
    </div>
  )
}

// ── Queue ───────────────────────────────────────────────

/** Queue: every queue's jobs, waiting or running vs failed vs done. */
export function QueueChart({ queues }: { queues: DevQueue[] }) {
  return (
    <Card title="Jobs per queue" description="Waiting or running, failed, and the rest still kept (completed, cancelled)">
      <HBarChart
        label="Jobs per queue"
        labelWidth={120}
        parts={[
          { key: 'active', label: 'Waiting or running', tone: 'accent' },
          { key: 'failed', label: 'Failed', tone: 'danger' },
          { key: 'rest', label: 'Done', tone: 'muted' },
        ]}
        rows={queues.map((queue) => ({
          key: queue.name,
          label: queue.name,
          values: { active: queue.active, failed: queue.failed, rest: Math.max(0, queue.total - queue.active - queue.failed) },
        }))}
      />
    </Card>
  )
}

// ── Files ───────────────────────────────────────────────

const typeLabels: Record<string, string> = { image: 'Images', video: 'Videos', document: 'Documents', other: 'Other' }

/** Files: storage by type and by disk. */
export function FileCharts({ stats }: { stats: DevFileStats }) {
  const [measure, setMeasure] = useState<'size' | 'files'>('size')
  const format = measure === 'size' ? (value: number) => formatBytes(value) : undefined
  const picker = (
    <WindowPicker
      value={measure === 'size' ? 1 : 0}
      options={[
        { value: 1, label: 'Size' },
        { value: 0, label: 'Count' },
      ]}
      onChange={(value) => setMeasure(value === 1 ? 'size' : 'files')}
      label="Measure"
    />
  )
  return (
    <div className="chart-row mt-16">
      <Card title="By type" description={measure === 'size' ? 'Bytes stored' : 'Files'} actions={picker}>
        <DonutChart
          label="Files by type"
          totalLabel={measure === 'size' ? 'stored' : 'files'}
          format={format}
          slices={stats.by_type.map((row) => ({ key: row.type, label: typeLabels[row.type] ?? row.type, value: row[measure] }))}
        />
      </Card>
      <Card title="By disk" description={measure === 'size' ? 'Bytes stored' : 'Files'}>
        <DonutChart
          label="Files by disk"
          totalLabel={measure === 'size' ? 'stored' : 'files'}
          format={format}
          slices={stats.by_disk.map((row) => ({ key: row.disk, label: row.disk, value: row[measure] }))}
        />
      </Card>
    </div>
  )
}

// ── Sessions ────────────────────────────────────────────

/** Sessions: active tokens by when they were last used. */
export function SessionChart({ stats }: { stats: DevSessionStats }) {
  return (
    <DonutChart
      label="Active tokens by last use"
      totalLabel="tokens"
      slices={[
        { key: 'used', label: 'Used in the last 24 hours', value: stats.used_24h, tone: 'ok' },
        { key: 'idle', label: 'Idle 1 to 30 days', value: Math.max(0, stats.idle_tokens - stats.inactive_30d), tone: 'series-4' },
        { key: 'inactive', label: 'Unused for 30+ days', value: stats.inactive_30d, tone: 'muted' },
      ]}
    />
  )
}
