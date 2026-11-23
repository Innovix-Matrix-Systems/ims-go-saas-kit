import { keepPreviousData, useInfiniteQuery } from '@tanstack/react-query'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useDeferredValue, useMemo, useState } from 'react'
import type { ColumnDef } from '@tanstack/react-table'
import { api } from '../api/client'
import type { CursorPage, DevRequestSummary } from '../api/types'
import { DataTable } from '../components/DataTable'
import { Select } from '../components/inputs'
import {
  Badge,
  Button,
  Cluster,
  EmptyState,
  ErrorBox,
  LoadingBlock,
  MethodBadge,
  PageHeader,
  RefreshToggle,
  Spinner,
  StatusChip,
} from '../components/ui'
import { formatBytes, formatDuration, relativeTime } from '../format'

export const Route = createFileRoute('/_app/requests/')({
  validateSearch: (search: Record<string, unknown>) => ({
    request_id: typeof search.request_id === 'string' ? search.request_id : undefined,
  }),
  component: RequestsPage,
})

const methods = ['', 'GET', 'POST', 'PATCH', 'PUT', 'DELETE']

function RequestsPage() {
  const navigate = useNavigate()
  const search = Route.useSearch()
  const [searchText, setSearchText] = useState('')
  const [requestID, setRequestID] = useState(search.request_id ?? '')
  const [method, setMethod] = useState('')
  const [status, setStatus] = useState('')
  const [live, setLive] = useState(false)
  const deferredSearch = useDeferredValue(searchText)
  const filtered = Boolean(searchText || requestID || method || status)

  const query = useInfiniteQuery({
    queryKey: ['requests', { search: deferredSearch, method, status, requestID }],
    queryFn: ({ pageParam, signal }) =>
      api<CursorPage<DevRequestSummary>>('/dev/api/requests', {
        query: {
          limit: 25,
          cursor: pageParam || undefined,
          search: deferredSearch || undefined,
          method: method || undefined,
          status: status || undefined,
          request_id: requestID || undefined,
        },
        signal,
        unwrap: false,
      }),
    initialPageParam: '',
    getNextPageParam: (lastPage) => lastPage.meta?.next_cursor || undefined,
    placeholderData: keepPreviousData,
    refetchInterval: live ? 5000 : false,
  })

  const rows = useMemo(() => query.data?.pages.flatMap((page) => page.data) ?? [], [query.data])
  const maxDuration = useMemo(() => Math.max(1, ...rows.map((row) => row.duration_ms)), [rows])

  const clearFilters = () => {
    setSearchText('')
    setRequestID('')
    setMethod('')
    setStatus('')
  }

  const columns = useMemo<ColumnDef<DevRequestSummary, any>[]>(
    () => [
      {
        id: 'time',
        header: 'Time',
        accessorFn: (row) => row.created_at,
        cell: ({ row }) => (
          <span className="nowrap small dim" title={row.original.created_at}>
            {relativeTime(row.original.created_at)}
          </span>
        ),
      },
      {
        id: 'method',
        header: 'Method',
        accessorFn: (row) => row.method,
        cell: ({ row }) => <MethodBadge method={row.original.method} />,
      },
      {
        id: 'path',
        header: 'Path',
        accessorFn: (row) => row.path,
        cell: ({ row }) => (
          <div>
            <div className="path" title={row.original.path}>
              {row.original.path}
            </div>
            {row.original.route && row.original.route !== row.original.path ? (
              <div className="small dim mono">{row.original.route}</div>
            ) : null}
          </div>
        ),
      },
      {
        id: 'status',
        header: 'Status',
        accessorFn: (row) => row.status,
        cell: ({ row }) => <StatusChip status={row.original.status} />,
      },
      {
        id: 'duration',
        header: 'Duration',
        accessorFn: (row) => row.duration_ms,
        cell: ({ row }) => (
          <div className="duration-cell">
            <span className="num">{formatDuration(row.original.duration_ms)}</span>
            <div className="duration-bar">
              {/* Data-driven width: the share of the slowest request on screen. */}
              <span style={{ width: `${Math.min(100, (row.original.duration_ms / maxDuration) * 100)}%` }} />
            </div>
          </div>
        ),
      },
      {
        id: 'who',
        header: 'Who',
        accessorFn: (row) => row.user_id ?? 0,
        cell: ({ row }) => <Who request={row.original} />,
      },
      {
        id: 'bytes',
        header: 'Size',
        accessorFn: (row) => row.response_bytes,
        cell: ({ row }) => <span className="small dim nowrap num">{formatBytes(row.original.response_bytes)}</span>,
      },
    ],
    [maxDuration],
  )

  return (
    <>
      <PageHeader
        title="Requests"
        description="Every captured HTTP request, with its SQL, cache and queue events, logs and exceptions."
        actions={
          <>
            <RefreshToggle enabled={live} onChange={setLive} />
            <Button icon="refresh" onClick={() => void query.refetch()} disabled={query.isFetching}>
              Refresh
            </Button>
          </>
        }
      />

      <div className="filters">
        <label className="field grow">
          <span className="field-label">Path</span>
          <input
            className="input mono"
            type="search"
            placeholder="/api/v1/…"
            value={searchText}
            onChange={(event) => setSearchText(event.target.value)}
          />
        </label>
        <label className="field">
          <span className="field-label">Method</span>
          <Select value={method} onChange={setMethod} options={methods.map((value) => ({ value, label: value || 'Any' }))} />
        </label>
        <label className="field narrow">
          <span className="field-label">Status</span>
          <input
            className="input num"
            inputMode="numeric"
            placeholder="Any"
            value={status}
            onChange={(event) => setStatus(event.target.value.replace(/\D/g, '').slice(0, 3))}
          />
        </label>
        <label className="field">
          <span className="field-label">Request ID</span>
          <input
            className="input mono"
            placeholder="Exact ID"
            value={requestID}
            onChange={(event) => setRequestID(event.target.value)}
          />
        </label>
        {filtered ? (
          <Button variant="ghost" icon="x" onClick={clearFilters}>
            Clear
          </Button>
        ) : null}
      </div>

      {query.isLoading ? (
        <LoadingBlock label="Loading captured requests…" />
      ) : query.isError ? (
        <ErrorBox
          message={query.error instanceof Error ? query.error.message : 'Unable to load requests.'}
          onRetry={() => void query.refetch()}
        />
      ) : (
        <>
          <DataTable
            data={rows}
            columns={columns}
            onRowClick={(row) => void navigate({ to: '/requests/$id', params: { id: String(row.id) } })}
            empty={
              <EmptyState
                title={filtered ? 'No requests match' : 'No requests captured yet'}
                description={
                  filtered
                    ? 'Try a shorter path or clear the filters.'
                    : 'Requests show up here as they reach the API while capture is on.'
                }
                icon="requests"
                action={
                  filtered ? (
                    <Button variant="ghost" icon="x" onClick={clearFilters}>
                      Clear filters
                    </Button>
                  ) : undefined
                }
              />
            }
          />
          <Cluster gap={3} className="mt-16">
            {query.hasNextPage ? (
              <Button icon="chevron" onClick={() => void query.fetchNextPage()} disabled={query.isFetchingNextPage}>
                {query.isFetchingNextPage ? 'Loading…' : 'Load more'}
              </Button>
            ) : null}
            <span className="small dim">{rowCountLabel(rows.length, query.hasNextPage)}</span>
            {query.isFetching && !query.isFetchingNextPage ? <Spinner /> : null}
          </Cluster>
        </>
      )}
    </>
  )
}

/** The user and tenant a request ran as, and how it was made. */
function Who({ request }: { request: DevRequestSummary }) {
  if (request.user_id == null && request.tenant_id == null && request.route !== '/mcp') {
    return <span className="small dim">guest</span>
  }
  return (
    <Cluster gap={1}>
      {request.user_id != null ? <span className="small muted">user {request.user_id}</span> : null}
      {request.tenant_id != null ? <span className="small dim">· tenant {request.tenant_id}</span> : null}
      {request.route === '/mcp' ? (
        <Badge tone="info" title="An AI agent called this through an MCP connector">
          MCP
        </Badge>
      ) : null}
      {request.impersonator_id != null ? (
        <Badge tone="violet" title={`Acting as user ${request.user_id} on behalf of user ${request.impersonator_id}`}>
          acting · {request.impersonator_id}
        </Badge>
      ) : null}
    </Cluster>
  )
}

function rowCountLabel(count: number, more: boolean): string {
  if (count === 0) return ''
  return `${count} request${count === 1 ? '' : 's'}${more ? ' loaded' : ''}`
}
