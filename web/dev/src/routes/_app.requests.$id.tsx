import { useQuery } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useMemo, useState } from 'react'
import { api } from '../api/client'
import type { DevQuery, DevRequestDetail } from '../api/types'
import {
  BackLink,
  Badge,
  Button,
  Card,
  Cluster,
  CodeBlock,
  CopyButton,
  DetailStrip,
  EmptyState,
  ErrorBox,
  Grid,
  JsonView,
  LoadingBlock,
  MethodBadge,
  PageHeader,
  Stack,
  StatusChip,
  Tabs,
} from '../components/ui'
import { formatBytes, formatDateTime, formatDuration, levelTone, relativeTime } from '../format'

export const Route = createFileRoute('/_app/requests/$id')({
  component: RequestDetailPage,
})

type Tab = 'overview' | 'sql' | 'cache' | 'queue' | 'logs' | 'exceptions' | 'headers' | 'payload'

function RequestDetailPage() {
  const { id } = Route.useParams()
  const [tab, setTab] = useState<Tab>('overview')
  const query = useQuery({
    queryKey: ['request', id],
    queryFn: ({ signal }) => api<DevRequestDetail>(`/dev/api/requests/${id}`, { signal }),
  })

  const detail = query.data
  const queries = detail?.queries ?? []
  const cacheEvents = detail?.cache_events ?? []
  const queueEvents = detail?.queue_events ?? []
  const logs = detail?.logs ?? []
  const exceptions = detail?.exceptions ?? []

  if (query.isLoading) return <LoadingBlock label="Loading request…" />
  if (query.isError) {
    return (
      <ErrorBox
        message={query.error instanceof Error ? query.error.message : 'Unable to load request.'}
        onRetry={() => void query.refetch()}
      />
    )
  }
  if (!detail) return <EmptyState title="Request not found" icon="requests" />

  const request = detail.request

  return (
    <>
      <BackLink to="/requests" search={{ request_id: undefined }}>
        Requests
      </BackLink>
      <PageHeader
        title={request.path}
        description={request.route && request.route !== request.path ? `Route ${request.route}` : undefined}
        meta={`Request #${request.id} · captured ${relativeTime(request.created_at)}`}
        actions={
          <Button icon="refresh" onClick={() => void query.refetch()} disabled={query.isFetching}>
            Refresh
          </Button>
        }
      />

      <DetailStrip
        items={[
          { label: 'Method', value: <MethodBadge method={request.method} /> },
          { label: 'Status', value: <StatusChip status={request.status} /> },
          { label: 'Duration', value: formatDuration(request.duration_ms) },
          { label: 'Size', value: `${formatBytes(request.request_bytes)} in · ${formatBytes(request.response_bytes)} out` },
          {
            label: 'Who',
            value: (
              <>
                {request.user_id != null ? `user ${request.user_id}` : 'guest'}
                {request.tenant_id != null ? ` · tenant ${request.tenant_id}` : ''}
                {request.route === '/mcp' ? <Badge tone="info">MCP</Badge> : null}
                {request.impersonator_id != null ? <Badge tone="violet">acting · {request.impersonator_id}</Badge> : null}
              </>
            ),
          },
          {
            label: 'Request ID',
            wide: true,
            value: (
              <>
                <span className="mono truncate" title={request.request_id}>
                  {request.request_id}
                </span>
                <CopyButton value={request.request_id} />
              </>
            ),
          },
        ]}
      />

      <Tabs
        active={tab}
        onChange={setTab}
        tabs={[
          { id: 'overview', label: 'Overview' },
          { id: 'sql', label: 'Queries', count: queries.length },
          { id: 'cache', label: 'Cache', count: cacheEvents.length },
          { id: 'queue', label: 'Jobs', count: queueEvents.length },
          { id: 'logs', label: 'Logs', count: logs.length },
          { id: 'exceptions', label: 'Exceptions', count: exceptions.length },
          { id: 'headers', label: 'Headers' },
          { id: 'payload', label: 'Payload' },
        ]}
      />

      {tab === 'overview' ? (
        <Grid min="lg" gap={4}>
          <Card title="Request">
            <dl className="kv">
              <dt>Route</dt>
              <dd>{request.route || '—'}</dd>
              <dt>IP</dt>
              <dd>{request.ip ?? '—'}</dd>
              <dt>User agent</dt>
              <dd>{request.user_agent ?? '—'}</dd>
              <dt>Tenant / branch</dt>
              <dd>
                {request.tenant_id ?? '—'} / {request.branch_id ?? '—'}
              </dd>
              <dt>Captured</dt>
              <dd>{formatDateTime(request.created_at)}</dd>
            </dl>
          </Card>
          <Card title="What it did" description="Everything captured while this request ran.">
            <dl className="kv">
              <dt>Queries</dt>
              <dd>
                {queries.length} · {formatDuration(totalDuration(queries))}
              </dd>
              <dt>Cache</dt>
              <dd>
                {cacheEvents.length} operations · {cacheEvents.filter((event) => event.hit).length} hits
              </dd>
              <dt>Jobs queued</dt>
              <dd>{queueEvents.length}</dd>
              <dt>Logs</dt>
              <dd>{logs.length}</dd>
              <dt>Exceptions</dt>
              <dd className={exceptions.length > 0 ? 'danger-text' : undefined}>{exceptions.length}</dd>
            </dl>
          </Card>
        </Grid>
      ) : null}

      {tab === 'sql' ? <QueriesTab queries={queries} /> : null}

      {tab === 'cache' ? (
        <Card title="Cache" flush>
          {cacheEvents.length === 0 ? (
            <EmptyState title="No cache operations" icon="activity" />
          ) : (
            <div className="table-wrap">
              <table className="table">
                <thead>
                  <tr>
                    <th>Operation</th>
                    <th>Key</th>
                    <th>Result</th>
                    <th className="right">Duration</th>
                    <th>Time</th>
                  </tr>
                </thead>
                <tbody>
                  {cacheEvents.map((event) => (
                    <tr key={event.id}>
                      <td>
                        <Badge mono>{event.operation}</Badge>
                      </td>
                      <td className="mono small">{event.key || '—'}</td>
                      <td>
                        {event.hit === null || event.hit === undefined ? (
                          '—'
                        ) : event.hit ? (
                          <Badge tone="ok">hit</Badge>
                        ) : (
                          <Badge>miss</Badge>
                        )}
                      </td>
                      <td className="num small right">{formatDuration(event.duration_ms)}</td>
                      <td className="small dim">{relativeTime(event.created_at)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      ) : null}

      {tab === 'queue' ? (
        <Card title="Jobs" description="Jobs this request queued, and how they ended." flush>
          {queueEvents.length === 0 ? (
            <EmptyState title="No jobs queued" icon="queue" />
          ) : (
            <div className="table-wrap">
              <table className="table">
                <thead>
                  <tr>
                    <th>Event</th>
                    <th>Queue</th>
                    <th>Kind</th>
                    <th>Job</th>
                    <th className="right">Duration</th>
                    <th>Time</th>
                  </tr>
                </thead>
                <tbody>
                  {queueEvents.map((event) => (
                    <tr key={event.id}>
                      <td>
                        <Badge tone={event.event === 'failed' ? 'danger' : 'neutral'}>{event.event}</Badge>
                      </td>
                      <td className="mono small">{event.queue}</td>
                      <td className="mono small">{event.kind}</td>
                      <td className="num small">
                        {event.job_id ? (
                          <Link to="/jobs/$id" params={{ id: String(event.job_id) }} className="link">
                            #{event.job_id}
                          </Link>
                        ) : (
                          <span className="dim" title="Captured before job ids were recorded">
                            —
                          </span>
                        )}
                      </td>
                      <td className="num small right">{formatDuration(event.duration_ms)}</td>
                      <td className="small dim">{relativeTime(event.created_at)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      ) : null}

      {tab === 'logs' ? (
        <Card title="Logs" description="Warnings and errors written while the request ran." flush>
          {logs.length === 0 ? (
            <EmptyState title="No logs captured" description="Logs are captured at warn level and above." icon="logs" />
          ) : (
            <div className="log-list flat">
              {logs.map((log) => (
                <Link key={log.id} to="/logs/$id" params={{ id: String(log.id) }} className={`log-row level-${levelTone(log.level)}`}>
                  <span className="log-time">{relativeTime(log.created_at)}</span>
                  <span className="log-level">{log.level.toLowerCase()}</span>
                  <span className="log-msg">{log.message}</span>
                </Link>
              ))}
            </div>
          )}
        </Card>
      ) : null}

      {tab === 'exceptions' ? (
        exceptions.length === 0 ? (
          <Card flush>
            <EmptyState title="No exceptions" icon="exceptions" />
          </Card>
        ) : (
          <Stack gap={3}>
            {exceptions.map((exception) => (
              <Card
                key={exception.id}
                title={<span className="mono">{exception.class}</span>}
                description={formatDateTime(exception.created_at)}
                actions={
                  <Link to="/exceptions/$id" params={{ id: String(exception.id) }} className="btn ghost sm">
                    Open
                  </Link>
                }
              >
                <Stack gap={3}>
                  <div className="exception-message">{exception.message}</div>
                  {exception.stack ? <CodeBlock code={exception.stack} label="Stack trace" maxHeight={260} /> : null}
                </Stack>
              </Card>
            ))}
          </Stack>
        )
      ) : null}

      {tab === 'headers' ? (
        <Card title="Headers" description="Sensitive values are redacted before they are stored.">
          <JsonView value={request.headers} label="Headers" />
        </Card>
      ) : null}

      {tab === 'payload' ? (
        <Grid min="lg" gap={4}>
          <Card title="Request body">
            <JsonView value={request.request_body} label="Request body" />
          </Card>
          <Card title="Response body">
            <JsonView value={request.response_body} label="Response body" />
          </Card>
        </Grid>
      ) : null}
    </>
  )
}

function totalDuration(queries: DevQuery[]): number {
  return queries.reduce((sum, item) => sum + item.duration_ms, 0)
}

/** The request's SQL, with the total time and statements that ran more than once (an N+1 hint). */
function QueriesTab({ queries }: { queries: DevQuery[] }) {
  const repeats = useMemo(() => {
    const counts = new Map<string, number>()
    for (const item of queries) counts.set(item.sql, (counts.get(item.sql) ?? 0) + 1)
    return counts
  }, [queries])
  const repeated = [...repeats.values()].filter((count) => count > 1).length
  const slowest = queries.reduce((max, item) => Math.max(max, item.duration_ms), 0)

  if (queries.length === 0) {
    return (
      <Card flush>
        <EmptyState title="No SQL captured" description="This request ran no statements, or SQL capture is off." icon="requests" />
      </Card>
    )
  }
  return (
    <Stack gap={3}>
      <Cluster gap={3} className="small muted">
        <span>
          {queries.length} {queries.length === 1 ? 'query' : 'queries'} · {formatDuration(totalDuration(queries))} in total
        </span>
        {repeated > 0 ? (
          <Badge tone="warn" title="The same statement ran more than once: a loop issuing one query per row?">
            {repeated} repeated
          </Badge>
        ) : null}
      </Cluster>
      {queries.map((item) => (
        <div className="query-row" key={item.id}>
          <CodeBlock code={item.sql} label="SQL" />
          <div className="query-meta">
            <span className={item.duration_ms === slowest && queries.length > 1 ? 'query-slowest' : undefined}>
              {formatDuration(item.duration_ms)}
              {item.duration_ms === slowest && queries.length > 1 ? ' · slowest' : ''}
            </span>
            <span>{item.row_count} rows</span>
            {(repeats.get(item.sql) ?? 0) > 1 ? <span>ran {repeats.get(item.sql)}×</span> : null}
            {item.error ? <Badge tone="danger">error</Badge> : null}
            <span className="spacer" />
            <span>{formatDateTime(item.created_at)}</span>
          </div>
          {item.args ? (
            <div className="mt-8">
              <JsonView value={item.args} label="Args" maxHeight={220} />
            </div>
          ) : null}
          {item.error ? <ErrorBox message={item.error} /> : null}
        </div>
      ))}
    </Stack>
  )
}
