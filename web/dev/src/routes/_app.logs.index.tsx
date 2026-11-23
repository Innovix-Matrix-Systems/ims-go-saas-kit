import { useInfiniteQuery } from '@tanstack/react-query'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { useDeferredValue, useMemo, useRef, useState } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { api, writeSampleEvents } from '../api/client'
import type { CursorPage, DevLog } from '../api/types'
import { Button, EmptyState, ErrorBox, Icon, LoadingBlock, PageHeader, RefreshToggle } from '../components/ui'
import { formatTime, levelTone, relativeTime } from '../format'

export const Route = createFileRoute('/_app/logs/')({
  component: LogsPage,
})

const levels = ['', 'warn', 'error'] as const

function LogsPage() {
  const navigate = useNavigate()
  const [level, setLevel] = useState('')
  const [live, setLive] = useState(false)
  const [search, setSearch] = useState('')
  const deferredSearch = useDeferredValue(search.trim())
  const scrollRef = useRef<HTMLDivElement>(null)

  const query = useInfiniteQuery({
    queryKey: ['logs', { level, search: deferredSearch }],
    queryFn: ({ pageParam, signal }) =>
      api<CursorPage<DevLog>>('/dev/api/logs', {
        query: { limit: 50, cursor: pageParam || undefined, level: level || undefined, search: deferredSearch || undefined },
        signal,
        unwrap: false,
      }),
    initialPageParam: '',
    getNextPageParam: (lastPage) => lastPage.meta?.next_cursor || undefined,
    refetchInterval: live ? 5000 : false,
  })

  const rows = useMemo(() => query.data?.pages.flatMap((page) => page.data) ?? [], [query.data])

  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 54,
    overscan: 12,
  })

  const open = (log: DevLog) => void navigate({ to: '/logs/$id', params: { id: String(log.id) } })

  return (
    <>
      <PageHeader
        title="Logs"
        description="Warnings and errors the application wrote, newest first, each tied to its request."
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
        <div className="segmented" role="group" aria-label="Level">
          {levels.map((item) => (
            <button
              key={item || 'all'}
              className={level === item ? 'active' : ''}
              aria-pressed={level === item}
              onClick={() => setLevel(item)}
              type="button"
            >
              {item ? item.charAt(0).toUpperCase() + item.slice(1) : 'All'}
            </button>
          ))}
        </div>
        <label className="inspector-search search-box">
          <Icon name="search" />
          <input
            className="input"
            type="search"
            placeholder="Search the message, attributes or request ID"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
        </label>
        <div className="spacer" />
        <span className="small dim">
          {rows.length} loaded{deferredSearch ? ` matching “${deferredSearch}”` : ''}
        </span>
      </div>

      {query.isLoading ? (
        <LoadingBlock label="Loading logs…" />
      ) : query.isError ? (
        <ErrorBox
          message={query.error instanceof Error ? query.error.message : 'Unable to load logs.'}
          onRetry={() => void query.refetch()}
        />
      ) : rows.length === 0 ? (
        <div className="log-list">
          {deferredSearch || level ? (
            <EmptyState
              title="No logs match"
              description="Search looks in the message, the attributes and the start of the request ID."
              icon="search"
              action={
                <Button
                  icon="x"
                  onClick={() => {
                    setSearch('')
                    setLevel('')
                  }}
                >
                  Clear filters
                </Button>
              }
            />
          ) : (
            <EmptyState
              title="No logs captured yet"
              description="Warnings and errors appear here with the ID of the request that wrote them."
              icon="logs"
              action={
                <Button
                  icon="terminal"
                  onClick={async () => {
                    await writeSampleEvents({ logs: 3, level: 'warn', exception: false })
                    void query.refetch()
                  }}
                >
                  Write sample logs
                </Button>
              }
            />
          )}
        </div>
      ) : (
        <div className="log-list">
          <div
            className="log-scroll"
            ref={scrollRef}
            onScroll={(event) => {
              const target = event.currentTarget
              if (target.scrollHeight - target.scrollTop - target.clientHeight < 400) {
                if (query.hasNextPage && !query.isFetchingNextPage) void query.fetchNextPage()
              }
            }}
          >
            {/* Virtualised: the height and offsets below are computed per row. */}
            <div className="log-virtual" style={{ height: virtualizer.getTotalSize() }}>
              {virtualizer.getVirtualItems().map((virtualRow) => {
                const log = rows[virtualRow.index]
                if (!log) return null
                const attributes = log.attributes ? compactAttributes(log.attributes) : ''
                return (
                  <div
                    key={log.id}
                    ref={virtualizer.measureElement}
                    data-index={virtualRow.index}
                    className={`log-row level-${levelTone(log.level)}`}
                    style={{ transform: `translateY(${virtualRow.start}px)` }}
                    role="link"
                    tabIndex={0}
                    onClick={() => open(log)}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter') open(log)
                    }}
                  >
                    <span className="log-time" title={log.created_at}>
                      {formatTime(log.created_at)}
                    </span>
                    <span className="log-body">
                      <span className="log-msg">
                        <span className="log-level">{log.level.toLowerCase()}</span> {log.message}
                      </span>
                      {attributes ? <span className="attrs">{attributes}</span> : null}
                    </span>
                    {log.request_id ? (
                      <Link
                        to="/requests"
                        search={{ request_id: log.request_id }}
                        className="log-request mono"
                        title={`Requests with ID ${log.request_id}`}
                        onClick={(event) => event.stopPropagation()}
                      >
                        {log.request_id.slice(0, 10)}
                      </Link>
                    ) : (
                      <span className="log-request dim">{relativeTime(log.created_at)}</span>
                    )}
                  </div>
                )
              })}
            </div>
          </div>
        </div>
      )}
    </>
  )
}

function compactAttributes(attributes: unknown): string {
  try {
    const text = JSON.stringify(attributes)
    return text === '{}' ? '' : text.slice(0, 220)
  } catch {
    return ''
  }
}
