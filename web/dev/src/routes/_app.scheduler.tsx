import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useEffect, useMemo, useRef, useState } from 'react'
import { api, fetchMe, getCachedMe } from '../api/client'
import type { CursorPage, DevScheduledTask, DevScheduler, DevScheduleRun } from '../api/types'
import { Select } from '../components/inputs'
import { useToast } from '../components/Toasts'
import {
  Badge,
  Button,
  Card,
  Cluster,
  ConfirmDialog,
  Drawer,
  EmptyState,
  ErrorBox,
  LiveDot,
  LoadingBlock,
  PageHeader,
  Spinner,
  Stack,
  StatusDot,
  Tabs,
  type BadgeTone,
} from '../components/ui'
import { describeCron, formatDateTime, formatDuration, relativeTime, timeUntil } from '../format'

// The scheduler's tasks: when each runs, when it runs next and how its
// recent runs went, plus the full run history (schedule_runs). Runs come from
// the `ims schedule` process (one per deployment); Run now does the same work
// as `ims schedule:run`.

type View = 'tasks' | 'history'
type RunStatus = DevScheduleRun['status']

interface SchedulerSearch {
  view?: View
  task?: string
  status?: RunStatus
}

const runStatuses: RunStatus[] = ['ok', 'failed', 'running', 'interrupted']

export const Route = createFileRoute('/_app/scheduler')({
  validateSearch: (search: Record<string, unknown>): SchedulerSearch => ({
    view: search.view === 'history' ? 'history' : undefined,
    task: typeof search.task === 'string' && search.task ? search.task : undefined,
    status: runStatuses.includes(search.status as RunStatus) ? (search.status as RunStatus) : undefined,
  }),
  component: SchedulerPage,
})

const STALE_HEARTBEAT_MS = 2 * 60_000

/** A task is running while its last run is open within its timeout (as the server decides); one open longer was lost with its process. */
function isRunning(task: DevScheduledTask): boolean {
  const run = task.last_run
  if (run?.status !== 'running') return false
  if (task.timeout_seconds <= 0) return true
  return Date.now() - new Date(run.started_at).getTime() < (task.timeout_seconds + 60) * 1000
}

function runTone(status: RunStatus): BadgeTone {
  switch (status) {
    case 'ok':
      return 'ok'
    case 'failed':
      return 'danger'
    case 'interrupted':
      return 'warn'
    default:
      return 'accent'
  }
}

function SchedulerPage() {
  const search = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })
  const queryClient = useQueryClient()
  const { push } = useToast()
  const [selected, setSelected] = useState<string | null>(null)
  const [confirm, setConfirm] = useState<DevScheduledTask | null>(null)
  const me = useQuery({ queryKey: ['me'], queryFn: fetchMe, staleTime: 60_000, placeholderData: getCachedMe() ?? undefined })
  const canManage = Boolean(me.data && (me.data.is_super_admin || me.data.permissions.includes('dev.manage')))
  const view: View = search.view ?? 'tasks'

  const scheduler = useQuery({
    queryKey: ['scheduler'],
    queryFn: ({ signal }) => api<DevScheduler>('/dev/api/scheduler', { signal }),
    // Follow a running task closely; otherwise a slow refresh keeps "next run" true.
    refetchInterval: (query) => (query.state.data?.tasks.some(isRunning) ? 2_000 : 30_000),
  })

  const run = useMutation({
    mutationFn: (task: DevScheduledTask) => api(`/dev/api/scheduler/${encodeURIComponent(task.id)}/run`, { method: 'POST' }),
    onSuccess: (_result, task) => {
      push(`${task.id} started`, 'info')
      setConfirm(null)
      // The run records itself as it starts; refetch to pick that up.
      window.setTimeout(() => {
        void queryClient.invalidateQueries({ queryKey: ['scheduler'] })
        void queryClient.invalidateQueries({ queryKey: ['schedule-runs'] })
      }, 400)
      void queryClient.invalidateQueries({ queryKey: ['actions'] })
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Unable to run the task', 'error'),
  })

  const data = scheduler.data
  const tasks = data?.tasks ?? []
  const heartbeatAge = data?.heartbeat_at ? Date.now() - new Date(data.heartbeat_at).getTime() : null
  const processRunning = heartbeatAge !== null && heartbeatAge < STALE_HEARTBEAT_MS
  const failing = tasks.filter((task) => task.last_run?.status === 'failed').length
  const current = tasks.find((task) => task.id === selected) ?? null
  const openHistory = (task?: string, status?: RunStatus) => {
    setSelected(null)
    void navigate({ search: { view: 'history', task, status } })
  }

  return (
    <>
      <PageHeader
        title="Scheduler"
        description="Every task the scheduler runs: when, when next, and how its runs went."
        meta={
          data ? (
            <>
              <StatusDot tone={processRunning ? 'ok' : 'warn'} />
              <span>
                {processRunning
                  ? `The schedule process checked in ${relativeTime(data.heartbeat_at!)}`
                  : data.heartbeat_at
                    ? `The schedule process last checked in ${relativeTime(data.heartbeat_at)}`
                    : 'The schedule process has not run against this cache'}
              </span>
              <span>·</span>
              <span>times in {data.timezone}</span>
            </>
          ) : undefined
        }
        actions={
          <Button
            icon="refresh"
            onClick={() => {
              void scheduler.refetch()
              void queryClient.invalidateQueries({ queryKey: ['schedule-runs'] })
            }}
            disabled={scheduler.isFetching}
          >
            Refresh
          </Button>
        }
      />

      <Tabs
        tabs={[
          { id: 'tasks', label: 'Tasks', count: tasks.length || undefined },
          { id: 'history', label: 'History' },
        ]}
        active={view}
        onChange={(next) => void navigate({ search: next === 'history' ? { view: 'history' } : {}, replace: true })}
      />

      {scheduler.isLoading ? (
        <LoadingBlock label="Loading scheduled tasks…" />
      ) : scheduler.isError || !data ? (
        <ErrorBox
          message={scheduler.error instanceof Error ? scheduler.error.message : 'Unable to load the scheduler.'}
          onRetry={() => void scheduler.refetch()}
        />
      ) : view === 'history' ? (
        <History tasks={tasks} search={search} />
      ) : (
        <Stack gap={4}>
          {!processRunning ? (
            <div className="banner">
              <Badge tone="warn">not running</Badge>
              <span>
                Nothing runs on schedule until a scheduler process is up: start <span className="mono">ims schedule</span>{' '}
                (<span className="mono">make schedule</span> locally). Run now works either way.
              </span>
            </div>
          ) : null}

          <Card
            title="Tasks"
            description={`${tasks.length} registered${failing > 0 ? ` · ${failing} failed last time` : ''}`}
            flush
            actions={
              failing > 0 ? (
                <Button size="sm" variant="ghost" onClick={() => openHistory(undefined, 'failed')}>
                  Show failures
                </Button>
              ) : undefined
            }
          >
            {tasks.length === 0 ? (
              <EmptyState icon="clock" title="No scheduled tasks" description="Tasks are registered in internal/app/schedule.go." />
            ) : (
              <div className="table-wrap">
                <table className="table">
                  <thead>
                    <tr>
                      <th>Task</th>
                      <th>Runs</th>
                      <th>Next</th>
                      <th>Last run</th>
                      <th>Recent</th>
                      <th />
                    </tr>
                  </thead>
                  <tbody>
                    {tasks.map((task) => (
                      <tr
                        key={task.id}
                        className={`clickable ${selected === task.id ? 'selected' : ''}`}
                        onClick={() => setSelected(task.id)}
                      >
                        <td>
                          <div className="mono task-id">{task.id}</div>
                          <div className="small dim truncate task-description" title={task.description}>
                            {task.description || '—'}
                          </div>
                        </td>
                        <td>
                          <div className="nowrap">{describeCron(task.spec)}</div>
                          <div className="small dim mono">{task.spec}</div>
                        </td>
                        <td className="nowrap" title={task.next_run ? formatDateTime(task.next_run) : undefined}>
                          {task.next_run ? timeUntil(task.next_run) : '—'}
                        </td>
                        <td>
                          {task.recorded ? (
                            <LastRunCell run={task.last_run ?? null} />
                          ) : (
                            <span className="small dim" title="This task keeps no run history">
                              not kept
                            </span>
                          )}
                        </td>
                        <td>
                          <RunStrip runs={task.recent} />
                        </td>
                        <td className="right">
                          <Button
                            size="sm"
                            icon="play"
                            disabled={!canManage || isRunning(task)}
                            title={canManage ? 'Run this task now' : 'Needs the dev.manage permission'}
                            onClick={(event) => {
                              event.stopPropagation()
                              setConfirm(task)
                            }}
                          >
                            Run now
                          </Button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>
        </Stack>
      )}

      <Drawer
        open={current !== null}
        size="md"
        title={current?.id ?? ''}
        subtitle={current?.description}
        onClose={() => setSelected(null)}
        actions={
          current?.recorded ? (
            <Button size="sm" onClick={() => openHistory(current.id)}>
              Full history
            </Button>
          ) : undefined
        }
      >
        {current ? (
          <Stack gap={4}>
            {current.last_run?.status === 'failed' && current.last_run.error ? <ErrorBox message={current.last_run.error} /> : null}
            <dl className="kv">
              <dt>Runs</dt>
              <dd>
                {describeCron(current.spec)} <span className="dim">({current.spec})</span>
              </dd>
              <dt>Next run</dt>
              <dd>{current.next_run ? `${formatDateTime(current.next_run)} (${timeUntil(current.next_run)})` : '—'}</dd>
              <dt>Timeout</dt>
              <dd>{current.timeout_seconds > 0 ? `${current.timeout_seconds} s` : 'none'}</dd>
              <dt>Overlap</dt>
              <dd>{current.skips_overlap ? 'skipped while the previous run is still going' : 'allowed'}</dd>
              <dt>Maintenance</dt>
              <dd>{current.even_in_maintenance ? 'runs while the app is down' : 'paused while the app is down'}</dd>
            </dl>
            {current.recorded ? (
              <Card title="Recent runs" description={`The last ${current.recent.length || 'few'}, newest first.`} flush>
                {current.recent.length === 0 ? (
                  <EmptyState icon="clock" title="No runs recorded yet" description="Runs appear here from the next one on." />
                ) : (
                  <RunTable runs={current.recent} showTask={false} />
                )}
              </Card>
            ) : (
              <div className="banner">
                <span>
                  This task runs too often to keep a history; the page header shows when the scheduler last checked in.
                </span>
              </div>
            )}
          </Stack>
        ) : null}
      </Drawer>

      <ConfirmDialog
        open={confirm !== null}
        title={`Run ${confirm?.id ?? 'task'} now?`}
        description="It runs once in the API process, the same as ims schedule:run, and is recorded in the history and the audit log. The regular schedule is not changed."
        confirmLabel="Run now"
        busy={run.isPending}
        onCancel={() => setConfirm(null)}
        onConfirm={() => {
          if (confirm) run.mutate(confirm)
        }}
      />
    </>
  )
}

/** Every recorded run, newest first, filtered by task and status; older pages load on scroll. */
function History({ tasks, search }: { tasks: DevScheduledTask[]; search: SchedulerSearch }) {
  const navigate = useNavigate({ from: Route.fullPath })
  const setFilter = (patch: Partial<SchedulerSearch>) =>
    void navigate({ search: (previous) => ({ ...previous, ...patch, view: 'history' }), replace: true })
  const filtered = Boolean(search.task || search.status)

  const runs = useInfiniteQuery({
    queryKey: ['schedule-runs', search.task, search.status],
    queryFn: ({ pageParam, signal }) =>
      api<CursorPage<DevScheduleRun>>('/dev/api/scheduler/runs', {
        query: { limit: 50, cursor: pageParam || undefined, task: search.task, status: search.status },
        signal,
        unwrap: false,
      }),
    initialPageParam: '',
    getNextPageParam: (lastPage) => lastPage.meta?.next_cursor || undefined,
  })
  const rows = useMemo(() => runs.data?.pages.flatMap((page) => page.data) ?? [], [runs.data])

  // Load the next, older page when the end of the list scrolls into view.
  const moreRef = useRef<HTMLDivElement>(null)
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = runs
  useEffect(() => {
    const target = moreRef.current
    if (!target || !hasNextPage) return
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting) && !isFetchingNextPage) void fetchNextPage()
      },
      { rootMargin: '200px' },
    )
    observer.observe(target)
    return () => observer.disconnect()
  }, [hasNextPage, isFetchingNextPage, fetchNextPage])

  return (
    <Stack gap={4}>
      <div className="filters">
        <label className="field">
          <span className="field-label">Task</span>
          <Select
            value={search.task ?? ''}
            onChange={(value) => setFilter({ task: value || undefined })}
            options={[{ value: '', label: 'Every task' }, ...tasks.map((task) => ({ value: task.id, label: task.id }))]}
          />
        </label>
        <div className="field">
          <span className="field-label">Status</span>
          <div className="segmented" role="group" aria-label="Status">
            {(['', ...runStatuses] as const).map((status) => (
              <button
                key={status || 'all'}
                type="button"
                className={(search.status ?? '') === status ? 'active' : ''}
                aria-pressed={(search.status ?? '') === status}
                onClick={() => setFilter({ status: status || undefined })}
              >
                {status ? status.charAt(0).toUpperCase() + status.slice(1) : 'All'}
              </button>
            ))}
          </div>
        </div>
        {filtered ? (
          <Button variant="ghost" icon="x" onClick={() => void navigate({ search: { view: 'history' }, replace: true })}>
            Clear
          </Button>
        ) : null}
      </div>

      <Card title="Runs" description={`Newest first · ${rows.length}${runs.hasNextPage ? ' loaded, more as you scroll' : ''}`} flush>
        {runs.isLoading ? (
          <LoadingBlock label="Loading the history…" />
        ) : runs.isError ? (
          <ErrorBox message={runs.error instanceof Error ? runs.error.message : 'Unable to load the history.'} onRetry={() => void runs.refetch()} />
        ) : rows.length === 0 ? (
          <EmptyState
            icon="clock"
            title={filtered ? 'No runs match' : 'No runs recorded yet'}
            description={filtered ? 'Clear a filter to see more.' : 'Every scheduled or manual run is recorded here from now on.'}
          />
        ) : (
          <RunTable runs={rows} showTask={!search.task} />
        )}
      </Card>
      <div ref={moreRef} className="load-more">
        {runs.isFetchingNextPage ? <Spinner /> : null}
        {!runs.hasNextPage && rows.length > 0 ? <span className="small dim">That is every recorded run.</span> : null}
      </div>
    </Stack>
  )
}

/** Runs as a table: when, which task, how it was started, how it went and why it failed. */
function RunTable({ runs, showTask }: { runs: DevScheduleRun[]; showTask: boolean }) {
  return (
    <div className="table-wrap">
      <table className="table">
        <thead>
          <tr>
            <th>Started</th>
            {showTask ? <th>Task</th> : null}
            <th>Status</th>
            <th className="right">Took</th>
            <th>Started by</th>
            <th>Error</th>
          </tr>
        </thead>
        <tbody>
          {runs.map((item) => (
            <tr key={item.id}>
              <td className="nowrap" title={formatDateTime(item.started_at)}>
                {relativeTime(item.started_at)}
              </td>
              {showTask ? <td className="mono small">{item.task}</td> : null}
              <td>
                {item.status === 'running' ? (
                  <Cluster gap={1}>
                    <LiveDot /> <span className="small">running</span>
                  </Cluster>
                ) : (
                  <Badge tone={runTone(item.status)}>{item.status}</Badge>
                )}
              </td>
              <td className="right num small">{item.duration_ms != null ? formatDuration(item.duration_ms) : '—'}</td>
              <td className="small muted">{item.trigger === 'manual' ? 'run now' : 'schedule'}</td>
              <td className="small danger-text">
                {item.error ? (
                  <span className="truncate task-error" title={item.error}>
                    {item.error}
                  </span>
                ) : null}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

/** The last ten runs as small squares, oldest left, newest right. */
function RunStrip({ runs }: { runs: DevScheduleRun[] }) {
  if (runs.length === 0) return <span className="small dim">—</span>
  return (
    <span className="run-strip" aria-label={`Last ${runs.length} runs`}>
      {[...runs].reverse().map((item) => (
        <i
          key={item.id}
          className={`run-mark ${runTone(item.status)}`}
          title={`${item.status} · ${formatDateTime(item.started_at)}${item.duration_ms != null ? ` · ${formatDuration(item.duration_ms)}` : ''}`}
        />
      ))}
    </span>
  )
}

/** How a task's last run went: running (live), or its status, when and how long it took. */
function LastRunCell({ run }: { run: DevScheduleRun | null }) {
  if (!run) return <span className="small dim">not run yet</span>
  if (run.status === 'running') {
    return (
      <Cluster gap={1}>
        <LiveDot /> <span className="small">running since {relativeTime(run.started_at)}</span>
      </Cluster>
    )
  }
  return (
    <Stack gap={0}>
      <Cluster gap={1}>
        <Badge tone={runTone(run.status)}>{run.status}</Badge>
        <span className="small" title={formatDateTime(run.started_at)}>
          {relativeTime(run.finished_at ?? run.started_at)}
        </span>
        {run.duration_ms != null ? <span className="small dim">· {formatDuration(run.duration_ms)}</span> : null}
        {run.trigger === 'manual' ? <span className="small dim">· run now</span> : null}
      </Cluster>
      {run.status === 'failed' && run.error ? (
        <span className="small danger-text truncate task-error" title={run.error}>
          {run.error}
        </span>
      ) : null}
    </Stack>
  )
}
