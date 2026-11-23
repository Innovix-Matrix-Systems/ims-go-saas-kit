import { useInfiniteQuery } from '@tanstack/react-query'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { useMemo, useState } from 'react'
import { api, writeSampleEvents } from '../api/client'
import type { CursorPage, DevException } from '../api/types'
import { useToast } from '../components/Toasts'
import {
  Badge,
  Button,
  Card,
  Cluster,
  EmptyState,
  ErrorBox,
  LoadingBlock,
  PageHeader,
  RefreshToggle,
} from '../components/ui'
import { formatDateTime, relativeTime } from '../format'

export const Route = createFileRoute('/_app/exceptions/')({
  component: ExceptionsPage,
})

type View = 'grouped' | 'all'

interface ExceptionGroup {
  key: string
  latest: DevException
  count: number
  firstSeen: string
}

function ExceptionsPage() {
  const navigate = useNavigate()
  const { push } = useToast()
  const [live, setLive] = useState(false)
  const [view, setView] = useState<View>('grouped')
  const query = useInfiniteQuery({
    queryKey: ['exceptions'],
    queryFn: ({ pageParam, signal }) =>
      api<CursorPage<DevException>>('/dev/api/exceptions', {
        query: { limit: 50, cursor: pageParam || undefined },
        signal,
        unwrap: false,
      }),
    initialPageParam: '',
    getNextPageParam: (lastPage) => lastPage.meta?.next_cursor || undefined,
    refetchInterval: live ? 5000 : false,
  })
  const rows = useMemo(() => query.data?.pages.flatMap((page) => page.data) ?? [], [query.data])

  // Same class and message = the same problem; rows arrive newest first.
  const groups = useMemo(() => {
    const byKey = new Map<string, ExceptionGroup>()
    for (const row of rows) {
      const key = `${row.class}\u0000${row.message}`
      const group = byKey.get(key)
      if (group) {
        group.count++
        group.firstSeen = row.created_at
      } else {
        byKey.set(key, { key, latest: row, count: 1, firstSeen: row.created_at })
      }
    }
    return [...byKey.values()]
  }, [rows])

  const simulate = async () => {
    try {
      await writeSampleEvents({ logs: 0, level: 'error', exception: true })
      void query.refetch()
    } catch (error) {
      push(error instanceof Error ? error.message : 'Unable to write a sample exception', 'error')
    }
  }
  const open = (exception: DevException) =>
    void navigate({ to: '/exceptions/$id', params: { id: String(exception.id) } })

  return (
    <>
      <PageHeader
        title="Exceptions"
        description="Panics and server errors with their stack trace, each tied to its request."
        actions={
          <>
            <RefreshToggle enabled={live} onChange={setLive} />
            <Button icon="refresh" onClick={() => void query.refetch()} disabled={query.isFetching}>
              Refresh
            </Button>
          </>
        }
      />

      {query.isLoading ? (
        <LoadingBlock label="Loading exceptions…" />
      ) : query.isError ? (
        <ErrorBox
          message={query.error instanceof Error ? query.error.message : 'Unable to load exceptions.'}
          onRetry={() => void query.refetch()}
        />
      ) : rows.length === 0 ? (
        <Card flush>
          <EmptyState
            title="No exceptions captured"
            description="Panics and server errors appear here with their stack trace."
            icon="exceptions"
            action={
              <Button icon="alert" onClick={() => void simulate()}>
                Write a sample exception
              </Button>
            }
          />
        </Card>
      ) : (
        <>
          <div className="toolbar">
            <div className="segmented" role="group" aria-label="View">
              <button type="button" className={view === 'grouped' ? 'active' : ''} aria-pressed={view === 'grouped'} onClick={() => setView('grouped')}>
                Grouped
              </button>
              <button type="button" className={view === 'all' ? 'active' : ''} aria-pressed={view === 'all'} onClick={() => setView('all')}>
                All
              </button>
            </div>
            <span className="small dim">
              {view === 'grouped'
                ? `${groups.length} kind${groups.length === 1 ? '' : 's'} in the ${rows.length} loaded`
                : `${rows.length} loaded`}
            </span>
          </div>

          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>Exception</th>
                  {view === 'grouped' ? <th className="right">Count</th> : null}
                  <th>{view === 'grouped' ? 'Last seen' : 'When'}</th>
                  {view === 'grouped' ? <th>First seen</th> : <th>Request</th>}
                </tr>
              </thead>
              <tbody>
                {view === 'grouped'
                  ? groups.map((group) => (
                      <tr key={group.key} className="clickable" onClick={() => open(group.latest)}>
                        <td>
                          <ExceptionSummary exception={group.latest} />
                        </td>
                        <td className="right">
                          <Badge tone={group.count > 1 ? 'danger' : 'neutral'} mono>
                            ×{group.count}
                          </Badge>
                        </td>
                        <td className="small nowrap" title={formatDateTime(group.latest.created_at)}>
                          {relativeTime(group.latest.created_at)}
                        </td>
                        <td className="small dim nowrap" title={formatDateTime(group.firstSeen)}>
                          {relativeTime(group.firstSeen)}
                        </td>
                      </tr>
                    ))
                  : rows.map((exception) => (
                      <tr key={exception.id} className="clickable" onClick={() => open(exception)}>
                        <td>
                          <ExceptionSummary exception={exception} />
                        </td>
                        <td className="small nowrap" title={formatDateTime(exception.created_at)}>
                          {relativeTime(exception.created_at)}
                        </td>
                        <td>
                          {exception.request_id ? (
                            <Link
                              to="/requests"
                              search={{ request_id: exception.request_id }}
                              className="small link mono"
                              onClick={(event) => event.stopPropagation()}
                            >
                              {exception.request_id.slice(0, 10)}
                            </Link>
                          ) : (
                            <span className="small dim">—</span>
                          )}
                        </td>
                      </tr>
                    ))}
              </tbody>
            </table>
          </div>
          <Cluster gap={3} className="mt-16">
            {query.hasNextPage ? (
              <Button icon="chevron" onClick={() => void query.fetchNextPage()} disabled={query.isFetchingNextPage}>
                {query.isFetchingNextPage ? 'Loading…' : 'Load more'}
              </Button>
            ) : null}
          </Cluster>
        </>
      )}
    </>
  )
}

function ExceptionSummary({ exception }: { exception: DevException }) {
  return (
    <div className="exception-summary">
      <span className="mono exception-class">{exception.class}</span>
      <span className="exception-line" title={exception.message}>
        {exception.message}
      </span>
    </div>
  )
}
