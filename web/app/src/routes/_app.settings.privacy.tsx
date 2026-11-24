import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { api, apiBlob, saveBlob, ApiError, type Schemas } from '../api/client'
import { forgetSession } from '../auth/session'
import { Field, PasswordInput, useApiErrors } from '../components/form'
import { fetchJobs, isRunning, jobStatus, jobsKey, type Job } from '../components/jobs'
import { useToast } from '../components/Toasts'
import { Button, Card, ErrorBox, Icon, Modal, Spinner } from '../components/ui'
import { intlLocale, useLocale, useT } from '../i18n'

interface PrivacySearch {
  /** The export a "your data is ready" e-mail links to (its job id). */
  export?: string
}

export const Route = createFileRoute('/_app/settings/privacy')({
  validateSearch: (search: Record<string, unknown>): PrivacySearch => ({
    export: typeof search.export === 'string' && search.export !== '' ? search.export : undefined,
  }),
  component: PrivacyPage,
})

type PrivacyRequest = Schemas['PrivacyRequest']

interface ExportJob {
  job_id: string
  status: string
}

function PrivacyPage() {
  return (
    <div className="stack g-4">
      <ExportCard />
      <DeletionCard />
    </div>
  )
}

/**
 * The personal-data export. The page finds the export by itself: the one the
 * e-mail links to (?export=), else the latest one, so it survives a reload and
 * the link in the mail lands on a Download button.
 */
function ExportCard() {
  const t = useT()
  const locale = useLocale()
  const errors = useApiErrors()
  const queryClient = useQueryClient()
  const { export: linked } = Route.useSearch()
  const [password, setPassword] = useState('')
  const [started, setStarted] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const latest = useQuery({
    queryKey: [...jobsKey, 'privacy-latest'],
    queryFn: () => fetchJobs(1, 1, { type: 'privacy_export' }),
    enabled: !linked,
  })
  const jobId = started ?? linked ?? latest.data?.data[0]?.job_id ?? null
  const job = useQuery({
    queryKey: [...jobsKey, 'privacy-export', jobId],
    queryFn: () => api<Job>(`/data-processing/job/${jobId}`),
    enabled: Boolean(jobId),
    retry: false,
    refetchInterval: (query) => (query.state.data && isRunning(query.state.data) ? 3000 : false),
  })

  const start = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    errors.clear()
    try {
      const result = await api<ExportJob>('/user/privacy/export', { method: 'POST', body: { password } })
      setStarted(result.job_id)
      setPassword('')
      void queryClient.invalidateQueries({ queryKey: jobsKey })
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  const download = async () => {
    saveBlob(await apiBlob(`/api/v1/data-processing/job/${jobId}/download`), 'my-data.zip')
  }

  const current = job.data
  const until = current?.expires_at
    ? new Intl.DateTimeFormat(intlLocale(locale), { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(current.expires_at))
    : null
  const form = (
    <form onSubmit={start} className="stack form-narrow">
      <Field label={t('auth.password')} error={errors.field('password')}>
        <PasswordInput name="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
      </Field>
      {errors.box}
      <div>
        <button type="submit" className="btn" disabled={busy || !password}>
          {busy ? <Spinner /> : <Icon name="download" />} {t('privacy.exportStart')}
        </button>
      </div>
    </form>
  )

  let body = form
  if (jobId && job.isError) {
    // Removed after it expired, or not yours: start a new one.
    body = (
      <div className="stack">
        <ErrorBox message={t('privacy.exportGone')} />
        {form}
      </div>
    )
  } else if (current && jobStatus(current) === 'completed') {
    body = (
      <div className="stack">
        <div className="success-box">
          {t('privacy.exportReady')} {until && t('privacy.exportUntil', { date: until })}
        </div>
        <div>
          <Button variant="primary" icon="download" onClick={() => void download()}>
            {t('privacy.download')}
          </Button>
        </div>
      </div>
    )
  } else if (current && jobStatus(current) === 'failed') {
    body = (
      <div className="stack">
        <ErrorBox message={t('privacy.exportFailed')} />
        {form}
      </div>
    )
  } else if (jobId && (job.isPending || (current && isRunning(current)))) {
    body = (
      <div className="success-box">
        <Spinner /> {t('privacy.exportStarted')}
      </div>
    )
  }

  return (
    <Card title={t('privacy.exportTitle')} description={t('privacy.exportBody')}>
      {body}
    </Card>
  )
}

function DeletionCard() {
  const t = useT()
  const locale = useLocale()
  const navigate = useNavigate()
  const { push } = useToast()
  const queryClient = useQueryClient()
  const errors = useApiErrors()
  const scheduled = useQuery({
    queryKey: ['deletion'],
    queryFn: async () => {
      try {
        return await api<PrivacyRequest>('/user/privacy/deletion', { quiet: true })
      } catch (cause) {
        if (cause instanceof ApiError && cause.status === 404) return null
        throw cause
      }
    },
  })
  const [open, setOpen] = useState(false)
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [busy, setBusy] = useState(false)

  const request = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    errors.clear()
    try {
      await api('/user/privacy/deletion', { method: 'POST', body: { password } })
      // Asking signs out every session, this one included.
      forgetSession()
      await navigate({ to: '/login' })
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  const cancel = async () => {
    await api('/user/privacy/deletion', { method: 'DELETE' })
    push(t('privacy.deletionCancelled'), 'success')
    await queryClient.invalidateQueries({ queryKey: ['deletion'] })
  }

  const when = scheduled.data?.scheduled_for
    ? new Intl.DateTimeFormat(intlLocale(locale), { dateStyle: 'long' }).format(new Date(scheduled.data.scheduled_for))
    : ''

  return (
    <Card title={t('privacy.deleteTitle')} description={t('privacy.deleteBody')}>
      {scheduled.data && scheduled.data.status === 'scheduled' ? (
        <div className="stack">
          <div className="banner warn">{t('privacy.deleteScheduled', { date: when })}</div>
          <div>
            <Button variant="primary" icon="refresh" onClick={() => void cancel()}>
              {t('privacy.cancelDeletion')}
            </Button>
          </div>
        </div>
      ) : (
        <div>
          <Button variant="danger" icon="trash" onClick={() => setOpen(true)}>
            {t('privacy.deleteStart')}
          </Button>
        </div>
      )}
      <Modal open={open} title={t('privacy.deleteTitle')} description={t('privacy.deleteBody')} onClose={() => setOpen(false)}>
        <form onSubmit={request} className="stack">
          <Field label={t('auth.password')} error={errors.field('password')}>
            <PasswordInput name="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
          <Field label={t('privacy.deleteConfirmLabel')}>
            <input className="input mono" value={confirm} onChange={(e) => setConfirm(e.target.value)} />
          </Field>
          {errors.box}
          <div className="modal-actions">
            <Button variant="ghost" type="button" onClick={() => setOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button variant="danger" type="submit" disabled={busy || !password || confirm !== 'DELETE'}>
              {busy ? <Spinner /> : null} {t('privacy.deleteStart')}
            </Button>
          </div>
        </form>
      </Modal>
    </Card>
  )
}
