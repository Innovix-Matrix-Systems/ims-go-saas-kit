import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useCallback, useEffect, useState } from 'react'
import { useT } from '../i18n'
import { useDismiss } from './inputs'
import { fetchJobs, isRunning, jobStatus, jobsKey, refreshWhileRunning, statusTones, useJobActions, useJobDates, useJobName } from './jobs'
import { Badge, Button, Icon } from './ui'

/**
 * The top bar's exports & imports: a count while something runs, and the
 * latest jobs with their progress and downloads. Opens by itself when an
 * export starts.
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
  useEffect(() => {
    const show = () => setOpen(true)
    window.addEventListener('jobs:open', show)
    return () => window.removeEventListener('jobs:open', show)
  }, [])

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
                      <span className="small muted">{when(job.created_at)}</span>
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
    </div>
  )
}
