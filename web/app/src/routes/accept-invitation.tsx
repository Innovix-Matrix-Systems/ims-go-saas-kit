import { Link, createFileRoute } from '@tanstack/react-router'
import { useEffect, useState, type FormEvent } from 'react'
import { api } from '../api/client'
import { AuthCard, Field, PasswordInput, passwordProblem, readLinkParams, useApiErrors } from '../components/form'
import { Icon, Spinner } from '../components/ui'
import { FormSkeleton } from '../components/Skeleton'
import { useT } from '../i18n'

export const Route = createFileRoute('/accept-invitation')({
  component: AcceptInvitationPage,
})

interface Preview {
  name: string
  email: string
  organization_name: string
  inviter_name: string
}

function AcceptInvitationPage() {
  const t = useT()
  const [{ token, email }] = useState(() => readLinkParams(['token', 'email'] as const))
  const [preview, setPreview] = useState<Preview | null>(null)
  const [invalid, setInvalid] = useState(!token || !email)
  const errors = useApiErrors()
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState(false)
  const weak = passwordProblem(password, t)
  const differs = confirm && confirm !== password ? t('form.passwordsDiffer') : undefined

  useEffect(() => {
    if (!token || !email) return
    api<Preview>('/invitation/preview', { method: 'POST', body: { email, token }, quiet: true, token: null })
      .then(setPreview)
      .catch(() => setInvalid(true))
  }, [token, email])

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    errors.clear()
    try {
      await api('/invitation/accept', {
        method: 'POST',
        quiet: true,
        token: null,
        body: { email, token, password, password_confirmation: confirm },
      })
      setDone(true)
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  if (invalid) {
    return (
      <AuthCard title={t('invite.title', { organization: '…' })} footer={<Link to="/login">{t('auth.backToSignIn')}</Link>}>
        <p className="login-sub">{t('invite.invalid')}</p>
      </AuthCard>
    )
  }
  if (!preview) {
    return (
      <AuthCard title={t('common.loading')}>
        <FormSkeleton fields={3} />
      </AuthCard>
    )
  }

  const organization = preview.organization_name || t('app.name')
  return (
    <AuthCard
      title={t('invite.title', { organization })}
      subtitle={
        done
          ? undefined
          : preview.inviter_name
            ? t('invite.subtitleWithInviter', { inviter: preview.inviter_name, organization })
            : t('invite.subtitle')
      }
      footer={<Link to="/login">{t('auth.backToSignIn')}</Link>}
    >
      {done ? (
        <div className="stack">
          <div className="success-box">{t('invite.done')}</div>
          <Link to="/login" className="btn primary">
            <Icon name="key" /> {t('auth.signIn')}
          </Link>
        </div>
      ) : (
        <form onSubmit={submit} className="stack">
          <Field label={t('auth.email')}>
            <input className="input" type="email" value={preview.email} readOnly name="email" autoComplete="username" />
          </Field>
          <Field label={t('auth.password')} error={errors.field('password') ?? weak} hint={t('form.passwordHint')}>
            <PasswordInput name="password" autoComplete="new-password" required autoFocus value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
          <Field label={t('form.confirmPassword')} error={errors.field('password_confirmation') ?? differs}>
            <PasswordInput name="password_confirmation" autoComplete="new-password" required value={confirm} onChange={(e) => setConfirm(e.target.value)} />
          </Field>
          {errors.field('token') ? <div className="small danger-text">{errors.field('token')}</div> : errors.box}
          <button type="submit" className="btn primary mt-4" disabled={busy || Boolean(weak) || !password || confirm !== password}>
            {busy ? <Spinner /> : <Icon name="check" />} {t('invite.submit')}
          </button>
        </form>
      )}
    </AuthCard>
  )
}
