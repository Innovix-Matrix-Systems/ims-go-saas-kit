import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { api, type Schemas } from '../api/client'
import { useSession } from '../auth/session'
import { Field, PasswordInput, passwordProblem, useApiErrors } from '../components/form'
import { TwoFactorSetup } from '../components/TwoFactorSetup'
import { useToast } from '../components/Toasts'
import { Badge, Button, Card, Icon, Modal, Spinner } from '../components/ui'
import { intlLocale, useLocale, useT } from '../i18n'

export const Route = createFileRoute('/_app/settings/security')({
  component: SecurityPage,
})

type TwoFactorStatus = Schemas['TwoFactorStatus']
type Session = Schemas['Session']

function SecurityPage() {
  return (
    <div className="stack g-4">
      <ChangePasswordCard />
      <TwoFactorCard />
      <SessionsCard />
    </div>
  )
}

function ChangePasswordCard() {
  const t = useT()
  const session = useSession()
  const { push } = useToast()
  const errors = useApiErrors()
  const [current, setCurrent] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const weak = passwordProblem(password, t)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    errors.clear()
    try {
      await api('/user/profile/change-password', { method: 'POST', body: { current_password: current, password } })
      setCurrent('')
      setPassword('')
      push(t('security.passwordChanged'), 'success')
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card title={t('security.changePassword')} description={t('security.changePasswordHint')}>
      <form onSubmit={submit} className="stack form-narrow">
        {/* Tells password managers which account the new password is for. */}
        <input type="email" name="email" autoComplete="username" value={session?.user.email ?? ''} readOnly hidden />
        <Field label={t('form.currentPassword')} error={errors.field('current_password')}>
          <PasswordInput name="password" autoComplete="current-password" required value={current} onChange={(e) => setCurrent(e.target.value)} />
        </Field>
        <Field label={t('form.newPassword')} error={errors.field('password') ?? weak} hint={t('form.passwordHint')}>
          <PasswordInput name="password" autoComplete="new-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        {errors.box}
        <div>
          <button type="submit" className="btn primary" disabled={busy || !current || !password || Boolean(weak)}>
            {busy ? <Spinner /> : <Icon name="key" />} {t('security.changePassword')}
          </button>
        </div>
      </form>
    </Card>
  )
}

function TwoFactorCard() {
  const t = useT()
  const queryClient = useQueryClient()
  const status = useQuery({ queryKey: ['two-factor'], queryFn: () => api<TwoFactorStatus>('/two-factor') })
  const [mode, setMode] = useState<'enable' | 'disable' | 'regenerate' | null>(null)
  const refresh = () => void queryClient.invalidateQueries({ queryKey: ['two-factor'] })
  const data = status.data

  return (
    <Card
      title={t('twoFactor.status')}
      actions={
        data ? (
          <Badge tone={data.enabled ? 'ok' : 'warn'}>{data.enabled ? t('twoFactor.on') : t('twoFactor.off')}</Badge>
        ) : null
      }
    >
      {!data ? (
        <Spinner />
      ) : (
        <div className="stack">
          {data.required ? <span className="small muted">{t('twoFactor.required')}</span> : null}
          {data.enabled ? (
            <>
              <span className="small muted">{t('twoFactor.codesLeft', { count: data.recovery_codes_left })}</span>
              <div className="flex g-2">
                <Button icon="refresh" onClick={() => setMode('regenerate')}>
                  {t('twoFactor.regenerate')}
                </Button>
                {!data.required ? (
                  <Button variant="danger" icon="x" onClick={() => setMode('disable')}>
                    {t('twoFactor.disable')}
                  </Button>
                ) : null}
              </div>
            </>
          ) : (
            <>
              <p className="login-sub">{t('onboarding.twoFactorHint')}</p>
              <div>
                <Button variant="primary" icon="shield" onClick={() => setMode('enable')}>
                  {t('twoFactor.enable')}
                </Button>
              </div>
            </>
          )}
        </div>
      )}
      <Modal open={mode === 'enable'} title={t('twoFactor.enable')} onClose={() => setMode(null)}>
        <TwoFactorSetup
          onDone={() => {
            setMode(null)
            refresh()
          }}
        />
      </Modal>
      <PasswordAction
        open={mode === 'disable'}
        title={t('twoFactor.disable')}
        description={t('twoFactor.disableConfirm')}
        confirmLabel={t('twoFactor.disable')}
        danger
        run={(password) => api('/two-factor', { method: 'DELETE', body: { password } })}
        onClose={() => setMode(null)}
        onDone={refresh}
      />
      <RegenerateCodes open={mode === 'regenerate'} onClose={() => setMode(null)} onDone={refresh} />
    </Card>
  )
}

/** A small dialog that asks for the password before a sensitive change. */
function PasswordAction({
  open,
  title,
  description,
  confirmLabel,
  danger,
  run,
  onClose,
  onDone,
}: {
  open: boolean
  title: string
  description?: string
  confirmLabel: string
  danger?: boolean
  run: (password: string) => Promise<unknown>
  onClose: () => void
  onDone: () => void
}) {
  const t = useT()
  const errors = useApiErrors()
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const close = () => {
    setPassword('')
    errors.clear()
    onClose()
  }
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    errors.clear()
    try {
      await run(password)
      close()
      onDone()
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Modal open={open} title={title} description={description} onClose={close}>
      <form onSubmit={submit} className="stack">
        <Field label={t('auth.password')} error={errors.field('password')}>
          <PasswordInput name="password" autoComplete="current-password" required autoFocus value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        {errors.box}
        <div className="modal-actions">
          <Button variant="ghost" type="button" onClick={close}>
            {t('common.cancel')}
          </Button>
          <Button variant={danger ? 'danger' : 'primary'} type="submit" disabled={busy || !password}>
            {busy ? <Spinner /> : null} {confirmLabel}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

function RegenerateCodes({ open, onClose, onDone }: { open: boolean; onClose: () => void; onDone: () => void }) {
  const t = useT()
  const [codes, setCodes] = useState<string[] | null>(null)
  if (codes) {
    return (
      <Modal
        open={open}
        title={t('twoFactor.codesTitle')}
        description={t('twoFactor.codesBody')}
        onClose={() => {
          setCodes(null)
          onClose()
        }}
        actions={
          <Button
            variant="primary"
            onClick={() => {
              setCodes(null)
              onClose()
            }}
          >
            {t('twoFactor.finish')}
          </Button>
        }
      >
        <div className="codes-grid">
          {codes.map((code) => (
            <span key={code}>{code}</span>
          ))}
        </div>
      </Modal>
    )
  }
  return (
    <PasswordAction
      open={open}
      title={t('twoFactor.regenerate')}
      confirmLabel={t('twoFactor.regenerate')}
      run={async (password) => {
        const result = await api<{ recovery_codes: string[] }>('/two-factor/recovery-codes', { method: 'POST', body: { password } })
        setCodes(result.recovery_codes)
      }}
      onClose={() => (codes ? undefined : onClose())}
      onDone={onDone}
    />
  )
}

function SessionsCard() {
  const t = useT()
  const locale = useLocale()
  const queryClient = useQueryClient()
  const sessions = useQuery({ queryKey: ['sessions'], queryFn: () => api<Session[]>('/sessions') })
  const refresh = () => void queryClient.invalidateQueries({ queryKey: ['sessions'] })
  const date = (value: string) =>
    new Intl.DateTimeFormat(intlLocale(locale), { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
  const others = (sessions.data ?? []).filter((session) => !session.current)

  return (
    <Card
      title={t('security.sessions')}
      description={t('security.sessionsHint')}
      actions={
        others.length > 0 ? (
          <Button size="sm" icon="logout" onClick={() => void api('/sessions/revoke-others', { method: 'POST' }).then(refresh)}>
            {t('security.signOutOthers')}
          </Button>
        ) : null
      }
      flush
    >
      {!sessions.data ? (
        <div className="p-4">
          <Spinner />
        </div>
      ) : (
        <ul className="session-list">
          {sessions.data.map((session) => (
            <li key={session.id} className="session-row">
              <Icon name="monitor" />
              <div className="session-meta">
                <div className="session-name">
                  {session.device} {session.current ? <Badge tone="info">{t('security.thisDevice')}</Badge> : null}
                </div>
                <div className="small muted">
                  {session.browser}
                  {session.ip ? ` · ${session.ip}` : ''}
                  {session.last_used_at ? ` · ${t('security.lastUsed', { when: date(session.last_used_at) })}` : ''}
                </div>
              </div>
              {!session.current ? (
                <Button size="sm" variant="ghost" onClick={() => void api(`/sessions/${session.id}`, { method: 'DELETE' }).then(refresh)}>
                  {t('security.signOutDevice')}
                </Button>
              ) : null}
            </li>
          ))}
        </ul>
      )}
    </Card>
  )
}
