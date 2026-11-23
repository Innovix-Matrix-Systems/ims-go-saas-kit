import { Link, createFileRoute } from '@tanstack/react-router'
import { useState, type FormEvent } from 'react'
import { api } from '../api/client'
import { forgetSession } from '../auth/session'
import { AuthCard, Field, PasswordInput, passwordProblem, readLinkParams, useApiErrors } from '../components/form'
import { Icon, Spinner } from '../components/ui'
import { useT } from '../i18n'

export const Route = createFileRoute('/reset-password')({
  component: ResetPasswordPage,
})

function ResetPasswordPage() {
  const t = useT()
  const [{ token, email }] = useState(() => readLinkParams(['token', 'email'] as const))
  const errors = useApiErrors()
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState(false)
  const weak = passwordProblem(password, t)
  const differs = confirm && confirm !== password ? t('form.passwordsDiffer') : undefined

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    errors.clear()
    try {
      await api('/reset-password', {
        method: 'POST',
        quiet: true,
        token: null,
        body: { email, token, password, password_confirmation: confirm },
      })
      // Every session was revoked, including one in this browser.
      forgetSession()
      setDone(true)
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  if (!token || !email) {
    return (
      <AuthCard title={t('reset.title')} footer={<Link to="/forgot-password">{t('forgot.submit')}</Link>}>
        <p className="login-sub">{t('reset.missing')}</p>
      </AuthCard>
    )
  }

  return (
    <AuthCard title={t('reset.title')} subtitle={done ? undefined : t('reset.subtitle')} footer={<Link to="/login">{t('auth.backToSignIn')}</Link>}>
      {done ? (
        <div className="stack">
          <div className="success-box">{t('reset.done')}</div>
          <Link to="/login" className="btn primary">
            <Icon name="key" /> {t('auth.signIn')}
          </Link>
        </div>
      ) : (
        <form onSubmit={submit} className="stack">
          <Field label={t('form.newPassword')} error={errors.field('password') ?? weak} hint={t('form.passwordHint')}>
            <PasswordInput autoComplete="new-password" required autoFocus value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
          <Field label={t('form.confirmPassword')} error={errors.field('password_confirmation') ?? differs}>
            <PasswordInput autoComplete="new-password" required value={confirm} onChange={(e) => setConfirm(e.target.value)} />
          </Field>
          {errors.field('token') ? <div className="small danger-text">{errors.field('token')}</div> : errors.box}
          <button type="submit" className="btn primary mt-4" disabled={busy || Boolean(weak) || !password || confirm !== password}>
            {busy ? <Spinner /> : <Icon name="check" />} {t('reset.submit')}
          </button>
        </form>
      )}
    </AuthCard>
  )
}
