import { Link, createFileRoute, redirect } from '@tanstack/react-router'
import { useState, type FormEvent } from 'react'
import { api, ApiError } from '../api/client'
import { isSignedIn } from '../auth/session'
import { AuthCard, Field, PasswordInput, passwordProblem, useApiErrors } from '../components/form'
import { ErrorBox, Icon, Spinner } from '../components/ui'
import { getLocale, useT } from '../i18n'

export const Route = createFileRoute('/signup')({
  beforeLoad: () => {
    if (isSignedIn()) throw redirect({ to: '/' })
  },
  component: SignUpPage,
})

function SignUpPage() {
  const t = useT()
  const errors = useApiErrors()
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [organization, setOrganization] = useState('')
  const [phone, setPhone] = useState('')
  const [password, setPassword] = useState('')
  const [accept, setAccept] = useState(false)
  const [busy, setBusy] = useState(false)
  const [sentTo, setSentTo] = useState<string | null>(null)
  const weak = passwordProblem(password, t)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    errors.clear()
    try {
      await api('/signup', {
        method: 'POST',
        quiet: true,
        token: null,
        body: {
          name: name.trim(),
          email: email.trim(),
          organization_name: organization.trim(),
          phone: phone.trim() || null,
          password,
          locale: getLocale(),
          accept_terms: accept,
        },
      })
      setSentTo(email.trim())
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  if (sentTo) {
    return (
      <AuthCard title={t('signup.checkEmailTitle')} footer={<Link to="/login">{t('auth.backToSignIn')}</Link>}>
        <div className="success-box">{t('signup.checkEmailBody', { email: sentTo })}</div>
      </AuthCard>
    )
  }

  const closed = errors.error instanceof ApiError && errors.error.status === 403
  return (
    <AuthCard
      title={t('signup.title')}
      subtitle={t('signup.subtitle')}
      footer={
        <span>
          {t('auth.haveAccount')} <Link to="/login">{t('auth.signIn')}</Link>
        </span>
      }
    >
      <form onSubmit={submit} className="stack">
        <Field label={t('signup.name')} error={errors.field('name')}>
          <input className="input" autoComplete="name" required autoFocus value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label={t('auth.email')} error={errors.field('email')}>
          <input className="input" type="email" autoComplete="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
        </Field>
        <Field label={t('signup.organization')} error={errors.field('organization_name')}>
          <input className="input" autoComplete="organization" required value={organization} onChange={(e) => setOrganization(e.target.value)} />
        </Field>
        <Field label={`${t('signup.phone')} (${t('form.optional')})`} error={errors.field('phone')}>
          <input className="input" type="tel" autoComplete="tel" value={phone} onChange={(e) => setPhone(e.target.value)} />
        </Field>
        <Field label={t('auth.password')} error={errors.field('password') ?? weak} hint={t('form.passwordHint')}>
          <PasswordInput autoComplete="new-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        <label className="terms-row">
          <input type="checkbox" checked={accept} onChange={(e) => setAccept(e.target.checked)} />
          <span>{t('signup.acceptTerms')}</span>
        </label>
        {errors.field('accept_terms') ? <span className="small danger-text">{errors.field('accept_terms')}</span> : null}
        {closed ? <ErrorBox message={t('signup.closed')} /> : errors.box}
        <button type="submit" className="btn primary mt-4" disabled={busy || !accept || Boolean(weak) || !name.trim() || !email.trim() || !organization.trim()}>
          {busy ? <Spinner /> : <Icon name="building" />}
          {t('signup.submit')}
        </button>
      </form>
    </AuthCard>
  )
}
