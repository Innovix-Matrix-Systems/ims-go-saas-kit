import { useQueryClient } from '@tanstack/react-query'
import { api, apiBlob, saveBlob, type Schemas } from '../api/client'
import { intlLocale, useLocale, useT, type MessageKey } from '../i18n'
import { queryClient } from '../query'
import { sessionQuery, type Session } from '../auth/session'
import { useToast } from './Toasts'
import type { BadgeTone } from './ui'

// Exports and imports run as background jobs (the API's data-processing
// jobs). The tray in the top bar and the Exports & imports page read the
// signed-in person's own jobs from here.

export type Job = Schemas['ProcessingJob']
export interface JobPage {
  data: Job[]
  meta: Schemas['PaginationMeta']
}

/** Query key shared by the tray and the page, so starting an export refreshes both. */
export const jobsKey = ['jobs'] as const

export function fetchJobs(page: number, perPage: number): Promise<JobPage> {
  return api<JobPage>('/data-processing/jobs/user', { envelope: true, query: { page, per_page: perPage } })
}

export const jobStatus = (job: Job) => job.status.toLowerCase() as 'pending' | 'processing' | 'completed' | 'failed'
export const isRunning = (job: Job) => ['pending', 'processing'].includes(jobStatus(job))

export const statusTones: Record<ReturnType<typeof jobStatus>, BadgeTone> = {
  pending: 'neutral',
  processing: 'info',
  completed: 'ok',
  failed: 'danger',
}

/** Poll every few seconds while something runs, otherwise rarely. */
export const refreshWhileRunning = (page?: JobPage) => (page?.data.some(isRunning) ? 3000 : 60_000)

/** "Members export", "Activity log export", … */
export function useJobName() {
  const t = useT()
  return (job: Job) => {
    if (job.type === 'privacy_export') return t('jobs.kind.privacy_export')
    const key = `jobs.kind.${job.type}.${job.entity_type ?? ''}` as MessageKey
    const known = ['jobs.kind.export.user', 'jobs.kind.import.user', 'jobs.kind.export.activity']
    return known.includes(key) ? t(key) : t('jobs.kind.other', { type: job.type })
  }
}

export function useJobDates() {
  const locale = intlLocale(useLocale())
  return (value?: string | null) =>
    value ? new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) : '—'
}

/**
 * Staff not acting in an organization change their own jobs through the
 * internal routes; the organization routes are read-only for them.
 */
function writePath(path: string): string {
  const session = queryClient.getQueryData<Session>(sessionQuery.queryKey)
  return session?.isPlatformUser && !session.acting ? `/internal${path}` : path
}

/** Download, retry and remove, each refreshing the lists and saying what happened. */
export function useJobActions() {
  const t = useT()
  const client = useQueryClient()
  const { push } = useToast()
  const refresh = () => client.invalidateQueries({ queryKey: jobsKey })
  const report = (cause: unknown) => push(cause instanceof Error ? cause.message : String(cause), 'error')
  return {
    download: async (job: Job) => {
      try {
        saveBlob(await apiBlob(`/data-processing/job/${job.job_id}/download`), job.file_name ?? `${job.job_id}.xlsx`)
      } catch {
        // apiBlob reports the error.
      }
    },
    retry: async (job: Job) => {
      try {
        await api(writePath(`/data-processing/job/${job.job_id}/retry`), { method: 'POST' })
        push(t('jobs.retried'), 'success')
        await refresh()
      } catch (cause) {
        report(cause)
      }
    },
    remove: async (job: Job) => {
      try {
        await api(writePath(`/data-processing/job/${job.job_id}`), { method: 'DELETE' })
        push(t('jobs.removed'), 'success')
        await refresh()
      } catch (cause) {
        report(cause)
      }
    },
  }
}

/**
 * Starts an export and says where to find it. `start` calls the API; the
 * tray picks the new job up at once.
 */
export function useStartExport() {
  const t = useT()
  const client = useQueryClient()
  const { push } = useToast()
  return async (what: string, start: () => Promise<unknown>) => {
    try {
      await start()
      push(t('jobs.started', { what }), 'success')
      await client.invalidateQueries({ queryKey: jobsKey })
      window.dispatchEvent(new CustomEvent('jobs:open'))
    } catch (cause) {
      push(cause instanceof Error ? cause.message : String(cause), 'error')
    }
  }
}
