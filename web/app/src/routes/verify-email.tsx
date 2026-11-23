import { Link, createFileRoute } from '@tanstack/react-router'
import { useEffect, useRef, useState, type FormEvent } from 'react'
import { api } from '../api/client'
import { AuthCard, Field, readLinkParams } from '../components/form'
import { Icon, Spinner } from '../components/ui'
import { useT } from '../i18n'

export const Route = createFileRoute('/verify-email')({
  component: VerifyEmailPage,
})

type State = 'working' | 'done' | 'failed' | 'resent'

function VerifyEmailPage() {
  const t = useT()
  const [{ token }] = useState(() => readLinkParams(['token'] as const))
  const [state, setState] = useState<State>(token ? 'working' : 'failed')
  const [email, setEmail] = useState('')
  const [busy, setBusy] = useState(false)
  const started = useRef(false)

  useEffect(() => {
    if (!token || started.current) return
    started.current = true
    api('/email/verify', { method: 'POST', body: { token }, quiet: true, token: null })
      .then(() => setState('done'))
      .catch(() => setState('failed'))
  }, [token])

  const resend = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    try {
      await api('/email/verification-notification', { method: 'POST', body: { email: email.trim() }, quiet: true, token: null })
    } catch {
      /* the answer is the same either way */
    } finally {
      setBusy(false)
      setState('resent')
    }
  }

  return (
    <AuthCard title={t('verify.title')} footer={<Link to="/login">{t('auth.backToSignIn')}</Link>}>
      {state === 'working' ? (
        <p className="login-sub">
          <Spinner /> {t('verify.working')}
        </p>
      ) : null}
      {state === 'done' ? (
        <div className="stack">
          <div className="success-box">{t('verify.done')}</div>
          <Link to="/login" className="btn primary">
            <Icon name="key" /> {t('auth.signIn')}
          </Link>
        </div>
      ) : null}
      {state === 'resent' ? <div className="success-box">{t('verify.resent')}</div> : null}
      {state === 'failed' ? (
        <form onSubmit={resend} className="stack">
          <p className="login-sub">{t('verify.failed')}</p>
          <Field label={t('auth.email')}>
            <input className="input" type="email" name="email" autoComplete="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
          </Field>
          <button type="submit" className="btn primary" disabled={busy || !email.trim()}>
            {busy ? <Spinner /> : <Icon name="mail" />} {t('verify.resend')}
          </button>
        </form>
      ) : null}
    </AuthCard>
  )
}
