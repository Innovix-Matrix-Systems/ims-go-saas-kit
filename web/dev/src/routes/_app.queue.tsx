import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { useMemo, useState, type MouseEvent } from 'react'
import type { ColumnDef } from '@tanstack/react-table'
import { api } from '../api/client'
import type { CursorPage, DevJob, DevQueue } from '../api/types'
import { DataTable } from '../components/DataTable'
import { Select } from '../components/inputs'
import { JobFacts, JobState, jobActionCopy, jobCan, jobStateLabels, jobStates, type JobAction, type JobStateName } from '../components/Jobs'
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
  Grid,
  Icon,
  LoadingBlock,
  PageHeader,
  RefreshToggle,
  Stack,
  Tabs,
} from '../components/ui'
import { formatDateTime, relativeTime } from '../format'
import { QueueChart } from '../components/PanelCharts'

type StateFilter = '' | JobStateName

export const Route = createFileRoute('/_app/queue')({
  validateSearch: (search: Record<string, unknown>): { state?: StateFilter } => ({
    state: jobStates.includes(search.state as JobStateName) ? (search.state as StateFilter) : undefined,
  }),
  component: QueuePage,
})

interface ConfirmState {
  title: string
  description: string
  confirmLabel: string
  danger?: boolean
  run: () => Promise<unknown>
}

function QueuePage() {
  const { push } = useToast()
  const queryClient = useQueryClient()
  const navigate = useNavigate({ from: '/queue' })
  const stateFilter: StateFilter = Route.useSearch().state ?? ''
  const [queueFilter, setQueueFilter] = useState('')
  const [kind, setKind] = useState('')
  const [live, setLive] = useState(false)
  const [selectedJob, setSelectedJob] = useState<number | null>(null)
  const [confirm, setConfirm] = useState<ConfirmState | null>(null)
  const [confirmBusy, setConfirmBusy] = useState(false)

  const setStateFilter = (state: StateFilter) => void navigate({ search: { state: state || undefined }, replace: true })

  const queues = useQuery({
    queryKey: ['queues'],
    queryFn: ({ signal }) => api<DevQueue[]>('/dev/api/queue/queues', { signal }),
    refetchInterval: live ? 5000 : false,
    retry: false,
  })

  const jobs = useInfiniteQuery({
    queryKey: ['jobs', { queueFilter, stateFilter, kind }],
    queryFn: ({ pageParam, signal }) =>
      api<CursorPage<DevJob>>('/dev/api/queue/jobs', {
        query: {
          limit: 25,
          cursor: pageParam || undefined,
          queue: queueFilter || undefined,
          state: stateFilter || undefined,
          kind: kind || undefined,
        },
        signal,
        unwrap: false,
      }),
    initialPageParam: '',
    getNextPageParam: (lastPage) => lastPage.meta?.next_cursor || undefined,
    refetchInterval: live ? 5000 : false,
  })

  const jobDetail = useQuery({
    queryKey: ['job', selectedJob],
    queryFn: ({ signal }) => api<DevJob>(`/dev/api/queue/jobs/${selectedJob}`, { signal }),
    enabled: selectedJob !== null,
  })

  const rows = useMemo(() => jobs.data?.pages.flatMap((page) => page.data) ?? [], [jobs.data])
  const queueRows = queues.data ?? []

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['queues'] })
    void queryClient.invalidateQueries({ queryKey: ['jobs'] })
    void queryClient.invalidateQueries({ queryKey: ['job'] })
    void queryClient.invalidateQueries({ queryKey: ['actions'] })
    void queryClient.invalidateQueries({ queryKey: ['overview'] })
  }

  const queueAction = useMutation({
    mutationFn: ({ name, action }: { name: string; action: 'pause' | 'resume' }) =>
      api(`/dev/api/queue/queues/${encodeURIComponent(name)}/${action}`, { method: 'POST' }),
    onSuccess: (_result, variables) => {
      push(`Queue ${variables.name} ${variables.action === 'pause' ? 'paused' : 'resumed'}`, 'success')
      invalidate()
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Queue action failed', 'error'),
  })

  const jobAction = useMutation({
    mutationFn: ({ id, action }: { id: number; action: JobAction }) => {
      if (action === 'delete') return api(`/dev/api/queue/jobs/${id}`, { method: 'DELETE' })
      return api(`/dev/api/queue/jobs/${id}/${action}`, { method: 'POST' })
    },
    onSuccess: (_result, variables) => {
      const done = { retry: 'queued again', cancel: 'cancelled', delete: 'deleted' }[variables.action]
      push(`Job #${variables.id} ${done}`, 'success')
      if (variables.action === 'delete') setSelectedJob(null)
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Job action failed', 'error'),
    onSettled: invalidate,
  })

  // Every job action asks first, from the table and from the drawer alike.
  const askJob = (job: DevJob, action: JobAction, event?: MouseEvent) => {
    event?.stopPropagation()
    const copy = jobActionCopy(job, action)
    setConfirm({ ...copy, confirmLabel: copy.title, run: () => jobAction.mutateAsync({ id: job.id, action }) })
  }

  const columns = useMemo<ColumnDef<DevJob, any>[]>(
    () => [
      {
        id: 'id',
        header: 'Job',
        accessorFn: (row) => row.id,
        cell: ({ row }) => <span className="mono small">#{row.original.id}</span>,
      },
      {
        id: 'kind',
        header: 'Kind',
        accessorFn: (row) => row.kind,
        cell: ({ row }) => <span className="mono small">{row.original.kind}</span>,
      },
      {
        id: 'queue',
        header: 'Queue',
        accessorFn: (row) => row.queue,
        cell: ({ row }) => <span className="small muted">{row.original.queue}</span>,
      },
      {
        id: 'state',
        header: 'State',
        accessorFn: (row) => row.state,
        cell: ({ row }) => <JobState state={row.original.state} />,
      },
      {
        id: 'attempt',
        header: 'Attempt',
        accessorFn: (row) => row.attempt,
        cell: ({ row }) => (
          <span className="num small">
            {row.original.attempt}/{row.original.max_attempts}
          </span>
        ),
      },
      {
        id: 'created',
        header: 'Created',
        accessorFn: (row) => row.created_at,
        cell: ({ row }) => (
          <span className="small dim nowrap" title={formatDateTime(row.original.created_at)}>
            {relativeTime(row.original.created_at)}
          </span>
        ),
      },
      {
        id: 'actions',
        header: '',
        enableSorting: false,
        cell: ({ row }) => (
          <Cluster gap={1} justify="end" className="row-actions">
            {jobCan('retry', row.original.state) ? (
              <Button size="sm" variant="ghost" title="Retry" aria-label="Retry" onClick={(event) => askJob(row.original, 'retry', event)}>
                <Icon name="refresh" />
              </Button>
            ) : null}
            {jobCan('cancel', row.original.state) ? (
              <Button size="sm" variant="ghost" title="Cancel" aria-label="Cancel" onClick={(event) => askJob(row.original, 'cancel', event)}>
                <Icon name="pause" />
              </Button>
            ) : null}
            {jobCan('delete', row.original.state) ? (
              <Button size="sm" variant="ghost" title="Delete" aria-label="Delete" onClick={(event) => askJob(row.original, 'delete', event)}>
                <Icon name="trash" />
              </Button>
            ) : null}
          </Cluster>
        ),
      },
    ],
    [],
  )

  const totals = queueRows.reduce(
    (sum, queue) => ({ active: sum.active + queue.active, failed: sum.failed + queue.failed }),
    { active: 0, failed: 0 },
  )

  return (
    <>
      <PageHeader
        title="Queue"
        description="River's queues and jobs: what is waiting, running and failing, with retry, cancel and pause."
        meta={queueRows.length > 0 ? `${totals.active.toLocaleString()} waiting or running · ${totals.failed.toLocaleString()} failed` : undefined}
        actions={
          <>
            <RefreshToggle enabled={live} onChange={setLive} />
            <Button
              icon="refresh"
              onClick={() => {
                void queues.refetch()
                void jobs.refetch()
              }}
              disabled={queues.isFetching || jobs.isFetching}
            >
              Refresh
            </Button>
          </>
        }
      />

      <Stack gap={5}>
        {queues.isError ? (
          <ErrorBox message={queues.error instanceof Error ? queues.error.message : 'Queue administration is unavailable (QUEUE_DRIVER=sync?).'} />
        ) : queues.isLoading ? (
          <LoadingBlock label="Loading queues…" />
        ) : queueRows.length === 0 ? (
          <Card flush>
            <EmptyState title="No queues reported" description="A queue appears once a job is queued on it." icon="queue" />
          </Card>
        ) : (
          <Grid min="md" gap={3}>
            {queueRows.map((queue) => (
              <div key={queue.name} className={`stat-card queue-tile ${queue.paused_at ? 'paused' : ''}`}>
                <Cluster gap={2} justify="between">
                  <span className="queue-name">{queue.name}</span>
                  {queue.paused_at ? <Badge tone="warn">paused {relativeTime(queue.paused_at)}</Badge> : null}
                </Cluster>
                <div className="queue-counts">
                  <div>
                    <div className="queue-count">{queue.active.toLocaleString()}</div>
                    <div className="stat-label">waiting or running</div>
                  </div>
                  <div>
                    <div className={`queue-count ${queue.failed > 0 ? 'danger-text' : ''}`}>{queue.failed.toLocaleString()}</div>
                    <div className="stat-label">failed</div>
                  </div>
                  <div>
                    <div className="queue-count muted">{queue.total.toLocaleString()}</div>
                    <div className="stat-label">total</div>
                  </div>
                </div>
                <Cluster gap={2}>
                  {queue.paused_at ? (
                    <Button size="sm" icon="play" onClick={() => queueAction.mutate({ name: queue.name, action: 'resume' })}>
                      Resume
                    </Button>
                  ) : (
                    <Button size="sm" icon="pause" title="Workers stop taking new jobs from this queue" onClick={() => queueAction.mutate({ name: queue.name, action: 'pause' })}>
                      Pause
                    </Button>
                  )}
                  {queue.failed > 0 ? (
                    <Button
                      size="sm"
                      variant="ghost"
                      onClick={() => {
                        setQueueFilter(queue.name)
                        setStateFilter('discarded')
                      }}
                    >
                      Show failed
                    </Button>
                  ) : null}
                </Cluster>
              </div>
            ))}
          </Grid>
        )}
        {queueRows.length > 0 ? <QueueChart queues={queueRows} /> : null}

        <div>
          <Tabs
            tabs={(['', ...jobStates] as StateFilter[]).map((state) => ({ id: state || 'all', label: state ? jobStateLabels[state] : 'All' }))}
            active={stateFilter || 'all'}
            onChange={(id) => setStateFilter(id === 'all' ? '' : (id as StateFilter))}
          />
          <div className="filters">
            <label className="field">
              <span className="field-label">Queue</span>
              <Select
                value={queueFilter}
                onChange={setQueueFilter}
                options={[{ value: '', label: 'Any' }, ...queueRows.map((queue) => ({ value: queue.name, label: queue.name }))]}
              />
            </label>
            <label className="field grow">
              <span className="field-label">Kind</span>
              <input
                className="input mono"
                type="search"
                placeholder="mail.send"
                value={kind}
                onChange={(event) => setKind(event.target.value)}
              />
            </label>
            {queueFilter || kind ? (
              <Button
                variant="ghost"
                icon="x"
                onClick={() => {
                  setQueueFilter('')
                  setKind('')
                }}
              >
                Clear
              </Button>
            ) : null}
          </div>

          {jobs.isLoading ? (
            <LoadingBlock label="Loading jobs…" />
          ) : jobs.isError ? (
            <ErrorBox message={jobs.error instanceof Error ? jobs.error.message : 'Unable to load jobs.'} onRetry={() => void jobs.refetch()} />
          ) : (
            <>
              <DataTable
                data={rows}
                columns={columns}
                onRowClick={(row) => setSelectedJob(row.id)}
                empty={
                  <EmptyState
                    title={stateFilter ? `No ${jobStateLabels[stateFilter].toLowerCase()} jobs` : 'No jobs match'}
                    description="Change the state, queue or kind, or queue a job from the Playground."
                    icon="queue"
                  />
                }
              />
              <Cluster gap={3} className="mt-16">
                {jobs.hasNextPage ? (
                  <Button icon="chevron" onClick={() => void jobs.fetchNextPage()} disabled={jobs.isFetchingNextPage}>
                    {jobs.isFetchingNextPage ? 'Loading…' : 'Load more'}
                  </Button>
                ) : null}
                {rows.length > 0 ? <span className="small dim">{rows.length} jobs</span> : null}
              </Cluster>
            </>
          )}
        </div>
      </Stack>

      <Drawer
        open={selectedJob !== null}
        onClose={() => setSelectedJob(null)}
        size="md"
        title={jobDetail.data ? `Job #${jobDetail.data.id}` : `Job #${selectedJob}`}
        subtitle={jobDetail.data ? `${jobDetail.data.kind} · ${jobDetail.data.queue}` : undefined}
        actions={
          jobDetail.data ? (
            <>
              <Link to="/jobs/$id" params={{ id: String(jobDetail.data.id) }} className="btn sm" onClick={() => setSelectedJob(null)}>
                Open
              </Link>
              {jobCan('retry', jobDetail.data.state) ? (
                <Button size="sm" icon="refresh" onClick={() => jobDetail.data && askJob(jobDetail.data, 'retry')}>
                  Retry
                </Button>
              ) : null}
              {jobCan('cancel', jobDetail.data.state) ? (
                <Button size="sm" variant="danger" icon="pause" onClick={() => jobDetail.data && askJob(jobDetail.data, 'cancel')}>
                  Cancel
                </Button>
              ) : null}
            </>
          ) : null
        }
      >
        {jobDetail.isLoading ? (
          <LoadingBlock label="Loading job…" />
        ) : jobDetail.isError ? (
          <ErrorBox message={jobDetail.error instanceof Error ? jobDetail.error.message : 'Unable to load job.'} />
        ) : jobDetail.data ? (
          <JobFacts job={jobDetail.data} />
        ) : null}
      </Drawer>

      <ConfirmDialog
        open={confirm !== null}
        title={confirm?.title ?? ''}
        description={confirm?.description}
        confirmLabel={confirm?.confirmLabel}
        danger={confirm?.danger}
        busy={confirmBusy}
        onCancel={() => setConfirm(null)}
        onConfirm={() => {
          if (!confirm) return
          setConfirmBusy(true)
          void confirm
            .run()
            .catch(() => undefined)
            .finally(() => {
              setConfirmBusy(false)
              setConfirm(null)
            })
        }}
      />
    </>
  )
}
