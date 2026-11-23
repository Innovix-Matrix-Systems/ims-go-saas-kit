import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState, type FormEvent } from 'react'
import { api, type Schemas, type User } from '../api/client'
import { can, useSession } from '../auth/session'
import { Field, PasswordInput, useApiErrors } from '../components/form'
import { Select } from '../components/inputs'
import { useToast } from '../components/Toasts'
import { Avatar, Badge, Card, CopyField, Icon, PageHeader, Spinner } from '../components/ui'
import { OrganizationForm } from '../components/OrganizationForm'
import { useT, type MessageKey } from '../i18n'
import { FormSkeleton, ListSkeleton, SettingsSkeleton } from '../components/Skeleton'
import { organizationQuery, type Organization } from '../components/organization'

export const Route = createFileRoute('/_app/org/settings')({
  component: OrganizationSettingsPage,
})

type SettingsSection = Schemas['SettingsGroupSection']
type Setting = Schemas['SettingItem']

function OrganizationSettingsPage() {
  const t = useT()
  const session = useSession()
  const organization = useQuery(organizationQuery)
  return (
    <>
      <PageHeader title={t('org.settings')} description={organization.data?.name} />
      {!organization.data ? (
        <div className="stack g-4">
          <Card>
            <FormSkeleton fields={3} />
          </Card>
          <Card>
            <ListSkeleton rows={1} avatar />
          </Card>
        </div>
      ) : (
        <div className="stack g-4">
          <ProfileCard organization={organization.data} editable={can(session, 'organization.update')} />
          <OwnerCard organization={organization.data} />
          {can(session, 'setting.update') ? <PoliciesCard /> : null}
        </div>
      )}
    </>
  )
}

function ProfileCard({ organization, editable }: { organization: Organization; editable: boolean }) {
  const t = useT()
  return (
    <Card title={t('org.profileTitle')} description={t('org.profileHint')}>
      <div className="stack g-4 form-narrow">
        <dl className="details">
          <dt>{t('orgPage.id')}</dt>
          <dd>
            <CopyField value={String(organization.id)} />
          </dd>
          <dt>{t('org.code')}</dt>
          <dd>
            <CopyField value={organization.code} short />
          </dd>
        </dl>
        {editable ? (
          <OrganizationForm organization={organization} />
        ) : (
          <dl className="details">
            <dt>{t('org.name')}</dt>
            <dd>{organization.name}</dd>
            <dt>{t('org.contactEmail')}</dt>
            <dd>{organization.email ?? <span className="muted">{t('profilePage.notSet')}</span>}</dd>
            <dt>{t('org.contactPhone')}</dt>
            <dd>{organization.phone ?? <span className="muted">{t('profilePage.notSet')}</span>}</dd>
          </dl>
        )}
      </div>
    </Card>
  )
}

function OwnerCard({ organization }: { organization: Organization }) {
  const t = useT()
  const owner = organization.owner
  return (
    <Card title={t('org.ownerTitle')}>
      <div className="stack g-4">
        {owner ? (
          <div className="owner-card">
            <Avatar name={owner.name} />
            <div className="member-meta">
              <span className="member-name">
                {owner.name} {organization.is_owner ? <Badge tone="info">{t('org.ownerYou')}</Badge> : null}
              </span>
              <span className="small muted">{owner.email}</span>
            </div>
          </div>
        ) : (
          <p className="muted small">{t('org.ownerNone')}</p>
        )}
        {organization.is_owner ? <TransferForm ownerId={owner?.id} /> : null}
      </div>
    </Card>
  )
}

function TransferForm({ ownerId }: { ownerId?: number }) {
  const t = useT()
  const client = useQueryClient()
  const { push } = useToast()
  const errors = useApiErrors()
  const [userId, setUserId] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const members = useQuery({
    queryKey: ['members', 'accepted-all'],
    queryFn: () => api<User[]>('/user', { query: { invitation: 'accepted', per_page: 100, order_by: 'name', order_direction: 'asc' } }),
  })
  const candidates = (members.data ?? []).filter((user) => user.id !== ownerId && user.is_active)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    errors.clear()
    try {
      await api('/organization/transfer-ownership', { method: 'POST', body: { user_id: Number(userId), password } })
      setPassword('')
      setUserId('')
      await client.invalidateQueries({ queryKey: ['organization'] })
      await client.invalidateQueries({ queryKey: ['members'] })
      push(t('org.transferred'), 'success')
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit} className="stack form-narrow">
      <div>
        <div className="strong">{t('org.transferTitle')}</div>
        <p className="small muted">{t('org.transferHint')}</p>
      </div>
      <Field label={t('org.newOwner')} error={errors.field('user_id')}>
        <Select
          value={userId}
          placeholder={t('org.newOwner')}
          ariaLabel={t('org.newOwner')}
          onChange={setUserId}
          options={candidates.map((user) => ({ value: String(user.id), label: user.name, text: `${user.name} ${user.email}`, hint: user.email }))}
        />
      </Field>
      <Field label={t('auth.password')} error={errors.field('password')}>
        <PasswordInput name="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
      </Field>
      {errors.box}
      <div>
        <button type="submit" className="btn danger" disabled={busy || !userId || !password}>
          {busy ? <Spinner /> : <Icon name="key" />} {t('org.transfer')}
        </button>
      </div>
    </form>
  )
}

// What an organization may set, in words its admins use. Settings the app
// does not know yet fall back to the API's description.
interface Known {
  label: MessageKey
  hint: MessageKey
  unit?: MessageKey
  kind?: 'color' | 'url'
  placeholder?: string
}

const knownSettings: Record<string, Known> = {
  'notification.enabled': { label: 'policy.notifications', hint: 'policy.notificationsHint' },
  'data_processing_job.cleanup_days': { label: 'policy.jobsKeep', hint: 'policy.jobsKeepHint', unit: 'policy.days' },
  'branding.name': { label: 'policy.brandName', hint: 'policy.brandNameHint' },
  'branding.logo_url': { label: 'policy.brandLogo', hint: 'policy.brandLogoHint', kind: 'url', placeholder: 'https://' },
  'branding.color': { label: 'policy.brandColor', hint: 'policy.brandColorHint', kind: 'color' },
  'branding.footer': { label: 'policy.brandFooter', hint: 'policy.brandFooterHint' },
  'security.new_sign_in_alert': { label: 'policy.signInAlert', hint: 'policy.signInAlertHint' },
  'security.require_two_factor': { label: 'policy.requireTwoFactor', hint: 'policy.requireTwoFactorHint' },
  'privacy.deletion_grace_days': { label: 'policy.deletionGrace', hint: 'policy.deletionGraceHint', unit: 'policy.days' },
}

const knownGroups: Record<string, MessageKey> = {
  notifications: 'policy.groupNotifications',
  'data-processing': 'policy.groupJobs',
  data_processing: 'policy.groupJobs',
  branding: 'policy.groupBranding',
  security: 'policy.groupSecurity',
  privacy: 'policy.groupPrivacy',
}

// The organization's own overrides of tenant-scoped settings, grouped as the
// API returns them.
function PoliciesCard() {
  const t = useT()
  const settings = useQuery({ queryKey: ['settings'], queryFn: () => api<SettingsSection[]>('/settings') })
  const sections = (settings.data ?? [])
    .map((section) => ({ ...section, settings: section.settings.filter((item) => item.scope === 'tenant' && item.editable) }))
    .filter((section) => section.settings.length > 0)

  return (
    <Card title={t('org.policiesTitle')} description={t('org.policiesHint')}>
      {!settings.data ? (
        <SettingsSkeleton rows={5} />
      ) : (
        <div className="policy-groups">
          {sections.map((section) => {
            const group = knownGroups[section.group.slug]
            return (
              <section key={section.group.slug} className="policy-group">
                <h3 className="policy-group-title">{group ? t(group) : section.group.name}</h3>
                {section.settings.map((item) => (
                  <SettingRow key={item.key} item={item} />
                ))}
              </section>
            )
          })}
        </div>
      )}
    </Card>
  )
}

function SettingRow({ item }: { item: Setting }) {
  const t = useT()
  const client = useQueryClient()
  const { push } = useToast()
  const known = knownSettings[item.key]
  const [draft, setDraft] = useState(() => display(item.value))
  const [busy, setBusy] = useState(false)
  useEffect(() => setDraft(display(item.value)), [item.value])
  const label = known ? t(known.label) : item.description || item.key
  const id = `setting-${item.key.replace(/[^a-z0-9]+/gi, '-')}`

  const save = async (value: unknown) => {
    setBusy(true)
    try {
      await api(`/settings/tenant/${encodeURIComponent(item.key)}`, { method: 'PATCH', body: { value } })
      await client.invalidateQueries({ queryKey: ['settings'] })
      push(t('form.saved'), 'success')
    } catch (cause) {
      push(cause instanceof Error ? cause.message : String(cause), 'error')
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
      return void save(number)
    }
    if (item.type === 'json') {
      try {
        return void save(JSON.parse(next))
      } catch {
        return setDraft(display(item.value))
      }
    }
    void save(next)
  }

  const keys = (e: React.KeyboardEvent<HTMLInputElement>) => {
    // Enter saves by leaving the field, so the change is sent once.
    if (e.key === 'Enter') {
      e.preventDefault()
      e.currentTarget.blur()
    }
  }

  return (
    <div className="setting-row">
      <div className="setting-meta">
        <label className="setting-label" htmlFor={id}>
          {label}
        </label>
        {known ? <div className="setting-hint">{t(known.hint)}</div> : null}
      </div>
      <div className="setting-control">
        {item.type === 'bool' ? (
          <button
            id={id}
            type="button"
            role="switch"
            aria-checked={item.value === true}
            aria-label={label}
            className={`switch ${item.value === true ? 'on' : ''}`}
            disabled={busy}
            onClick={() => void save(item.value !== true)}
          />
        ) : known?.kind === 'color' ? (
          <span className="setting-color">
            <input
              type="color"
              aria-label={label}
              disabled={busy}
              value={/^#[0-9a-f]{6}$/i.test(draft) ? draft : '#000000'}
              onChange={(e) => setDraft(e.target.value)}
              onBlur={(e) => commit(e.target.value)}
            />
            <input id={id} className="input mono" disabled={busy} value={draft} maxLength={7} onChange={(e) => setDraft(e.target.value)} onBlur={() => commit()} onKeyDown={keys} />
          </span>
        ) : known?.unit ? (
          <span className="input-unit">
            <input id={id} className="input" type="number" min="0" disabled={busy} value={draft} onChange={(e) => setDraft(e.target.value)} onBlur={() => commit()} onKeyDown={keys} />
            <span className="input-unit-label">{t(known.unit)}</span>
          </span>
        ) : (
          <input
            id={id}
            className="input"
            type={item.type === 'int' ? 'number' : known?.kind === 'url' ? 'url' : 'text'}
            placeholder={known?.placeholder}
            disabled={busy}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onBlur={() => commit()}
            onKeyDown={keys}
          />
        )}
      </div>
    </div>
  )
}

function display(value: unknown): string {
  if (value == null) return ''
  return typeof value === 'string' ? value : JSON.stringify(value)
}
