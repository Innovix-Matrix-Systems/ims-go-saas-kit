import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { useEffect, useMemo, useRef, useState, type MouseEvent } from 'react'
import type { ColumnDef } from '@tanstack/react-table'
import { api, apiBlob } from '../api/client'
import type {
  CursorPage,
  DevBackupCleanResult,
  DevBackupConfig,
  DevBackupRun,
  DevBackupStats,
} from '../api/types'
import { DataTable } from '../components/DataTable'
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
  Grid,
  LiveDot,
  LoadingBlock,
  PageHeader,
  Spinner,
  Stack,
  StatTile,
  type BadgeTone,
  type StatTone,
} from '../components/ui'
import { formatBytes, formatDateTime, relativeTime } from '../format'

export const Route = createFileRoute('/_app/backups')({
  component: BackupsPage,
})

const statusTones: Record<string, BadgeTone> = {
  completed: 'ok',
  partial: 'warn',
  failed: 'danger',
  running: 'accent',
  pending: 'neutral',
}

/** A run's status; a running one gets the live dot. */
function RunStatus({ status }: { status: string }) {
  return (
    <Cluster gap={1}>
      {status === 'running' ? <LiveDot /> : null}
      <Badge tone={statusTones[status] ?? 'neutral'}>{status}</Badge>
    </Cluster>
  )
}

function BackupsPage() {
  const { push } = useToast()
  const queryClient = useQueryClient()
  const [status, setStatus] = useState('')
  const [selected, setSelected] = useState<DevBackupRun | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<DevBackupRun | null>(null)
  const [cleanPreview, setCleanPreview] = useState<DevBackupCleanResult | null>(null)

  const stats = useQuery({
    queryKey: ['backups', 'stats'],
    queryFn: ({ signal }) => api<DevBackupStats>('/dev/api/backups/stats', { signal }),
  })

  const config = useQuery({
    queryKey: ['backups', 'config'],
    queryFn: ({ signal }) => api<DevBackupConfig>('/dev/api/backups/config', { signal }),
    staleTime: 60_000,
  })

  const runs = useInfiniteQuery({
    queryKey: ['backups', 'list', status],
    queryFn: ({ pageParam, signal }) =>
      api<CursorPage<DevBackupRun>>('/dev/api/backups', {
        query: { limit: 25, cursor: pageParam || undefined, status: status || undefined },
        signal,
        unwrap: false,
      }),
    initialPageParam: '',
    getNextPageParam: (lastPage) => lastPage.meta?.next_cursor || undefined,
    refetchInterval: (query) => {
      const pages = query.state.data?.pages ?? []
      const active = pages.some((page) =>
        page.data.some((run) => run.status === 'pending' || run.status === 'running'),
      )
      return active ? 3_000 : false
    },
  })

  const rows = useMemo(() => runs.data?.pages.flatMap((page) => page.data) ?? [], [runs.data])
  const lastSuccess = rows.find((run) => run.status === 'completed' || run.status === 'partial')
  const active = rows.some((run) => run.status === 'pending' || run.status === 'running')
  const enabled = config.data?.enabled ?? false

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['backups'] })
    void queryClient.invalidateQueries({ queryKey: ['actions'] })
    void queryClient.invalidateQueries({ queryKey: ['overview'] })
  }

  const create = useMutation({
    mutationFn: () => api<DevBackupRun>('/dev/api/backups', { method: 'POST' }),
    onSuccess: (run) => {
      push(
        run.status === 'completed'
          ? `Backup #${run.id} completed (${formatBytes(run.size_bytes)})`
          : `Backup #${run.id} queued`,
        'success',
      )
      invalidate()
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Unable to create a backup', 'error'),
  })

  const remove = useMutation({
    mutationFn: (id: number) => api(`/dev/api/backups/${id}`, { method: 'DELETE' }),
    onSuccess: (_result, id) => {
      push('Backup deleted', 'success')
      setConfirmDelete(null)
      if (selected?.id === id) setSelected(null)
      invalidate()
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Unable to delete the backup', 'error'),
  })

  const clean = useMutation({
    mutationFn: (dryRun: boolean) =>
      api<DevBackupCleanResult>('/dev/api/backups/clean', {
        method: 'POST',
        body: { dry_run: dryRun },
      }),
    onSuccess: (result) => {
      if (result.dry_run) {
        if (result.candidates === 0) {
          push('No backups are past the retention window', 'info')
          return
        }
        setCleanPreview(result)
        return
      }
      setCleanPreview(null)
      push(`Deleted ${result.deleted} backup${result.deleted === 1 ? '' : 's'}`, 'success')
      invalidate()
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Backup cleanup failed', 'error'),
  })

  const download = useMutation({
    mutationFn: async (run: DevBackupRun) => {
      const blob = await apiBlob(`/dev/api/backups/${run.id}/download`)
      const url = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = url
      anchor.download = run.filename
      document.body.appendChild(anchor)
      anchor.click()
      anchor.remove()
      window.setTimeout(() => URL.revokeObjectURL(url), 1_000)
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Unable to download the backup', 'error'),
  })

  const askDelete = (event: MouseEvent, run: DevBackupRun) => {
    event.stopPropagation()
    setConfirmDelete(run)
  }

  const columns = useMemo<ColumnDef<DevBackupRun, any>[]>(
    () => [
      {
        id: 'created',
        header: 'Created',
        accessorFn: (row) => row.created_at,
        enableSorting: false,
        cell: ({ row }) => (
          <span className="small dim nowrap" title={formatDateTime(row.original.created_at)}>
            {relativeTime(row.original.created_at)}
          </span>
        ),
      },
      {
        id: 'status',
        header: 'Status',
        accessorFn: (row) => row.status,
        enableSorting: false,
        cell: ({ row }) => <RunStatus status={row.original.status} />,
      },
      {
        id: 'filename',
        header: 'Archive',
        accessorFn: (row) => row.filename,
        enableSorting: false,
        cell: ({ row }) => (
          <div className="min-0">
            <div className="mono small">{row.original.filename}</div>
            {row.original.status === 'failed' && row.original.error ? (
              <div className="small danger-text truncate run-error" title={row.original.error}>
                {row.original.error}
              </div>
            ) : (
              <div className="small dim">
                {row.original.scope}
                {row.original.pg_dump_version ? ` · pg_dump ${row.original.pg_dump_version}` : ''}
              </div>
            )}
          </div>
        ),
      },
      {
        id: 'trigger',
        header: 'Trigger',
        accessorFn: (row) => row.trigger,
        enableSorting: false,
        cell: ({ row }) => (
          <div>
            <div className="small">{row.original.trigger}</div>
            <div className="small dim">
              {row.original.actor_user_id ? `user #${row.original.actor_user_id}` : 'system'}
            </div>
          </div>
        ),
      },
      {
        id: 'size',
        header: 'Size',
        accessorFn: (row) => row.size_bytes,
        enableSorting: false,
        cell: ({ row }) => <span className="num small">{formatBytes(row.original.size_bytes)}</span>,
      },
      {
        id: 'destinations',
        header: 'Destinations',
        enableSorting: false,
        cell: ({ row }) => (
          <Cluster gap={1}>
            {row.original.destinations.length === 0 ? (
              <span className="small dim">—</span>
            ) : (
              row.original.destinations.map((destination) => (
                <Badge
                  key={destination.destination}
                  tone={destination.status === 'completed' ? 'neutral' : 'danger'}
                  title={destination.status}
                >
                  {destination.destination}
                </Badge>
              ))
            )}
          </Cluster>
        ),
      },
      {
        id: 'actions',
        header: '',
        enableSorting: false,
        cell: ({ row }) => (
          <Cluster gap={1} justify="end" className="row-actions">
            <Button
              size="sm"
              variant="ghost"
              icon="download"
              title="Download"
              disabled={row.original.status !== 'completed' && row.original.status !== 'partial'}
              onClick={(event) => {
                event.stopPropagation()
                download.mutate(row.original)
              }}
            />
            <Button
              size="sm"
              variant="ghost"
              icon="trash"
              title="Delete"
              disabled={row.original.status === 'running' || row.original.status === 'pending'}
              onClick={(event) => askDelete(event, row.original)}
            />
          </Cluster>
        ),
      },
    ],
    [],
  )

  // Load the next page when the end of the list scrolls into view.
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

  const lastTone: StatTone | undefined =
    stats.data?.status === 'failed' ? 'danger' : stats.data?.status === 'warning' ? 'warn' : undefined

  return (
    <>
      <PageHeader
        title="Backups"
        description="PostgreSQL archives kept locally and, optionally, on S3 or R2. Every run is recorded."
        meta={
          active ? (
            <>
              <LiveDot /> A backup is running; the list follows it.
            </>
          ) : undefined
        }
        actions={
          <>
            <Button
              variant="primary"
              icon="backup"
              disabled={!enabled || create.isPending}
              title={enabled ? 'Create a backup now' : 'Set BACKUP_ENABLED=true to enable backups'}
              onClick={() => create.mutate()}
            >
              {create.isPending ? 'Starting…' : 'Run backup'}
            </Button>
            <Button
              icon="trash"
              disabled={clean.isPending || !enabled}
              title="Shows what the retention policy would delete, then asks"
              onClick={() => clean.mutate(true)}
            >
              Clean old backups
            </Button>
            <Button
              icon="refresh"
              onClick={() => {
                void queryClient.invalidateQueries({ queryKey: ['backups'] })
              }}
              disabled={runs.isFetching}
            >
              Refresh
            </Button>
          </>
        }
      />

      <Stack gap={4}>
        <Grid min="sm" gap={3}>
          <StatTile
            label="Last backup"
            value={stats.data?.last_success_at ? relativeTime(stats.data.last_success_at) : 'Never'}
            tone={lastTone}
            hint={stats.data?.summary ?? 'Loading…'}
          />
          <StatTile label="Last size" value={formatBytes(lastSuccess?.size_bytes)} hint="the newest good archive" />
          <StatTile label="Stored" value={formatBytes(stats.data?.total_size ?? 0)} hint="all kept archives" />
          <StatTile
            label="Successful runs"
            value={stats.data?.successful_runs ?? 0}
            hint={`of ${stats.data?.total_runs ?? 0} runs`}
          />
        </Grid>

        {config.isError ? (
          <ErrorBox message={config.error instanceof Error ? config.error.message : 'Unable to load backup configuration.'} />
        ) : config.data ? (
          <SetupStrip config={config.data} />
        ) : null}

        <Card
          title="Runs"
          description={`Newest first · ${rows.length}${runs.hasNextPage ? ' loaded, more as you scroll' : ''}`}
          flush
          actions={
            <Select
              size="sm"
              value={status}
              onChange={setStatus}
              ariaLabel="Status"
              options={[
                { value: '', label: 'All statuses' },
                { value: 'completed', label: 'Completed' },
                { value: 'partial', label: 'Partial' },
                { value: 'failed', label: 'Failed' },
                { value: 'running', label: 'Running' },
                { value: 'pending', label: 'Pending' },
              ]}
            />
          }
        >
          {runs.isLoading ? (
            <LoadingBlock label="Loading backup runs…" />
          ) : runs.isError ? (
            <ErrorBox
              message={runs.error instanceof Error ? runs.error.message : 'Unable to load backup runs.'}
              onRetry={() => void runs.refetch()}
            />
          ) : (
            <DataTable
              data={rows}
              columns={columns}
              onRowClick={(run) => setSelected(run)}
              empty={
                <EmptyState
                  title="No backups yet"
                  description={enabled ? 'Run the first one now; the schedule takes over from there.' : 'Turn backups on with BACKUP_ENABLED=true.'}
                  icon="backup"
                  action={
                    enabled ? (
                      <Button variant="primary" icon="backup" disabled={create.isPending} onClick={() => create.mutate()}>
                        Run backup
                      </Button>
                    ) : undefined
                  }
                />
              }
            />
          )}
        </Card>
        {/* Scrolling to the end of the list loads the next, older page. */}
        <div ref={moreRef} className="load-more">
          {runs.isFetchingNextPage ? <Spinner /> : null}
          {!runs.hasNextPage && rows.length > 0 ? <span className="small dim">That is every run.</span> : null}
        </div>
      </Stack>

      <Drawer
        open={selected !== null}
        title={selected?.filename ?? 'Backup'}
        subtitle={
          selected
            ? `${selected.status} · ${formatBytes(selected.size_bytes)} · ${selected.trigger}`
            : undefined
        }
        onClose={() => setSelected(null)}
        actions={
          selected ? (
            <>
              <Button
                size="sm"
                icon="download"
                disabled={
                  download.isPending ||
                  (selected.status !== 'completed' && selected.status !== 'partial')
                }
                onClick={() => download.mutate(selected)}
              >
                Download
              </Button>
              <Button
                size="sm"
                variant="danger"
                icon="trash"
                disabled={selected.status === 'running' || selected.status === 'pending'}
                onClick={(event) => askDelete(event, selected)}
              >
                Delete
              </Button>
            </>
          ) : undefined
        }
      >
        {selected ? (
          <Stack gap={4}>
            {selected.error ? <ErrorBox message={selected.error} /> : null}
            <dl className="kv">
              <dt>Status</dt>
              <dd>
                <RunStatus status={selected.status} />
              </dd>
              <dt>Run ID</dt>
              <dd className="mono">#{selected.id}</dd>
              <dt>Trigger</dt>
              <dd>
                {selected.trigger}
                {selected.actor_user_id ? ` · user #${selected.actor_user_id}` : ' · system'}
              </dd>
              <dt>Scope</dt>
              <dd>{selected.scope}</dd>
              <dt>Database</dt>
              <dd className="mono small">{selected.database_name || '—'}</dd>
              <dt>pg_dump</dt>
              <dd>{selected.pg_dump_version || '—'}</dd>
              <dt>Size</dt>
              <dd>{formatBytes(selected.size_bytes)}</dd>
              <dt>SHA-256</dt>
              <dd className="mono small break">{selected.checksum_sha256 || '—'}</dd>
              <dt>Created</dt>
              <dd>{formatDateTime(selected.created_at)}</dd>
              <dt>Started</dt>
              <dd>{selected.started_at ? formatDateTime(selected.started_at) : '—'}</dd>
              <dt>Finished</dt>
              <dd>{selected.finished_at ? formatDateTime(selected.finished_at) : '—'}</dd>
              <dt>UUID</dt>
              <dd className="mono small break">{selected.uuid}</dd>
            </dl>

            <Card title="Destinations" description="Each destination is tried on its own." flush>
              {selected.destinations.length === 0 ? (
                <div className="small dim card-body">No destination results were recorded.</div>
              ) : (
                <ul className="attention-list">
                  {selected.destinations.map((destination) => (
                    <li key={destination.destination} className="destination-row">
                      <Cluster gap={2} justify="between">
                        <Cluster gap={2}>
                          <Badge tone={destination.status === 'completed' ? 'ok' : 'danger'}>{destination.destination}</Badge>
                          <span className="small dim">{destination.status}</span>
                        </Cluster>
                        <span className="small dim num">{formatBytes(destination.size_bytes)}</span>
                      </Cluster>
                      {destination.path ? <div className="mono small break">{destination.path}</div> : null}
                      {destination.key ? <div className="mono small break">{destination.key}</div> : null}
                      {destination.error ? <div className="small danger-text">{destination.error}</div> : null}
                    </li>
                  ))}
                </ul>
              )}
            </Card>
          </Stack>
        ) : null}
      </Drawer>

      <ConfirmDialog
        open={confirmDelete !== null}
        title={`Delete ${confirmDelete?.filename ?? 'backup'}?`}
        description="The archive is removed from local storage and every recorded remote destination. The run remains in the audit log."
        confirmLabel="Delete backup"
        danger
        busy={remove.isPending}
        onCancel={() => setConfirmDelete(null)}
        onConfirm={() => {
          if (confirmDelete) remove.mutate(confirmDelete.id)
        }}
      />

      <ConfirmDialog
        open={cleanPreview !== null}
        title="Clean old backups"
        description={
          cleanPreview
            ? `${cleanPreview.candidates} backup(s) are past the retention window. This removes them from every destination and marks the runs deleted.`
            : undefined
        }
        confirmLabel="Delete expired backups"
        danger
        busy={clean.isPending}
        onCancel={() => setCleanPreview(null)}
        onConfirm={() => clean.mutate(false)}
      />
    </>
  )
}

/** The backup setup in one compact strip: where archives go, when, and how long they stay. */
function SetupStrip({ config }: { config: DevBackupConfig }) {
  const retention = [
    `all for ${config.keep_all_days} d`,
    `daily for ${config.keep_daily_days} d`,
    `weekly for ${config.keep_weekly_weeks} w`,
    `monthly for ${config.keep_monthly_months} mo`,
    `yearly for ${config.keep_yearly_years} y`,
    ...(config.max_total_mb > 0 ? [`at most ${config.max_total_mb} MB`] : []),
  ]
  return (
    <Card tight className="setup-strip">
      <dl className="setup-pairs">
        <div>
          <dt>Backups</dt>
          <dd>
            <Badge tone={config.enabled ? 'ok' : 'warn'}>{config.enabled ? 'on' : 'off'}</Badge>
          </dd>
        </div>
        <div>
          <dt>Destinations</dt>
          <dd>
            <Cluster gap={1}>
              {config.destinations.map((destination) => (
                <Badge key={destination}>{destination}</Badge>
              ))}
            </Cluster>
          </dd>
        </div>
        <div>
          <dt>Directory</dt>
          <dd className="mono">{config.directory}</dd>
        </div>
        <div>
          <dt>Remote prefix</dt>
          <dd className="mono">{config.s3_prefix || '—'}</dd>
        </div>
        <div>
          <dt>pg_dump</dt>
          <dd className={config.pg_dump_available ? 'mono' : 'danger-text'} title={config.pg_dump_path}>
            {config.pg_dump_available ? config.pg_dump_version : 'not found: install the PostgreSQL client tools'}
          </dd>
        </div>
        <div>
          <dt>Runs</dt>
          <dd className="mono">{config.run_cron}</dd>
        </div>
        <div>
          <dt>Cleans</dt>
          <dd className="mono">{config.clean_cron}</dd>
        </div>
        <div>
          <dt>Warns after</dt>
          <dd>{config.max_age_hours} h</dd>
        </div>
        <div>
          <dt>Includes</dt>
          <dd>{config.include_files ? `database and ${config.files_paths.join(', ') || 'files'}` : 'database only'}</dd>
        </div>
        <div className="wide">
          <dt>
            Keeps{' '}
            <span
              className="setup-hint"
              title="The newest archive is never deleted. Cleanup removes an archive from every destination before it marks the run deleted, and shows how many it would remove first."
            >
              (?)
            </span>
          </dt>
          <dd>{retention.join(' · ')}</dd>
        </div>
      </dl>
    </Card>
  )
}
