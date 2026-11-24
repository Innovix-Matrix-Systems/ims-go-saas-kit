import { Link, useNavigate } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useCallback, useEffect, useState } from 'react'
import { useT } from '../i18n'
import { useDismiss } from './inputs'
import { fetchJobs, isRunning, jobStatus, jobsKey, refreshWhileRunning, statusTones, useJobActions, useJobDates, useJobName, type QueuedJob } from './jobs'
import { Badge, Button, Icon, Modal } from './ui'

/**
 * The top bar's background jobs: a dot while something runs, and the latest
 * jobs with their progress and downloads. Starting an export shows the
 * queued dialog.
 */
export function JobsTray() {
  const t = useT()
  const name = useJobName()
  const when = useJobDates()
  const actions = useJobActions()
  const [open, setOpen] = useState(false)
  const close = useCallback(() => setOpen(false), [])
  const ref = useDismiss(open, close)
  const jobs = useQuery({
    queryKey: [...jobsKey, 'tray'],
    queryFn: () => fetchJobs(1, 6),
    refetchInterval: (query) => refreshWhileRunning(query.state.data),
  })

  const items = jobs.data?.data ?? []
  const running = items.filter(isRunning).length

  return (
    <div className="appearance" ref={ref}>
      <button
        type="button"
        className="icon-btn jobs-trigger"
        title={t('jobs.tray')}
        aria-label={running ? `${t('jobs.tray')} (${t('jobs.running', { count: running })})` : t('jobs.tray')}
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
      >
        <Icon name="download" />
        {running ? <span className="jobs-dot" aria-hidden="true" /> : null}
      </button>
      {open ? (
        <div className="ui-popover align-right jobs-menu" role="dialog" aria-label={t('jobs.tray')}>
          <div className="jobs-menu-head">
            <span className="strong">{t('jobs.tray')}</span>
            <Link to="/jobs" className="link-btn" onClick={close}>
              {t('jobs.seeAll')}
            </Link>
          </div>
          {items.length === 0 ? (
            <div className="jobs-empty small muted">{t('jobs.none')}</div>
          ) : (
            <ul className="jobs-list">
              {items.map((job) => {
                const status = jobStatus(job)
                return (
                  <li key={job.id} className="jobs-item">
                    <div className="jobs-item-head">
                      <span className="jobs-name">{name(job)}</span>
                      <Badge tone={statusTones[status]}>{t(`jobs.status.${status}`)}</Badge>
                    </div>
                    {isRunning(job) ? (
                      <div className="meter" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={job.progress_percentage}>
                        <span className="meter-fill" style={{ width: `${Math.max(4, job.progress_percentage)}%` }} />
                      </div>
                    ) : null}
                    <div className="jobs-item-foot">
                      <span className="jobs-item-dates small muted">
                        <span>{when(job.created_at)}</span>
                        {job.expires_at ? <span>{t('jobs.availableUntil', { date: when(job.expires_at) })}</span> : null}
                      </span>
                      <span className="spacer" />
                      {status === 'completed' && job.file_name ? (
                        <Button size="sm" variant="ghost" icon="download" onClick={() => void actions.download(job)}>
                          {t('jobs.download')}
                        </Button>
                      ) : null}
                      {status === 'failed' ? (
                        <Button size="sm" variant="ghost" icon="refresh" onClick={() => void actions.retry(job)}>
                          {t('jobs.retry')}
                        </Button>
                      ) : null}
                    </div>
                  </li>
                )
              })}
            </ul>
          )}
        </div>
      ) : null}
      <JobQueuedDialog />
    </div>
  )
}

/** Says an export is queued, with its job ID, and leads to Background jobs. */
function JobQueuedDialog() {
  const t = useT()
  const navigate = useNavigate()
  const [queued, setQueued] = useState<QueuedJob | null>(null)
  useEffect(() => {
    const show = (event: Event) => setQueued((event as CustomEvent<QueuedJob>).detail)
    window.addEventListener('jobs:queued', show)
    return () => window.removeEventListener('jobs:queued', show)
  }, [])
  const close = () => setQueued(null)
  return (
    <Modal
      open={queued != null}
      title={queued ? t('jobs.queuedTitle', { what: queued.what }) : ''}
      onClose={close}
      actions={
        <>
          <Button onClick={close}>{t('jobs.close')}</Button>
          <Button
            variant="primary"
            onClick={() => {
              close()
              void navigate({ to: '/jobs' })
            }}
          >
            {t('jobs.view')}
          </Button>
        </>
      }
    >
      <div className="jobs-queued">
        <div className="jobs-queued-note">
          <Icon name="check" />
          <span>{t('jobs.queuedNote')}</span>
        </div>
        <p className="muted">{t('jobs.queuedBody')}</p>
        {queued?.jobId ? <p className="small muted mono">{t('jobs.queuedId', { id: queued.jobId })}</p> : null}
      </div>
    </Modal>
  )
}
