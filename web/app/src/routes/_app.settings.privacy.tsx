import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { api, apiBlob, ApiError, type Schemas } from '../api/client'
import { forgetSession } from '../auth/session'
import { Field, PasswordInput, useApiErrors } from '../components/form'
import { useToast } from '../components/Toasts'
import { Button, Card, Icon, Modal, Spinner } from '../components/ui'
import { intlLocale, useLocale, useT } from '../i18n'

export const Route = createFileRoute('/_app/settings/privacy')({
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

function ExportCard() {
  const t = useT()
  const errors = useApiErrors()
  const [password, setPassword] = useState('')
  const [jobId, setJobId] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const job = useQuery({
    queryKey: ['export-job', jobId],
    queryFn: () => api<{ status: string }>(`/data-processing/job/${jobId}`),
    enabled: Boolean(jobId),
    refetchInterval: (query) => (query.state.data?.status === 'completed' || query.state.data?.status === 'failed' ? false : 3000),
  })

  const start = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    errors.clear()
    try {
      const result = await api<ExportJob>('/user/privacy/export', { method: 'POST', body: { password } })
      setJobId(result.job_id)
      setPassword('')
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  const download = async () => {
    const blob = await apiBlob(`/api/v1/data-processing/job/${jobId}/download`)
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'my-data.zip'
    link.click()
    setTimeout(() => URL.revokeObjectURL(url), 10_000)
  }

  return (
    <Card title={t('privacy.exportTitle')} description={t('privacy.exportBody')}>
      {jobId ? (
        job.data?.status === 'completed' ? (
          <div className="stack">
            <div className="success-box">{t('privacy.exportReady')}</div>
            <div>
              <Button variant="primary" icon="download" onClick={() => void download()}>
                {t('privacy.download')}
              </Button>
            </div>
          </div>
        ) : (
          <div className="success-box">
            <Spinner /> {t('privacy.exportStarted')}
          </div>
        )
      ) : (
        <form onSubmit={start} className="stack form-narrow">
          <Field label={t('auth.password')} error={errors.field('password')}>
            <PasswordInput autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
          {errors.box}
          <div>
            <button type="submit" className="btn" disabled={busy || !password}>
              {busy ? <Spinner /> : <Icon name="download" />} {t('privacy.exportStart')}
            </button>
          </div>
        </form>
      )}
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
            <PasswordInput autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
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
