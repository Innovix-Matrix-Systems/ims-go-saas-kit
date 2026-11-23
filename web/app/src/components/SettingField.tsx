import { useQuery } from '@tanstack/react-query'
import { useEffect, useState, type KeyboardEvent, type ReactNode } from 'react'
import { api, type Schemas } from '../api/client'
import { useT, type MessageKey } from '../i18n'
import { Select } from './inputs'

// Settings in words the people changing them use. A key the app does not
// know yet falls back to the API's description.

export type Setting = Schemas['SettingItem']

interface Known {
  label: MessageKey
  hint: MessageKey
  unit?: MessageKey
  kind?: 'color' | 'url' | 'multiline' | 'package'
  placeholder?: string
}

const knownSettings: Record<string, Known> = {
  'system.name': { label: 'policy.systemName', hint: 'policy.systemNameHint' },
  'notification.enabled': { label: 'policy.notifications', hint: 'policy.notificationsHint' },
  'data_processing_job.cleanup_days': { label: 'policy.jobsKeep', hint: 'policy.jobsKeepHint', unit: 'policy.days' },
  'branding.name': { label: 'policy.brandName', hint: 'policy.brandNameHint' },
  'branding.logo_url': { label: 'policy.brandLogo', hint: 'policy.brandLogoHint', kind: 'url', placeholder: 'https://' },
  'branding.color': { label: 'policy.brandColor', hint: 'policy.brandColorHint', kind: 'color' },
  'branding.footer': { label: 'policy.brandFooter', hint: 'policy.brandFooterHint' },
  'security.new_sign_in_alert': { label: 'policy.signInAlert', hint: 'policy.signInAlertHint' },
  'security.require_two_factor': { label: 'policy.requireTwoFactor', hint: 'policy.requireTwoFactorHint' },
  'privacy.deletion_grace_days': { label: 'policy.deletionGrace', hint: 'policy.deletionGraceHint', unit: 'policy.days' },
  'privacy.export_ttl_hours': { label: 'policy.exportTtl', hint: 'policy.exportTtlHint', unit: 'policy.hours' },
  'privacy.erase_soft_deleted_after_days': { label: 'policy.eraseDeleted', hint: 'policy.eraseDeletedHint', unit: 'policy.days' },
  'signup.enabled': { label: 'policy.signupOpen', hint: 'policy.signupOpenHint' },
  'signup.package_id': { label: 'policy.signupPlan', hint: 'policy.signupPlanHint', kind: 'package' },
  'signup.terms_version': { label: 'policy.termsVersion', hint: 'policy.termsVersionHint' },
  'billing.payment_instructions': { label: 'policy.paymentInstructions', hint: 'policy.paymentInstructionsHint', kind: 'multiline' },
}

const knownGroups: Record<string, MessageKey> = {
  general: 'policy.groupGeneral',
  notifications: 'policy.groupNotifications',
  'data-processing': 'policy.groupJobs',
  branding: 'policy.groupBranding',
  security: 'policy.groupSecurity',
  privacy: 'policy.groupPrivacy',
  signup: 'policy.groupSignup',
  billing: 'policy.groupBilling',
}

/** A settings group's name, translated when the app knows it. */
export function useGroupName() {
  const t = useT()
  return (slug: string, fallback: string) => (knownGroups[slug] ? t(knownGroups[slug]) : fallback)
}

function display(value: unknown): string {
  if (value == null) return ''
  return typeof value === 'string' ? value : JSON.stringify(value)
}

const packagesQuery = {
  queryKey: ['console', 'packages'],
  queryFn: () => api<Schemas['Package'][]>('/packages', { asStaff: true }),
}

/**
 * One setting: its name and what it does, and the control that changes it
 * (a switch, a number with its unit, a colour, a plan, text). Changes are
 * saved when the field is left; `save` sends them and reports failures.
 */
export function SettingRow({
  item,
  save,
  editable = true,
  badge,
}: {
  item: Setting
  save: (value: unknown) => Promise<void>
  editable?: boolean
  badge?: ReactNode
}) {
  const t = useT()
  const known = knownSettings[item.key]
  const [draft, setDraft] = useState(() => display(item.value))
  const [busy, setBusy] = useState(false)
  useEffect(() => setDraft(display(item.value)), [item.value])
  const packages = useQuery({ ...packagesQuery, enabled: known?.kind === 'package' })
  const label = known ? t(known.label) : item.description || item.key
  const id = `setting-${item.key.replace(/[^a-z0-9]+/gi, '-')}`
  const disabled = busy || !editable

  const send = async (value: unknown) => {
    setBusy(true)
    try {
      await save(value)
    } catch {
      setDraft(display(item.value))
    } finally {
      setBusy(false)
    }
  }

  const commit = (next = draft) => {
    if (next === display(item.value)) return
    if (item.type === 'int') {
      const number = Number(next)
      if (!Number.isInteger(number)) return setDraft(display(item.value))
      return void send(number)
    }
    if (item.type === 'json') {
      try {
        return void send(JSON.parse(next))
      } catch {
        return setDraft(display(item.value))
      }
    }
    void send(next)
  }

  const keys = (e: KeyboardEvent<HTMLInputElement>) => {
    // Enter saves by leaving the field, so the change is sent once.
    if (e.key === 'Enter') {
      e.preventDefault()
      e.currentTarget.blur()
    }
  }

  let control: ReactNode
  if (item.type === 'bool') {
    control = (
      <button
        id={id}
        type="button"
        role="switch"
        aria-checked={item.value === true}
        aria-label={label}
        className={`switch ${item.value === true ? 'on' : ''}`}
        disabled={disabled}
        onClick={() => void send(item.value !== true)}
      />
    )
  } else if (known?.kind === 'package') {
    control = (
      <Select
        value={draft}
        ariaLabel={label}
        disabled={disabled}
        onChange={(value) => {
          setDraft(value)
          commit(value)
        }}
        options={[
          { value: '0', label: t('policy.signupPlanAuto') },
          ...(packages.data ?? []).map((pkg) => ({ value: String(pkg.id), label: pkg.name, hint: pkg.is_active ? undefined : t('policy.inactive') })),
        ]}
      />
    )
  } else if (known?.kind === 'color') {
    control = (
      <span className="setting-color">
        <input
          type="color"
          aria-label={label}
          disabled={disabled}
          value={/^#[0-9a-f]{6}$/i.test(draft) ? draft : '#000000'}
          onChange={(e) => setDraft(e.target.value)}
          onBlur={(e) => commit(e.target.value)}
        />
        <input id={id} className="input mono" disabled={disabled} value={draft} maxLength={7} onChange={(e) => setDraft(e.target.value)} onBlur={() => commit()} onKeyDown={keys} />
      </span>
    )
  } else if (known?.kind === 'multiline') {
    control = <textarea id={id} className="input" rows={5} disabled={disabled} value={draft} onChange={(e) => setDraft(e.target.value)} onBlur={() => commit()} />
  } else if (known?.unit) {
    control = (
      <span className="input-unit">
        <input id={id} className="input" type="number" min="0" disabled={disabled} value={draft} onChange={(e) => setDraft(e.target.value)} onBlur={() => commit()} onKeyDown={keys} />
        <span className="input-unit-label">{t(known.unit)}</span>
      </span>
    )
  } else {
    control = (
      <input
        id={id}
        className="input"
        type={item.type === 'int' ? 'number' : known?.kind === 'url' ? 'url' : 'text'}
        placeholder={known?.placeholder}
        disabled={disabled}
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={() => commit()}
        onKeyDown={keys}
      />
    )
  }

  return (
    <div className={`setting-row ${known?.kind === 'multiline' ? 'wide' : ''}`}>
      <div className="setting-meta">
        <label className="setting-label" htmlFor={id}>
          {label}
        </label>
        {known ? <div className="setting-hint">{t(known.hint)}</div> : null}
        {badge ? <div className="mt-4">{badge}</div> : null}
      </div>
      <div className="setting-control">{control}</div>
    </div>
  )
}
