import { keepPreviousData, useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useDeferredValue, useEffect, useMemo, useState } from 'react'
import { api } from '../api/client'
import type { CursorPage, DevAction, DevActionFacets, DevActionStats } from '../api/types'
import { DatePicker, Select } from '../components/inputs'
import {
  Button,
  Card,
  Badge,
  CopyButton,
  Drawer,
  EmptyState,
  ErrorBox,
  Icon,
  JsonView,
  LoadingBlock,
  PageHeader,
  RefreshToggle,
  StatTile,
  type BadgeTone,
} from '../components/ui'
import { formatDateTime, formatDuration, localDayEnd, localDayStart, relativeTime } from '../format'

interface AuditSearch {
  q?: string
  action?: string
  who?: number
  status?: 'ok' | 'error'
  from?: string
  to?: string
}

export const Route = createFileRoute('/_app/actions')({
  validateSearch: (search: Record<string, unknown>): AuditSearch => ({
    q: typeof search.q === 'string' && search.q ? search.q : undefined,
    action: typeof search.action === 'string' && search.action ? search.action : undefined,
    who: typeof search.who === 'number' ? search.who : undefined,
    status: search.status === 'ok' || search.status === 'error' ? search.status : undefined,
    from: typeof search.from === 'string' && search.from ? search.from : undefined,
    to: typeof search.to === 'string' && search.to ? search.to : undefined,
  }),
  component: AuditPage,
})

// ── labels ────────────────────────────────────────────────────────────────

const labels: Record<string, [string, string]> = {
  'auth.login': ['Signed in', 'sign-in'],
  'auth.logout': ['Signed out', 'sign-in'],
  'command.run': ['Command run', 'command'],
  'schedule.run': ['Scheduled task run now', 'command'],
  'backups.create': ['Backup started', 'backup'],
  'backups.clean': ['Backups cleaned', 'backup'],
  'backups.delete': ['Backup deleted', 'backup'],
  'developers.create': ['Developer added', 'access'],
  'developers.role.ensure': ['Developer role created', 'access'],
  'developers.unassign': ['Developer removed', 'access'],
  'files.delete': ['File deleted', 'files'],
  'maintenance.down': ['Maintenance mode on', 'maintenance'],
  'maintenance.up': ['Maintenance mode off', 'maintenance'],
  'flags.set': ['Feature flags changed', 'flags'],
  'flags.reset': ['Feature flag reset', 'flags'],
  'flags.sync': ['Feature flags synced', 'flags'],
  'playground.jobs': ['Playground jobs queued', 'queue'],
  'queue.job.cancel': ['Job cancelled', 'queue'],
  'queue.job.delete': ['Job deleted', 'queue'],
  'queue.job.retry': ['Failed job retried', 'queue'],
  'queue.pause': ['Queue paused', 'queue'],
  'queue.resume': ['Queue resumed', 'queue'],
  'sessions.logout_all': ['Everyone signed out', 'sessions'],
  'sessions.prune': ['Idle sessions pruned', 'sessions'],
  'sessions.token.revoke': ['Session revoked', 'sessions'],
  'sessions.user.revoke': ['User signed out', 'sessions'],
  'settings.set': ['Setting changed', 'settings'],
  'settings.reset': ['Setting reset', 'settings'],
  'settings.sync': ['Settings synced', 'settings'],
  'settings.place': ['Settings rearranged', 'settings'],
  'settings.groups.reorder': ['Groups reordered', 'settings'],
  'settings.group.create': ['Settings group created', 'settings'],
  'settings.group.update': ['Settings group updated', 'settings'],
  'settings.group.delete': ['Settings group deleted', 'settings'],
  'settings.group.assign': ['Setting moved', 'settings'],
  'telemetry.prune': ['Telemetry pruned', 'telescope'],
  'dev.simulate-events': ['Sample events generated', 'telescope'],
}

function actionLabel(action: string, status?: string): string {
  if (action === 'auth.login' && status && status !== 'ok') return 'Sign-in failed'
  const known = labels[action]?.[0]
  if (known) return known
  const text = action.replace(/[._-]+/g, ' ')
  return text.charAt(0).toUpperCase() + text.slice(1)
}

const actionTag = (action: string) => labels[action]?.[1] ?? action.split('.')[0]

/** "Edge 157 on Windows" from a user agent. */
function shortBrowser(agent?: string | null): string {
  if (!agent) return '—'
  const browser =
    /Edg\/(\d+)/.exec(agent)?.[0].replace('Edg/', 'Edge ') ??
    /OPR\/(\d+)/.exec(agent)?.[0].replace('OPR/', 'Opera ') ??
    /Firefox\/(\d+)/.exec(agent)?.[0].replace('/', ' ') ??
    /Chrome\/(\d+)/.exec(agent)?.[0].replace('/', ' ') ??
    (/Version\/(\d+).*Safari/.test(agent) ? `Safari ${/Version\/(\d+)/.exec(agent)?.[1]}` : null) ??
    (/curl\/[\d.]+/.exec(agent)?.[0] ?? agent.split(' ')[0])
  const os = /Windows/.test(agent)
    ? 'Windows'
    : /Mac OS X/.test(agent)
      ? 'macOS'
      : /Android/.test(agent)
        ? 'Android'
        : /iPhone|iPad/.test(agent)
          ? 'iOS'
          : /Linux/.test(agent)
            ? 'Linux'
            : ''
  return os ? `${browser} on ${os}` : browser
}

function who(action: DevAction): string {
  if (action.actor_email) return action.actor_email
  if (action.action === 'auth.login' && action.target) return action.target
  return action.actor_user_id ? `user #${action.actor_user_id}` : 'unknown'
}

function shownTarget(action: DevAction): string {
  if (!action.target || action.action === 'auth.login') return ''
  return action.target.replace(':', ' ')
}

const statusTone = (status: string): BadgeTone => (status === 'ok' ? 'ok' : 'danger')

// ── page ──────────────────────────────────────────────────────────────────

function AuditPage() {
  const search = Route.useSearch()
  const navigate = useNavigate({ from: '/actions' })
  const [live, setLive] = useState(false)
  const [text, setText] = useState(search.q ?? '')
  const deferredText = useDeferredValue(text.trim())
  const [openIndex, setOpenIndex] = useState<number | null>(null)

  const setFilter = (patch: Partial<AuditSearch>) => {
    setOpenIndex(null)
    void navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true })
  }
  useEffect(() => {
    if ((search.q ?? '') !== deferredText) setFilter({ q: deferredText || undefined })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [deferredText])
  useEffect(() => setText(search.q ?? ''), [search.q])

  const filters = { ...search, q: deferredText || undefined }
  const query = useInfiniteQuery({
    queryKey: ['actions', filters],
    queryFn: ({ pageParam, signal }) =>
      api<CursorPage<DevAction> & { meta: { next_cursor?: string; total?: number } }>('/dev/api/actions', {
        query: {
          limit: 50,
          cursor: pageParam || undefined,
          search: filters.q,
          action: filters.action,
          actor_id: filters.who,
          status: filters.status,
          from: localDayStart(filters.from),
          to: localDayEnd(filters.to),
        },
        signal,
        unwrap: false,
      }),
    initialPageParam: '',
    getNextPageParam: (lastPage) => lastPage.meta?.next_cursor || undefined,
    refetchInterval: live ? 5000 : false,
    placeholderData: keepPreviousData,
  })
  const stats = useQuery({
    queryKey: ['actions', 'stats'],
    queryFn: ({ signal }) => api<DevActionStats>('/dev/api/actions/stats', { signal }),
    refetchInterval: live ? 5000 : false,
  })
  const facets = useQuery({
    queryKey: ['actions', 'facets'],
    queryFn: ({ signal }) => api<DevActionFacets>('/dev/api/actions/facets', { signal }),
    staleTime: 30_000,
  })

  const rows = useMemo(() => query.data?.pages.flatMap((page) => page.data) ?? [], [query.data])
  const total = query.data?.pages[0]?.meta?.total ?? rows.length
  const filtered = Boolean(filters.q || filters.action || filters.who || filters.status || filters.from || filters.to)
  const selected = openIndex !== null ? rows[openIndex] ?? null : null

  const exportCSV = () => {
    const header = ['when', 'who', 'action', 'result', 'target', 'ip', 'browser', 'duration_ms', 'details']
    const escape = (value: unknown) => `"${String(value ?? '').replace(/"/g, '""')}"`
    const lines = rows.map((row) =>
      [row.created_at, who(row), row.action, row.status, row.target, row.ip, row.user_agent, row.duration_ms, JSON.stringify(row.params)]
        .map(escape)
        .join(','),
    )
    const blob = new Blob([[header.join(','), ...lines].join('\n')], { type: 'text/csv' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `audit-log-${new Date().toISOString().slice(0, 10)}.csv`
    link.click()
    URL.revokeObjectURL(url)
  }

  return (
    <>
      <PageHeader
        title="Audit log"
        description="Every sign-in and every action taken in this dashboard: who, what, from where."
        actions={
          <>
            <RefreshToggle enabled={live} onChange={setLive} />
            <Button icon="download" onClick={exportCSV} disabled={rows.length === 0}>
              Export CSV
            </Button>
            <Button icon="refresh" onClick={() => void query.refetch()} disabled={query.isFetching}>
              Refresh
            </Button>
          </>
        }
      />

      <div className="grid cols-3">
        <StatTile label="Last 24 h" value={stats.data?.last_day ?? 0} hint="recorded actions" />
        <div className="stat-card">
          <div className="stat-label">Failed sign-ins (24 h)</div>
          <div className={`stat-value ${(stats.data?.failed_sign_ins ?? 0) > 0 ? 'danger' : ''}`}>{stats.data?.failed_sign_ins ?? 0}</div>
          <div className="stat-hint">
            wrong password or unknown account ·{' '}
            <button type="button" className="link-btn" onClick={() => setFilter({ action: 'auth.login', status: 'error' })}>
              show them
            </button>
          </div>
        </div>
        <StatTile
          label="Retention"
          value={stats.data ? `${stats.data.retention_days} days` : '—'}
          hint="older rows are pruned daily"
        />
      </div>

      <Card className="mt-16" tight>
        <div className="audit-filters">
          <label className="field audit-search">
            <span className="field-label">Search</span>
            <span className="inspector-search">
              <Icon name="search" />
              <input
                className="input"
                placeholder="Email, target, IP or details"
                value={text}
                onChange={(event) => setText(event.target.value)}
              />
            </span>
          </label>
          <label className="field">
            <span className="field-label">Action</span>
            <Select
              value={filters.action ?? ''}
              onChange={(value) => setFilter({ action: value || undefined })}
              options={[
                { value: '', label: 'Any action' },
                ...(facets.data?.actions ?? []).map((kind) => ({
                  value: kind.action,
                  label: actionLabel(kind.action),
                  text: `${actionLabel(kind.action)} ${kind.action}`,
                  hint: String(kind.total),
                })),
              ]}
            />
          </label>
          <label className="field">
            <span className="field-label">Who</span>
            <Select
              value={filters.who ? String(filters.who) : ''}
              onChange={(value) => setFilter({ who: value ? Number(value) : undefined })}
              options={[
                { value: '', label: 'Anyone' },
                ...(facets.data?.actors ?? [])
                  .filter((actor) => actor.user_id > 0)
                  .map((actor) => ({
                    value: String(actor.user_id),
                    label: actor.email ?? `user #${actor.user_id}`,
                    hint: String(actor.total),
                  })),
              ]}
            />
          </label>
          <label className="field">
            <span className="field-label">Result</span>
            <Select
              value={filters.status ?? ''}
              onChange={(value) => setFilter({ status: (value || undefined) as AuditSearch['status'] })}
              options={[
                { value: '', label: 'Any result' },
                { value: 'ok', label: 'Succeeded' },
                { value: 'error', label: 'Failed' },
              ]}
            />
          </label>
          <label className="field">
            <span className="field-label">From</span>
            <DatePicker
              value={filters.from ?? ''}
              max={filters.to}
              placeholder="Not set"
              onChange={(value) => setFilter({ from: value || undefined })}
            />
          </label>
          <label className="field">
            <span className="field-label">To</span>
            <DatePicker
              value={filters.to ?? ''}
              min={filters.from}
              placeholder="Not set"
              onChange={(value) => setFilter({ to: value || undefined })}
            />
          </label>
          <div className="audit-count">
            {filtered ? (
              <button
                type="button"
                className="link-btn"
                onClick={() => {
                  setText('')
                  void navigate({ search: {}, replace: true })
                }}
              >
                Clear filters
              </button>
            ) : null}
            <span className="small dim">{total} matching</span>
          </div>
        </div>
      </Card>

      <Card className="mt-16" tight>
        {query.isLoading ? (
          <LoadingBlock label="Loading audit log…" />
        ) : query.isError ? (
          <ErrorBox message={query.error instanceof Error ? query.error.message : 'Unable to load the audit log.'} />
        ) : rows.length === 0 ? (
          <EmptyState
            title={filtered ? 'Nothing matches' : 'No actions recorded yet'}
            description={
              filtered
                ? 'Try a wider date range or clear a filter.'
                : 'Sign-ins and every change made in this dashboard appear here with who did it and from where.'
            }
            icon="actions"
          />
        ) : (
          <div className="table-wrap flat">
            <table className="table audit-table">
              <thead>
                <tr>
                  <th className="w-sm">When</th>
                  <th>Who</th>
                  <th>Action</th>
                  <th>Target</th>
                  <th className="w-sm">IP</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row, index) => (
                  <tr
                    key={row.id}
                    className={`clickable ${openIndex === index ? 'selected' : ''}`}
                    onClick={() => setOpenIndex(index)}
                  >
                    <td className="small dim nowrap" title={formatDateTime(row.created_at)}>
                      {relativeTime(row.created_at)}
                    </td>
                    <td className="small">{who(row)}</td>
                    <td>
                      <span className="audit-action">
                        <span className={`status-dot ${statusTone(row.status)}`} />
                        <b>{actionLabel(row.action, row.status)}</b>
                        <span className="tag">{actionTag(row.action)}</span>
                      </span>
                    </td>
                    <td className="mono small dim truncate maxw-sm">
                      {shownTarget(row)}
                    </td>
                    <td className="mono small dim">{row.ip ?? '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
      <div className="flex mt-16">
        {query.hasNextPage ? (
          <Button icon="chevron" onClick={() => void query.fetchNextPage()} disabled={query.isFetchingNextPage}>
            {query.isFetchingNextPage ? 'Loading…' : 'Load more'}
          </Button>
        ) : rows.length > 0 ? (
          <span className="small dim">
            Showing all {rows.length} of {total}
          </span>
        ) : null}
      </div>

      <AuditDrawer
        action={selected}
        hasPrev={openIndex !== null && openIndex > 0}
        hasNext={openIndex !== null && openIndex < rows.length - 1}
        onPrev={() => setOpenIndex((index) => (index !== null && index > 0 ? index - 1 : index))}
        onNext={() => setOpenIndex((index) => (index !== null && index < rows.length - 1 ? index + 1 : index))}
        onClose={() => setOpenIndex(null)}
        onFilter={setFilter}
      />
    </>
  )
}

function AuditDrawer({
  action,
  hasPrev,
  hasNext,
  onPrev,
  onNext,
  onClose,
  onFilter,
}: {
  action: DevAction | null
  hasPrev: boolean
  hasNext: boolean
  onPrev: () => void
  onNext: () => void
  onClose: () => void
  onFilter: (patch: Partial<AuditSearch>) => void
}) {
  useEffect(() => {
    if (!action) return
    const onKey = (event: KeyboardEvent) => {
      if (event.target instanceof HTMLInputElement) return
      if (event.key === 'ArrowUp' || event.key === 'k') onPrev()
      if (event.key === 'ArrowDown' || event.key === 'j') onNext()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [action, onPrev, onNext])

  if (!action) return null
  const label = actionLabel(action.action, action.status)
  return (
    <Drawer
      open
      title={
        <span className="audit-action">
          <span className={`status-dot ${statusTone(action.status)}`} />
          {label}
        </span>
      }
      subtitle={`${formatDateTime(action.created_at)} · ${relativeTime(action.created_at)}`}
      onClose={onClose}
      actions={
        <span className="flex g-1">
          <button type="button" className="icon-btn flip" title="Newer (↑)" disabled={!hasPrev} onClick={onPrev}>
            <Icon name="chevron" />
          </button>
          <button type="button" className="icon-btn" title="Older (↓)" disabled={!hasNext} onClick={onNext}>
            <Icon name="chevron" />
          </button>
        </span>
      }
    >
      <div className="stack">
        <dl className="kv audit-kv">
          <dt>Result</dt>
          <dd>
            <Badge tone={statusTone(action.status)}>{action.status === 'ok' ? 'ok' : 'failed'}</Badge>
          </dd>
          <dt>Who</dt>
          <dd>
            {who(action)}
            {action.actor_user_id ? <span className="dim"> (user #{action.actor_user_id})</span> : null}
          </dd>
          <dt>IP</dt>
          <dd>{action.ip ?? '—'}</dd>
          <dt>Browser</dt>
          <dd>
            <div>{shortBrowser(action.user_agent)}</div>
            {action.user_agent ? <div className="small dim">{action.user_agent}</div> : null}
          </dd>
          <dt>Action key</dt>
          <dd>{action.action}</dd>
          {action.target ? (
            <>
              <dt>Target</dt>
              <dd>{action.target}</dd>
            </>
          ) : null}
          <dt>Duration</dt>
          <dd>{formatDuration(action.duration_ms)}</dd>
        </dl>
        {action.error_message ? <ErrorBox message={action.error_message} /> : null}
        <div>
          <div className="flex mb-4">
            <span className="section-label m-0">
              Details
            </span>
            <span className="spacer" />
            <CopyButton value={JSON.stringify(action.params, null, 2)} />
          </div>
          <JsonView value={action.params} />
          <div className="small dim mt-8">Values of keys that look like secrets are never stored.</div>
        </div>
        <div className="audit-quick">
          {action.actor_user_id ? (
            <Button size="sm" icon="developers" onClick={() => onFilter({ who: action.actor_user_id })}>
              Everything by this person
            </Button>
          ) : null}
          <Button size="sm" icon="actions" onClick={() => onFilter({ action: action.action })}>
            Only “{actionLabel(action.action)}”
          </Button>
          {action.ip ? (
            <Button size="sm" icon="search" onClick={() => onFilter({ q: action.ip ?? undefined })}>
              From this IP
            </Button>
          ) : null}
        </div>
      </div>
    </Drawer>
  )
}
