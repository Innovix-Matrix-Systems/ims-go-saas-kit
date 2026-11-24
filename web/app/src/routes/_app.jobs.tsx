import { createFileRoute } from '@tanstack/react-router'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { fetchJobs, isRunning, jobStatus, jobsKey, refreshWhileRunning, statusTones, useJobActions, useJobDates, useJobName, type Job } from '../components/jobs'
import { Pagination } from '../components/Pagination'
import { TableSkeleton } from '../components/Skeleton'
import { Badge, Button, Card, ConfirmDialog, Drawer, EmptyState, PageHeader } from '../components/ui'
import { useT } from '../i18n'

export const Route = createFileRoute('/_app/jobs')({
  component: JobsPage,
})

interface RowError {
  row?: number
  errors?: Record<string, string[]>
}

/** Every export and import the signed-in person started, while they last. */
function JobsPage() {
  const t = useT()
  const name = useJobName()
  const when = useJobDates()
  const actions = useJobActions()
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState(20)
  const [removing, setRemoving] = useState<Job | null>(null)
  const [details, setDetails] = useState<Job | null>(null)
  const jobs = useQuery({
    queryKey: [...jobsKey, 'page', page, perPage],
    queryFn: () => fetchJobs(page, perPage),
    placeholderData: keepPreviousData,
    refetchInterval: (query) => refreshWhileRunning(query.state.data),
  })
  const items = jobs.data?.data ?? []

  return (
    <>
      <PageHeader title={t('jobs.title')} description={t('jobs.subtitle')} />
      <Card flush>
        {jobs.isLoading ? (
          <TableSkeleton columns={5} avatar={false} />
        ) : items.length === 0 ? (
          <EmptyState icon="download" title={t('jobs.none')} description={t('jobs.noneHint')} />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>{t('jobs.colWhat')}</th>
                  <th>{t('jobs.colStatus')}</th>
                  <th>{t('jobs.colStarted')}</th>
                  <th>{t('jobs.colAvailable')}</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {items.map((job) => {
                  const status = jobStatus(job)
                  const rowErrors = Array.isArray(job.errors) ? (job.errors as RowError[]) : []
                  return (
                    <tr key={job.id}>
                      <td>
                        <span className="member-meta">
                          <span className="member-name">{name(job)}</span>
                          {job.file_name ? <span className="small muted mono">{job.file_name}</span> : null}
                        </span>
                      </td>
                      <td>
                        <div className="jobs-status-cell">
                          <Badge tone={statusTones[status]}>{t(`jobs.status.${status}`)}</Badge>
                          {isRunning(job) ? (
                            <>
                              <div className="meter" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={job.progress_percentage}>
                                <span className="meter-fill" style={{ width: `${Math.max(4, job.progress_percentage)}%` }} />
                              </div>
                              {job.total_items > 0 ? (
                                <span className="small muted">{t('jobs.progress', { processed: job.processed_items, total: job.total_items })}</span>
                              ) : null}
                            </>
                          ) : null}
                          {status === 'failed' && job.error_message ? <span className="small danger-text">{job.error_message}</span> : null}
                          {job.error_count > 0 ? <span className="small muted">{t('jobs.rowErrors', { count: job.error_count })}</span> : null}
                        </div>
                      </td>
                      <td className="small muted nowrap">{when(job.created_at)}</td>
                      <td className="small muted nowrap">{when(job.expires_at)}</td>
                      <td className="right">
                        <div className="flex justify-end wrap">
                          {status === 'completed' && job.file_name ? (
                            <Button size="sm" variant="primary" icon="download" onClick={() => void actions.download(job)}>
                              {t('jobs.download')}
                            </Button>
                          ) : null}
                          {status === 'failed' ? (
                            <Button size="sm" icon="refresh" onClick={() => void actions.retry(job)}>
                              {t('jobs.retry')}
                            </Button>
                          ) : null}
                          {rowErrors.length > 0 ? (
                            <Button size="sm" variant="ghost" icon="list" onClick={() => setDetails(job)}>
                              {t('jobs.details')}
                            </Button>
                          ) : null}
                          {!isRunning(job) ? (
                            <Button size="sm" variant="ghost" icon="trash" onClick={() => setRemoving(job)}>
                              {t('jobs.remove')}
                            </Button>
                          ) : null}
                        </div>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
        {jobs.data ? (
          <Pagination
            page={page}
            perPage={perPage}
            total={jobs.data.meta.total}
            onPage={setPage}
            onPerPage={(size) => {
              setPerPage(size)
              setPage(1)
            }}
          />
        ) : null}
      </Card>
      <ConfirmDialog
        open={removing != null}
        title={t('jobs.remove')}
        description={removing ? t('jobs.removeConfirm', { what: name(removing).toLowerCase() }) : ''}
        confirmLabel={t('jobs.remove')}
        danger
        onConfirm={() => {
          if (removing) void actions.remove(removing)
          setRemoving(null)
        }}
        onCancel={() => setRemoving(null)}
      />
      {details ? (
        <Drawer open title={t('jobs.errorsTitle')} subtitle={name(details)} onClose={() => setDetails(null)} size="md">
          <ul className="jobs-errors">
            {(details.errors as RowError[]).map((item, index) => (
              <li key={index}>
                <span className="strong">{t('jobs.row', { row: item.row ?? index + 1 })}</span>
                <ul>
                  {Object.entries(item.errors ?? {}).flatMap(([field, messages]) =>
                    messages.map((message, i) => (
                      <li key={`${field}-${i}`} className="small">
                        <span className="muted">{field}:</span> {message}
                      </li>
                    )),
                  )}
                </ul>
              </li>
            ))}
          </ul>
        </Drawer>
      ) : null}
    </>
  )
}
