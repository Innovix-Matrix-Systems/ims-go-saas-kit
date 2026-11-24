import { useState, type InputHTMLAttributes, type ReactNode } from 'react'
import { ApiError } from '../api/client'
import { useT } from '../i18n'
import { AppearanceMenu } from './AppearanceMenu'
import { ErrorBox, Icon } from './ui'

/** The centred card used by every page reached without signing in. */
export function AuthCard({ title, subtitle, children, footer }: { title: string; subtitle?: ReactNode; children: ReactNode; footer?: ReactNode }) {
  const t = useT()
  return (
    <div className="login-wrap">
      <div className="login-top">
        <AppearanceMenu />
      </div>
      <div className="login-card">
        <div className="login-brand">
          <div className="brand-mark">I</div>
          <div className="brand-title">{t('app.name')}</div>
        </div>
        <div className="card">
          <h1 className="login-title">{title}</h1>
          {subtitle ? <p className="login-sub">{subtitle}</p> : null}
          {children}
        </div>
        {footer ? <div className="login-links">{footer}</div> : null}
      </div>
    </div>
  )
}

/** A labelled input with the API's message for that field, if any. */
export function Field({
  label,
  error,
  hint,
  children,
}: {
  label: string
  error?: string
  hint?: ReactNode
  children: ReactNode
}) {
  return (
    <label className="field">
      <span className="field-label">{label}</span>
      {children}
      {error ? <span className="small danger-text">{error}</span> : hint ? <span className="small muted">{hint}</span> : null}
    </label>
  )
}

/** A password input with a show/hide toggle. */
export function PasswordInput(props: Omit<InputHTMLAttributes<HTMLInputElement>, 'type'>) {
  const t = useT()
  const [visible, setVisible] = useState(false)
  return (
    <div className="input-with-action">
      <input {...props} className={`input ${props.className ?? ''}`} type={visible ? 'text' : 'password'} />
      <button
        type="button"
        className="icon-btn"
        onClick={() => setVisible((value) => !value)}
        aria-label={visible ? t('form.hidePassword') : t('form.showPassword')}
        title={visible ? t('form.hidePassword') : t('form.showPassword')}
      >
        <Icon name={visible ? 'x' : 'eye'} />
      </button>
    </div>
  )
}

/** The rule every new password follows (the API enforces the same). */
export function passwordProblem(password: string, t: ReturnType<typeof useT>): string | undefined {
  if (!password) return undefined
  if ([...password].length < 8) return t('form.passwordTooShort')
  if (new TextEncoder().encode(password).length > 72) return t('form.passwordTooLong')
  return undefined
}

/** Field errors and a form-level message from an API error. */
export function useApiErrors() {
  const [error, setError] = useState<Error | null>(null)
  const api = error instanceof ApiError ? error : null
  const field = (name: string) => api?.field(name)
  const hasFieldErrors = Boolean(api?.errors && Object.keys(api.errors).length > 0)
  const message = error && !hasFieldErrors ? error.message : null
  return {
    error,
    setError,
    field,
    /** A box for the message that is not about one field. */
    box: message ? <ErrorBox message={message} /> : null,
    clear: () => setError(null),
  }
}

/**
 * Reads values from a link opened from an e-mail (token, email), then removes
 * them from the address bar so they do not stay in history or get shared.
 */
export function readLinkParams<K extends string>(keys: readonly K[]): Record<K, string> {
  const params = new URLSearchParams(window.location.search)
  const out = {} as Record<K, string>
  for (const key of keys) out[key] = params.get(key) ?? ''
  if (keys.some((key) => params.has(key))) {
    window.history.replaceState(window.history.state, '', window.location.pathname)
  }
  return out
}
