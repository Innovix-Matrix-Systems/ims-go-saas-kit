import { Link, createFileRoute, redirect, useNavigate } from '@tanstack/react-router'
import { useState, type FormEvent } from 'react'
import { ApiError } from '../api/client'
import {
  isSignedIn,
  sessionQuery,
  signIn,
  takeSessionEndedNotice,
  verifySecondFactor,
  type PendingSecondFactor,
  type SignInOutcome,
} from '../auth/session'
import { AppearanceMenu } from '../components/AppearanceMenu'
import { LanguageMenu } from '../components/LanguageMenu'
import { ErrorBox, Icon, Spinner } from '../components/ui'
import { isLocale, setLocale, useT } from '../i18n'
import { queryClient } from '../query'
import { onboardingQuery } from './_app.onboarding'

interface LoginSearch {
  /** Where to go after signing in (an in-app path only). */
  next?: string
}

export const Route = createFileRoute('/login')({
  validateSearch: (search: Record<string, unknown>): LoginSearch => ({
    next: typeof search.next === 'string' && search.next.startsWith('/') && !search.next.startsWith('//') ? search.next : undefined,
  }),
  beforeLoad: () => {
    if (isSignedIn()) throw redirect({ to: '/' })
  },
  component: LoginPage,
})

function LoginPage() {
  const t = useT()
  const navigate = useNavigate()
  const { next } = Route.useSearch()
  const [expired] = useState(takeSessionEndedNotice)
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [remember, setRemember] = useState(false)
  const [code, setCode] = useState('')
  const [pending, setPending] = useState<PendingSecondFactor | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<ApiError | Error | null>(null)

  const done = async (outcome: SignInOutcome) => {
    if (outcome.kind === 'second-factor') {
      setPending(outcome.pending)
      setCode('')
      return
    }
    if (outcome.kind === 'two-factor-setup') {
      await navigate({ to: '/setup-two-factor' })
      return
    }
    // The language the user chose follows them to every device.
    const session = await queryClient.fetchQuery(sessionQuery).catch(() => null)
    const saved = session?.user.locale
    if (saved && isLocale(saved)) void setLocale(saved)
    // A new organization's owner continues its setup first.
    const progress = await queryClient.fetchQuery(onboardingQuery).catch(() => null)
    if (progress?.is_owner && !progress.completed) {
      await navigate({ to: '/onboarding' })
      return
    }
    await navigate({ href: next ?? '/' })
  }

  const run = async (action: () => Promise<SignInOutcome>) => {
    setBusy(true)
    setError(null)
    try {
      await done(await action())
    } catch (cause) {
      setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  const submitPassword = (event: FormEvent) => {
    event.preventDefault()
    void run(() => signIn(email.trim(), password, remember))
  }

  const submitCode = (event: FormEvent) => {
    event.preventDefault()
    if (pending) void run(() => verifySecondFactor(pending, code))
  }

  const message = errorMessage(error, t)
  const fieldError = (name: string) => (error instanceof ApiError ? error.field(name) : undefined)

  return (
    <div className="login-wrap">
      <div className="login-top">
        <LanguageMenu />
        <AppearanceMenu />
      </div>
      <div className="login-card">
        <div className="login-brand">
          <div className="brand-mark">I</div>
          <div className="brand-title">{t('app.name')}</div>
        </div>
        <div className="card">
          {pending ? (
            <>
              <h1 className="login-title">{t('auth.otpTitle')}</h1>
              <p className="login-sub">
                {pending.method === 'totp' ? t('auth.otpSubtitleTotp') : t('auth.otpSubtitleEmail')} {t('auth.otpRecoveryHint')}
              </p>
              <form onSubmit={submitCode} className="stack">
                <label className="field">
                  <span className="field-label">{t('auth.otpCode')}</span>
                  <input
                    className="input mono"
                    inputMode="text"
                    name="code" autoComplete="one-time-code"
                    placeholder="123456"
                    autoFocus
                    value={code}
                    onChange={(event) => setCode(event.target.value)}
                  />
                  {fieldError('otp') ? <span className="small danger-text">{fieldError('otp')}</span> : null}
                </label>
                {message ? <ErrorBox message={message} /> : null}
                <button type="submit" className="btn primary mt-4" disabled={busy || !code.trim()}>
                  {busy ? <Spinner /> : <Icon name="check" />}
                  {t('auth.verify')}
                </button>
                <button
                  type="button"
                  className="link-btn"
                  onClick={() => {
                    setPending(null)
                    setCode('')
                    setError(null)
                  }}
                >
                  {t('auth.useAnotherAccount')}
                </button>
              </form>
            </>
          ) : (
            <>
              <h1 className="login-title">{t('auth.signInTitle')}</h1>
              <p className="login-sub">{t('auth.signInSubtitle')}</p>
              {expired && !error ? <ErrorBox message={t('auth.sessionExpired')} /> : null}
              <form onSubmit={submitPassword} className="stack">
                <label className="field">
                  <span className="field-label">{t('auth.email')}</span>
                  <input
                    className="input"
                    type="email"
                    name="email" autoComplete="username"
                    required
                    autoFocus
                    value={email}
                    onChange={(event) => setEmail(event.target.value)}
                  />
                  {fieldError('email') ? <span className="small danger-text">{fieldError('email')}</span> : null}
                </label>
                <label className="field">
                  <span className="field-label">{t('auth.password')}</span>
                  <input
                    className="input"
                    type="password"
                    name="password" autoComplete="current-password"
                    required
                    value={password}
                    onChange={(event) => setPassword(event.target.value)}
                  />
                  {fieldError('password') ? <span className="small danger-text">{fieldError('password')}</span> : null}
                </label>
                <div className="form-row-between">
                  <label className="check-row">
                    <input type="checkbox" checked={remember} onChange={(event) => setRemember(event.target.checked)} />
                    {t('auth.remember')}
                  </label>
                  <Link to="/forgot-password" className="small">
                    {t('auth.forgot')}
                  </Link>
                </div>
                {message ? <ErrorBox message={message} /> : null}
                <button type="submit" className="btn primary mt-4" disabled={busy || !email.trim() || !password}>
                  {busy ? <Spinner /> : <Icon name="key" />}
                  {busy ? t('auth.signingIn') : t('auth.signIn')}
                </button>
              </form>
            </>
          )}
        </div>
        {!pending ? (
          <div className="login-links">
            <span>
              {t('auth.noAccount')} <Link to="/signup">{t('auth.createOrganization')}</Link>
            </span>
          </div>
        ) : null}
      </div>
    </div>
  )
}

/** The message for the form, unless the error is already shown on a field. */
function errorMessage(error: ApiError | Error | null, t: ReturnType<typeof useT>): string | null {
  if (!error) return null
  if (error instanceof ApiError) {
    if (error.status === 429 && error.retryAfter) return t('auth.throttled', { count: error.retryAfter })
    if (error.errors && Object.keys(error.errors).length > 0) return null
  }
  return error.message
}
