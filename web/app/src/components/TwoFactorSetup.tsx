import QRCode from 'qrcode'
import { useEffect, useState, type FormEvent } from 'react'
import { api } from '../api/client'
import { useT } from '../i18n'
import { Field, PasswordInput, useApiErrors } from './form'
import { Button, Icon, Spinner } from './ui'

interface Started {
  method: 'totp' | 'email'
  secret?: string
  otpauth_url?: string
}

interface Confirmed {
  recovery_codes: string[]
  /** A full session token, when the setup ran on a setup-only token. */
  token?: string
}

/**
 * Turns two-factor authentication on: method → password → code → recovery
 * codes. `token` overrides the stored session (the setup-only token from a
 * sign-in that requires two-factor).
 */
export function TwoFactorSetup({
  token,
  onDone,
}: {
  token?: string | null
  onDone: (result: Confirmed) => void
}) {
  const t = useT()
  const errors = useApiErrors()
  const [method, setMethod] = useState<'totp' | 'email'>('totp')
  const [password, setPassword] = useState('')
  const [started, setStarted] = useState<Started | null>(null)
  const [qr, setQr] = useState<string | null>(null)
  const [code, setCode] = useState('')
  const [confirmed, setConfirmed] = useState<Confirmed | null>(null)
  const [saved, setSaved] = useState(false)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!started?.otpauth_url) return
    QRCode.toDataURL(started.otpauth_url, { margin: 1, width: 180 })
      .then(setQr)
      .catch(() => setQr(null))
  }, [started?.otpauth_url])

  const run = async (action: () => Promise<void>) => {
    setBusy(true)
    errors.clear()
    try {
      await action()
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  const start = (event: FormEvent) => {
    event.preventDefault()
    void run(async () => {
      setStarted(await api<Started>('/two-factor', { method: 'POST', body: { method, password }, token, quiet: true }))
    })
  }

  const confirm = (event: FormEvent) => {
    event.preventDefault()
    void run(async () => {
      setConfirmed(await api<Confirmed>('/two-factor/confirm', { method: 'POST', body: { code: code.trim() }, token, quiet: true }))
    })
  }

  if (confirmed) {
    const text = confirmed.recovery_codes.join('\n')
    return (
      <div className="stack">
        <h3 className="card-title">{t('twoFactor.codesTitle')}</h3>
        <p className="login-sub">{t('twoFactor.codesBody')}</p>
        <div className="codes-grid">
          {confirmed.recovery_codes.map((value) => (
            <span key={value}>{value}</span>
          ))}
        </div>
        <div className="flex g-2">
          <Button variant="ghost" icon="copy" onClick={() => void navigator.clipboard?.writeText(text)}>
            {t('twoFactor.copyCodes')}
          </Button>
          <a className="btn ghost" href={`data:text/plain;charset=utf-8,${encodeURIComponent(text + '\n')}`} download="recovery-codes.txt">
            <Icon name="download" /> {t('twoFactor.downloadCodes')}
          </a>
        </div>
        <label className="check-row">
          <input type="checkbox" checked={saved} onChange={(e) => setSaved(e.target.checked)} />
          {t('twoFactor.savedCodes')}
        </label>
        <button type="button" className="btn primary" disabled={!saved} onClick={() => onDone(confirmed)}>
          <Icon name="check" /> {t('twoFactor.finish')}
        </button>
      </div>
    )
  }

  if (started) {
    return (
      <form onSubmit={confirm} className="stack">
        {started.method === 'totp' ? (
          <>
            <p className="login-sub">{t('twoFactor.scan')}</p>
            {qr ? (
              <div className="qr-box">
                <img src={qr} alt="QR code" />
              </div>
            ) : null}
            {started.secret ? (
              <p className="small muted">
                {t('twoFactor.manual')} <code className="mono">{started.secret}</code>
              </p>
            ) : null}
          </>
        ) : (
          <p className="login-sub">{t('twoFactor.emailSent')}</p>
        )}
        <Field label={t('twoFactor.code')} error={errors.field('code')}>
          <input
            className="input mono"
            inputMode="numeric"
            name="code" autoComplete="one-time-code"
            autoFocus
            value={code}
            onChange={(e) => setCode(e.target.value)}
          />
        </Field>
        {errors.box}
        <button type="submit" className="btn primary" disabled={busy || code.trim().length < 6}>
          {busy ? <Spinner /> : <Icon name="shield" />} {t('twoFactor.confirm')}
        </button>
      </form>
    )
  }

  return (
    <form onSubmit={start} className="stack">
      {(['totp', 'email'] as const).map((value) => (
        <label key={value} className="terms-row">
          <input type="radio" name="method" checked={method === value} onChange={() => setMethod(value)} />
          <span>
            <strong>{value === 'totp' ? t('twoFactor.methodApp') : t('twoFactor.methodEmail')}</strong>
            <br />
            <span className="small muted">{value === 'totp' ? t('twoFactor.methodAppHint') : t('twoFactor.methodEmailHint')}</span>
          </span>
        </label>
      ))}
      <Field label={t('auth.password')} error={errors.field('password')} hint={t('twoFactor.passwordPrompt')}>
        <PasswordInput name="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
      </Field>
      {errors.box}
      <button type="submit" className="btn primary" disabled={busy || !password}>
        {busy ? <Spinner /> : <Icon name="chevron" />} {t('twoFactor.start')}
      </button>
    </form>
  )
}
