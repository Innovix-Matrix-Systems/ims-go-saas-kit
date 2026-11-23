import { useForm } from '@tanstack/react-form'
import { createFileRoute, redirect } from '@tanstack/react-router'
import { useState } from 'react'
import { clearSession, fetchMe, getToken, login, setCachedMe, setToken, verifyOTP } from '../api/client'
import type { LoginResult } from '../api/types'
import { ErrorBox, Icon, Spinner } from '../components/ui'

export const Route = createFileRoute('/login')({
  beforeLoad: () => {
    if (getToken()) {
      throw redirect({ to: '/' })
    }
  },
  component: LoginPage,
})

/** The second step of a sign-in: the code the account waits for. */
interface CodeStep {
  email: string
  method: LoginResult['method']
  message?: string
}

function LoginPage() {
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [step, setStep] = useState<CodeStep | null>(null)
  const [code, setCode] = useState('')

  /** Stores the token of a finished sign-in and opens the dashboard. */
  const finish = async (result: LoginResult) => {
    if (result.status === 'TWO_FACTOR_SETUP_REQUIRED') {
      throw new Error(
        'This account must turn on two-factor authentication first (POST /api/v1/two-factor), then sign in again.',
      )
    }
    if (result.status !== 'AUTH_SUCCESS' || !result.token) {
      throw new Error(`Login did not complete (${result.status}).`)
    }
    setToken(result.token)
    try {
      const me = await fetchMe()
      setCachedMe(me)
    } catch {
      clearSession()
      throw new Error('This account does not have access to the developer dashboard.')
    }
    window.location.assign('/dev/')
  }

  const run = async (work: () => Promise<void>, fallback: string) => {
    setError(null)
    setBusy(true)
    try {
      await work()
    } catch (cause) {
      setToken(null)
      setError(cause instanceof Error ? cause.message : fallback)
    } finally {
      setBusy(false)
    }
  }

  const form = useForm({
    defaultValues: { email: '', password: '' },
    onSubmit: ({ value }) =>
      run(async () => {
        const result = await login(value.email.trim(), value.password)
        if (result.status === 'OTP_REQUIRED') {
          setStep({ email: value.email.trim(), method: result.method, message: result.message })
          return
        }
        await finish(result)
      }, 'Login failed'),
  })

  const submitCode = () =>
    run(async () => {
      if (step) await finish(await verifyOTP(step.email, code.trim()))
    }, 'Verification failed')

  return (
    <div className="login-wrap">
      <div className="login-card">
        <div className="login-brand">
          <div className="brand-mark">D</div>
          <div>
            <div className="brand-title">Dev dashboard</div>
            <div className="brand-sub">For super-admins and developers</div>
          </div>
        </div>
        <div className="card">
          {step ? (
            <>
              <h1 className="login-title">Enter your code</h1>
              <p className="login-sub">
                {step.message ??
                  (step.method === 'totp'
                    ? 'Enter the code from your authenticator app.'
                    : 'Enter the code sent to your e-mail address.')}{' '}
                A recovery code works too.
              </p>
              <form
                onSubmit={(event) => {
                  event.preventDefault()
                  void submitCode()
                }}
                className="stack"
              >
                <label className="field">
                  <span className="field-label">{step.method === 'totp' ? 'Authenticator code' : 'Code'}</span>
                  <input
                    className="input mono"
                    inputMode="numeric"
                    autoComplete="one-time-code"
                    placeholder="123456"
                    autoFocus
                    value={code}
                    onChange={(event) => setCode(event.target.value)}
                  />
                </label>
                {error ? <ErrorBox message={error} /> : null}
                <button type="submit" className="btn primary mt-4" disabled={busy || !code.trim()}>
                  {busy ? <Spinner /> : <Icon name="check" />}
                  {busy ? 'Verifying…' : 'Verify'}
                </button>
                <button
                  type="button"
                  className="link-btn"
                  onClick={() => {
                    setStep(null)
                    setCode('')
                    setError(null)
                  }}
                >
                  Use another account
                </button>
              </form>
            </>
          ) : (
            <>
              <h1 className="login-title">Sign in to continue</h1>
              <p className="login-sub">
                Super-admin and developer accounts only. This surface is disabled in production unless
                explicitly enabled.
              </p>

              <form
                onSubmit={(event) => {
                  event.preventDefault()
                  event.stopPropagation()
                  void form.handleSubmit()
                }}
                className="stack"
              >
                <form.Field
                  name="email"
                  validators={{
                    onChange: ({ value }) => (!value.trim() ? 'Email is required' : undefined),
                  }}
                >
                  {(field) => (
                    <label className="field">
                      <span className="field-label">Email</span>
                      <input
                        className="input"
                        type="email"
                        autoComplete="username"
                        placeholder="developer@ims.com"
                        value={field.state.value}
                        onChange={(event) => field.handleChange(event.target.value)}
                        onBlur={field.handleBlur}
                      />
                      {field.state.meta.errors.length > 0 ? (
                        <span className="small danger-text">{String(field.state.meta.errors[0])}</span>
                      ) : null}
                    </label>
                  )}
                </form.Field>

                <form.Field
                  name="password"
                  validators={{
                    onChange: ({ value }) => (!value ? 'Password is required' : undefined),
                  }}
                >
                  {(field) => (
                    <label className="field">
                      <span className="field-label">Password</span>
                      <input
                        className="input"
                        type="password"
                        autoComplete="current-password"
                        placeholder="••••••••"
                        value={field.state.value}
                        onChange={(event) => field.handleChange(event.target.value)}
                        onBlur={field.handleBlur}
                      />
                      {field.state.meta.errors.length > 0 ? (
                        <span className="small danger-text">{String(field.state.meta.errors[0])}</span>
                      ) : null}
                    </label>
                  )}
                </form.Field>

                {error ? <ErrorBox message={error} /> : null}

                <button type="submit" className="btn primary mt-4" disabled={busy}>
                  {busy ? <Spinner /> : <Icon name="check" />}
                  {busy ? 'Signing in…' : 'Sign in'}
                </button>
              </form>
            </>
          )}

          <div className="login-links">
            <span>Seeded: superadmin@ims.com · developer@ims.com</span>
            <a className="link" href="/docs" target="_blank" rel="noreferrer">
              API docs
            </a>
          </div>
        </div>
      </div>
    </div>
  )
}
