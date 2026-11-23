import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { api, type Schemas, type User } from '../api/client'
import { can, useSession } from '../auth/session'
import { Field, PasswordInput, useApiErrors } from '../components/form'
import { Select } from '../components/inputs'
import { useToast } from '../components/Toasts'
import { Avatar, Badge, Card, CopyField, Icon, PageHeader, Spinner } from '../components/ui'
import { OrganizationForm } from '../components/OrganizationForm'
import { useT } from '../i18n'
import { SettingRow, useGroupName, type Setting } from '../components/SettingField'
import { FormSkeleton, ListSkeleton, SettingsSkeleton } from '../components/Skeleton'
import { organizationQuery, type Organization } from '../components/organization'

export const Route = createFileRoute('/_app/org/settings')({
  component: OrganizationSettingsPage,
})

type SettingsSection = Schemas['SettingsGroupSection']

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

// The organization's own overrides of tenant-scoped settings, grouped as the
// API returns them.
function PoliciesCard() {
  const t = useT()
  const client = useQueryClient()
  const { push } = useToast()
  const groupName = useGroupName()
  const settings = useQuery({ queryKey: ['settings'], queryFn: () => api<SettingsSection[]>('/settings') })
  const sections = (settings.data ?? [])
    .map((section) => ({ ...section, settings: section.settings.filter((item) => item.scope === 'tenant' && item.editable) }))
    .filter((section) => section.settings.length > 0)

  const save = (item: Setting) => async (value: unknown) => {
    try {
      await api(`/settings/tenant/${encodeURIComponent(item.key)}`, { method: 'PATCH', body: { value } })
      await client.invalidateQueries({ queryKey: ['settings'] })
      if (item.key.startsWith('branding.')) await client.invalidateQueries({ queryKey: ['organization'] })
      push(t('form.saved'), 'success')
    } catch (cause) {
      push(cause instanceof Error ? cause.message : String(cause), 'error')
      throw cause
    }
  }

  return (
    <Card title={t('org.policiesTitle')} description={t('org.policiesHint')}>
      {!settings.data ? (
        <SettingsSkeleton rows={5} />
      ) : (
        <div className="policy-groups">
          {sections.map((section) => (
            <section key={section.group.slug} className="policy-group">
              <h3 className="policy-group-title">{groupName(section.group.slug, section.group.name)}</h3>
              {section.settings.map((item) => (
                <SettingRow key={item.key} item={item} save={save(item)} />
              ))}
            </section>
          ))}
        </div>
      )}
    </Card>
  )
}
