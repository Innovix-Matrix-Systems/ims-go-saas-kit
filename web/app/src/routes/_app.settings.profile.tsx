import { createFileRoute } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { api, type User } from '../api/client'
import { useSession } from '../auth/session'
import { Field, PasswordInput, useApiErrors } from '../components/form'
import { useToast } from '../components/Toasts'
import { Card, Icon, Spinner } from '../components/ui'
import { locales, setLocale, useT, type Locale } from '../i18n'

export const Route = createFileRoute('/_app/settings/profile')({
  component: ProfilePage,
})

function ProfilePage() {
  const session = useSession()
  if (!session) return null
  return (
    <div className="stack g-4">
      <ProfileForm user={session.user} />
      <ChangeEmailForm current={session.user.email} />
    </div>
  )
}

function ProfileForm({ user }: { user: User }) {
  const t = useT()
  const queryClient = useQueryClient()
  const { push } = useToast()
  const errors = useApiErrors()
  const [firstName, setFirstName] = useState(user.first_name ?? '')
  const [lastName, setLastName] = useState(user.last_name ?? '')
  const [name, setName] = useState(user.name)
  const [phone, setPhone] = useState(user.phone ?? '')
  const [locale, setLocaleChoice] = useState<string>(user.locale ?? '')
  const [busy, setBusy] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    errors.clear()
    try {
      await api<User>('/user/profile/update', {
        method: 'POST',
        body: {
          first_name: firstName.trim() || null,
          last_name: lastName.trim() || null,
          name: name.trim(),
          phone: phone.trim() || null,
          locale,
        },
      })
      if (locale === 'en' || locale === 'bn') setLocale(locale as Locale)
      await queryClient.invalidateQueries({ queryKey: ['session'] })
      push(t('form.saved'), 'success')
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card title={t('settings.profile')}>
      <form onSubmit={submit} className="stack form-narrow">
        <div className="form-grid-2">
          <Field label={t('profile.firstName')} error={errors.field('first_name')}>
            <input className="input" autoComplete="given-name" value={firstName} onChange={(e) => setFirstName(e.target.value)} />
          </Field>
          <Field label={t('profile.lastName')} error={errors.field('last_name')}>
            <input className="input" autoComplete="family-name" value={lastName} onChange={(e) => setLastName(e.target.value)} />
          </Field>
        </div>
        <Field label={t('profile.displayName')} error={errors.field('name')}>
          <input className="input" autoComplete="name" required value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label={`${t('signup.phone')} (${t('form.optional')})`} error={errors.field('phone')}>
          <input className="input" type="tel" autoComplete="tel" value={phone} onChange={(e) => setPhone(e.target.value)} />
        </Field>
        <Field label={t('profile.language')} error={errors.field('locale')}>
          <select className="input" value={locale} onChange={(e) => setLocaleChoice(e.target.value)}>
            <option value="">{t('profile.languageDefault')}</option>
            {locales.map((item) => (
              <option key={item.code} value={item.code} lang={item.code}>
                {item.label}
              </option>
            ))}
          </select>
        </Field>
        {errors.box}
        <div>
          <button type="submit" className="btn primary" disabled={busy || name.trim().length < 2}>
            {busy ? <Spinner /> : <Icon name="check" />} {busy ? t('form.saving') : t('form.save')}
          </button>
        </div>
      </form>
    </Card>
  )
}

function ChangeEmailForm({ current }: { current: string }) {
  const t = useT()
  const errors = useApiErrors()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [sentTo, setSentTo] = useState<string | null>(null)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    errors.clear()
    try {
      await api('/user/profile/change-email', { method: 'POST', body: { email: email.trim(), password } })
      setSentTo(email.trim())
      setPassword('')
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card title={t('profile.email')} description={current}>
      {sentTo ? (
        <div className="success-box">{t('profile.changeEmailSent', { email: sentTo })}</div>
      ) : (
        <form onSubmit={submit} className="stack form-narrow">
          <Field label={t('profile.newEmail')} error={errors.field('email')}>
            <input className="input" type="email" autoComplete="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
          </Field>
          <Field label={t('form.currentPassword')} error={errors.field('password')}>
            <PasswordInput autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
          {errors.box}
          <div>
            <button type="submit" className="btn" disabled={busy || !email.trim() || !password}>
              {busy ? <Spinner /> : <Icon name="mail" />} {t('profile.sendLink')}
            </button>
          </div>
        </form>
      )}
    </Card>
  )
}
