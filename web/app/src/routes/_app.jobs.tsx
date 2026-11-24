import { createFileRoute } from '@tanstack/react-router'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { api, type User } from '../api/client'
import { can, useSession } from '../auth/session'
import { Select } from '../components/inputs'
import { fetchJobs, isRunning, jobStatus, jobsKey, refreshWhileRunning, statusTones, useJobActions, useJobDates, useJobName, type Job } from '../components/jobs'
import { Pagination } from '../components/Pagination'
import { TableSkeleton } from '../components/Skeleton'
import { Avatar, Badge, Button, Card, ConfirmDialog, Drawer, EmptyState, PageHeader } from '../components/ui'
import { useT } from '../i18n'

export const Route = createFileRoute('/_app/jobs')({
  component: JobsPage,
})

interface RowError {
  row?: number
  errors?: Record<string, string[]>
}

const jobTypes = ['export', 'import', 'privacy_export'] as const
const jobStatuses = ['pending', 'processing', 'completed', 'failed'] as const

/**
 * The exports and imports the signed-in person started, while they last;
 * those who may see every job (data_processing_job.view.all) switch to all
 * of them and narrow them by member, type and status.
 */
function JobsPage() {
  const t = useT()
  const session = useSession()
  const name = useJobName()
  const when = useJobDates()
  const actions = useJobActions()
  const canSeeAll = can(session, 'data_processing_job.view.all')
  const [everyone, setEveryone] = useState(false)
  const [userId, setUserId] = useState('')
  const [type, setType] = useState('')
  const [status, setStatus] = useState('')
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState(20)
  const [removing, setRemoving] = useState<Job | null>(null)
  const [details, setDetails] = useState<Job | null>(null)
  const showAll = canSeeAll && everyone
  const filters = { everyone: showAll, userId: userId ? Number(userId) : undefined, type, status }
  const jobs = useQuery({
    queryKey: [...jobsKey, 'page', page, perPage, filters],
    queryFn: () => fetchJobs(page, perPage, filters),
    placeholderData: keepPreviousData,
    refetchInterval: (query) => refreshWhileRunning(query.state.data),
  })
  const members = useQuery({
    queryKey: ['jobs', 'members'],
    queryFn: () => api<User[]>('/user', { query: { per_page: 100, order_by: 'name', order_direction: 'asc' } }),
    enabled: showAll && can(session, 'user.view.all'),
    staleTime: 60_000,
  })
  const items = jobs.data?.data ?? []
  const filtered = showAll || !!type || !!status
  const pick = (set: (value: string) => void) => (value: string) => {
    set(value)
    setPage(1)
  }

  return (
    <>
      <PageHeader title={t('jobs.title')} description={t('jobs.subtitle')} />
      <div className="toolbar">
        {canSeeAll ? (
          <div className="segmented">
            {([false, true] as const).map((value) => (
              <button
                key={String(value)}
                type="button"
                className={everyone === value ? 'active' : ''}
                onClick={() => {
                  setEveryone(value)
                  setUserId('')
                  setPage(1)
                }}
              >
                {t(value ? 'jobs.everyone' : 'jobs.mine')}
              </button>
            ))}
          </div>
        ) : null}
        {showAll && members.data ? (
          <Select
            value={userId}
            onChange={pick(setUserId)}
            ariaLabel={t('jobs.colBy')}
            searchable
            options={[
              { value: '', label: t('jobs.anyone') },
              ...members.data.map((member) => ({ value: String(member.id), label: member.name, text: `${member.name} ${member.email}` })),
            ]}
          />
        ) : null}
        <Select
          value={type}
          onChange={pick(setType)}
          ariaLabel={t('jobs.anyType')}
          options={[{ value: '', label: t('jobs.anyType') }, ...jobTypes.map((value) => ({ value, label: t(`jobs.type.${value}`) }))]}
        />
        <Select
          value={status}
          onChange={pick(setStatus)}
          ariaLabel={t('jobs.anyStatus')}
          options={[{ value: '', label: t('jobs.anyStatus') }, ...jobStatuses.map((value) => ({ value, label: t(`jobs.status.${value}`) }))]}
        />
      </div>
      <Card flush>
        {jobs.isLoading ? (
          <TableSkeleton columns={showAll ? 6 : 5} avatar={false} />
        ) : items.length === 0 ? (
          filtered ? (
            <EmptyState icon="download" title={t('jobs.noneFiltered')} />
          ) : (
            <EmptyState icon="download" title={t('jobs.none')} description={t('jobs.noneHint')} />
          )
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>{t('jobs.colWhat')}</th>
                  {showAll ? <th>{t('jobs.colBy')}</th> : null}
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
                      {showAll ? (
                        <td>
                          {job.user_name ? (
                            <span className="member-cell">
                              <Avatar name={job.user_name} size="sm" />
                              <span className="nowrap">{job.user_id === session?.user.id ? t('jobs.you') : job.user_name}</span>
                            </span>
                          ) : (
                            <span className="muted">—</span>
                          )}
                        </td>
                      ) : null}
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
