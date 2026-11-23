import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState, type FormEvent } from 'react'
import { api, type Schemas, type User } from '../api/client'
import { can, useSession } from '../auth/session'
import { initials } from '../format'
import { Field, PasswordInput, useApiErrors } from '../components/form'
import { Select } from '../components/inputs'
import { useToast } from '../components/Toasts'
import { Badge, Card, CopyField, Icon, PageHeader, Spinner } from '../components/ui'
import { OrganizationForm } from '../components/OrganizationForm'
import { useT } from '../i18n'
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
            <span className="avatar">{initials(owner.name)}</span>
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
        <div className="stack g-4">
          {sections.map((section) => (
            <div key={section.group.slug} className="stack">
              <div className="strong">{section.group.name}</div>
              {section.settings.map((item) => (
                <SettingRow key={item.key} item={item} />
              ))}
            </div>
          ))}
        </div>
      )}
    </Card>
  )
}

function SettingRow({ item }: { item: Setting }) {
  const t = useT()
  const client = useQueryClient()
  const { push } = useToast()
  const [draft, setDraft] = useState(() => display(item.value))
  const [busy, setBusy] = useState(false)
  useEffect(() => setDraft(display(item.value)), [item.value])

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

  const commit = () => {
    if (draft === display(item.value)) return
    if (item.type === 'int') {
      const number = Number(draft)
      if (!Number.isInteger(number)) return setDraft(display(item.value))
      return void save(number)
    }
    if (item.type === 'json') {
      try {
        return void save(JSON.parse(draft))
      } catch {
        return setDraft(display(item.value))
      }
    }
    void save(draft)
  }

  return (
    <div className="setting-row">
      <div className="setting-meta">
        <div>{item.description || item.key}</div>
      </div>
      <div className="setting-control">
        {item.type === 'bool' ? (
          <button
            type="button"
            role="switch"
            aria-checked={item.value === true}
            aria-label={item.description || item.key}
            className={`switch ${item.value === true ? 'on' : ''}`}
            disabled={busy}
            onClick={() => void save(item.value !== true)}
          />
        ) : (
          <input
            className="input"
            type={item.type === 'int' ? 'number' : 'text'}
            disabled={busy}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onBlur={commit}
            onKeyDown={(e) => {
              // Enter saves by leaving the field, so the change is sent once.
              if (e.key === 'Enter') {
                e.preventDefault()
                e.currentTarget.blur()
              }
            }}
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
