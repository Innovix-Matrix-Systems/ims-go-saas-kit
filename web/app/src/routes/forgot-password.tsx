import { Link, createFileRoute } from '@tanstack/react-router'
import { useState, type FormEvent } from 'react'
import { api } from '../api/client'
import { AuthCard, Field, useApiErrors } from '../components/form'
import { Icon, Spinner } from '../components/ui'
import { useT } from '../i18n'

export const Route = createFileRoute('/forgot-password')({
  component: ForgotPasswordPage,
})

function ForgotPasswordPage() {
  const t = useT()
  const errors = useApiErrors()
  const [email, setEmail] = useState('')
  const [busy, setBusy] = useState(false)
  const [sent, setSent] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    errors.clear()
    try {
      await api('/forgot-password', { method: 'POST', body: { email: email.trim() }, quiet: true, token: null })
      setSent(true)
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  return (
    <AuthCard title={t('forgot.title')} subtitle={sent ? undefined : t('forgot.subtitle')} footer={<Link to="/login">{t('auth.backToSignIn')}</Link>}>
      {sent ? (
        <div className="success-box">{t('forgot.sent')}</div>
      ) : (
        <form onSubmit={submit} className="stack">
          <Field label={t('auth.email')} error={errors.field('email')}>
            <input className="input" type="email" name="email" autoComplete="email" required autoFocus value={email} onChange={(e) => setEmail(e.target.value)} />
          </Field>
          {errors.box}
          <button type="submit" className="btn primary mt-4" disabled={busy || !email.trim()}>
            {busy ? <Spinner /> : <Icon name="mail" />} {t('forgot.submit')}
          </button>
        </form>
      )}
    </AuthCard>
  )
}
