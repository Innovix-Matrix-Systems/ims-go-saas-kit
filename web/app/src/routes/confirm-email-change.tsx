import { Link, createFileRoute } from '@tanstack/react-router'
import { useEffect, useRef, useState } from 'react'
import { api } from '../api/client'
import { forgetSession } from '../auth/session'
import { AuthCard, readLinkParams } from '../components/form'
import { Icon, Spinner } from '../components/ui'
import { useT } from '../i18n'

export const Route = createFileRoute('/confirm-email-change')({
  component: ConfirmEmailChangePage,
})

function ConfirmEmailChangePage() {
  const t = useT()
  const [{ token }] = useState(() => readLinkParams(['token'] as const))
  const [state, setState] = useState<'working' | 'done' | 'failed'>(token ? 'working' : 'failed')
  const started = useRef(false)

  useEffect(() => {
    if (!token || started.current) return
    started.current = true
    api('/email/change/confirm', { method: 'POST', body: { token }, quiet: true, token: null })
      .then(() => {
        // Every session was signed out, this browser's included.
        forgetSession()
        setState('done')
      })
      .catch(() => setState('failed'))
  }, [token])

  return (
    <AuthCard title={t('emailChange.title')} footer={<Link to="/login">{t('auth.backToSignIn')}</Link>}>
      {state === 'working' ? (
        <p className="login-sub">
          <Spinner /> {t('emailChange.working')}
        </p>
      ) : state === 'done' ? (
        <div className="stack">
          <div className="success-box">{t('emailChange.done')}</div>
          <Link to="/login" className="btn primary">
            <Icon name="key" /> {t('auth.signIn')}
          </Link>
        </div>
      ) : (
        <p className="login-sub">{t('emailChange.failed')}</p>
      )}
    </AuthCard>
  )
}
