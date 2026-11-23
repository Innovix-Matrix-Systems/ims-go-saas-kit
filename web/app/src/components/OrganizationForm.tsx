import { useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent, type ReactNode } from 'react'
import { api } from '../api/client'
import { useT } from '../i18n'
import { Field, useApiErrors } from './form'
import type { Organization } from './organization'
import { useToast } from './Toasts'
import { Icon, Spinner } from './ui'

/**
 * The organization's name and contact details. Saving refreshes the
 * organization everywhere it shows (the sidebar, the profile, settings).
 * `actions` sits beside the save button (a Cancel in a dialog).
 */
export function OrganizationForm({ organization, onSaved, actions }: { organization: Organization; onSaved?: () => void; actions?: ReactNode }) {
  const t = useT()
  const client = useQueryClient()
  const { push } = useToast()
  const errors = useApiErrors()
  const [name, setName] = useState(organization.name)
  const [email, setEmail] = useState(organization.email ?? '')
  const [phone, setPhone] = useState(organization.phone ?? '')
  const [busy, setBusy] = useState(false)
  const dirty = name.trim() !== organization.name || email.trim() !== (organization.email ?? '') || phone.trim() !== (organization.phone ?? '')

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    errors.clear()
    try {
      await api('/organization', { method: 'PATCH', body: { name: name.trim(), email: email.trim() || null, phone: phone.trim() || null } })
      await client.invalidateQueries({ queryKey: ['organization'] })
      await client.invalidateQueries({ queryKey: ['session'] })
      push(t('form.saved'), 'success')
      onSaved?.()
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit} className="stack">
      <Field label={t('org.name')} error={errors.field('name')}>
        <input className="input" name="organization" autoComplete="organization" required value={name} onChange={(e) => setName(e.target.value)} />
      </Field>
      <div className="form-grid-2">
        <Field label={`${t('org.contactEmail')} (${t('form.optional')})`} error={errors.field('email')}>
          <input className="input" type="email" name="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} />
        </Field>
        <Field label={`${t('org.contactPhone')} (${t('form.optional')})`} error={errors.field('phone')}>
          <input className="input" type="tel" name="phone" autoComplete="tel" value={phone} onChange={(e) => setPhone(e.target.value)} />
        </Field>
      </div>
      {errors.box}
      <div className="form-actions">
        {actions}
        <button type="submit" className="btn primary" disabled={busy || !dirty || name.trim().length < 2}>
          {busy ? <Spinner /> : <Icon name="check" />} {busy ? t('form.saving') : t('form.save')}
        </button>
      </div>
    </form>
  )
}
