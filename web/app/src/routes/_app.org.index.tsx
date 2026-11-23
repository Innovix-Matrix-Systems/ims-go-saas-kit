import { Link, createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { api, type Schemas, type User } from '../api/client'
import { can, useSession } from '../auth/session'
import { formatBytes, relativeTimeIn } from '../format'
import { intlLocale, useLocale, useT } from '../i18n'
import { subscriptionStatusQuery } from '../components/billing'
import { OrganizationForm } from '../components/OrganizationForm'
import { organizationQuery } from '../components/organization'
import { PlanCard } from '../components/PlanCard'
import { FormSkeleton } from '../components/Skeleton'
import { OrgMark } from '../components/WorkspaceMenu'
import { Avatar, Badge, Button, Card, CopyField, Icon, Modal, type IconName } from '../components/ui'

export const Route = createFileRoute('/_app/org/')({
  component: OrganizationProfilePage,
})

type TenantDashboard = Schemas['TenantDashboard']
type Usage = Schemas['BillingUsage']

/** The organization's profile: who it is, how to reach it, who runs it, and its size. */
function OrganizationProfilePage() {
  const t = useT()
  const locale = intlLocale(useLocale())
  const session = useSession()
  const [editing, setEditing] = useState(false)
  const organization = useQuery(organizationQuery)
  const figures = useQuery({ queryKey: ['dashboard', 'org', 'profile'], queryFn: () => api<TenantDashboard>('/dashboard', { query: { days: 30 } }) })
  const billing = can(session, 'billing.view')
  const usage = useQuery({ queryKey: ['billing', 'usage'], queryFn: () => api<Usage>('/billing/usage'), enabled: billing })
  const subscription = useQuery({ ...subscriptionStatusQuery, enabled: billing })
  const membersAllowed = can(session, 'user.view.all')
  const members = useQuery({
    queryKey: ['members', 'preview'],
    queryFn: () => api<{ data: User[]; meta: Schemas['PaginationMeta'] }>('/user', { envelope: true, query: { per_page: 8, order_by: 'name', order_direction: 'asc' } }),
    enabled: membersAllowed,
  })
  const org = organization.data
  if (!org) {
    return (
      <Card>
        <FormSkeleton fields={4} />
      </Card>
    )
  }
  const editable = can(session, 'organization.update')
  const data = figures.data
  const plan = subscription.data?.package?.name
  const stats: { icon: IconName; label: string; value: string; to: string }[] = []
  if (data?.members) stats.push({ icon: 'users', label: t('dashboard.members'), value: String(data.members.total), to: '/org/members' })
  if (data?.branches != null) stats.push({ icon: 'tenants', label: t('org.branches'), value: String(data.branches), to: '/org/branches' })
  if (data?.roles != null) stats.push({ icon: 'shield', label: t('org.roles'), value: String(data.roles), to: '/org/roles' })
  if (data?.files) stats.push({ icon: 'files', label: t('dashboard.storage'), value: formatBytes(data.files.bytes), to: '/org/files' })

  return (
    <div className="profile">
      <section className="profile-hero card">
        <div className="profile-cover org" aria-hidden="true" />
        <div className="profile-hero-body">
          <div className="profile-photo">
            <OrgMark name={org.name} size="xl" />
          </div>
          <div className="profile-identity">
            <h1>{org.name}</h1>
            <div className="profile-badges">
              {org.is_active ? <Badge tone="ok">{t('dashboard.active')}</Badge> : <Badge tone="warn">{t('dashboard.suspended')}</Badge>}
              {plan ? <Badge tone="accent">{t('workspace.plan', { plan })}</Badge> : null}
              {org.is_owner ? <Badge tone="info">{t('org.ownerYou')}</Badge> : null}
              <span className="small muted">{t('orgPage.since', { date: new Intl.DateTimeFormat(locale, { dateStyle: 'long' }).format(new Date(org.created_at)) })}</span>
            </div>
          </div>
          <div className="profile-actions">
            {editable ? (
              <Button icon="edit" onClick={() => setEditing(true)}>
                {t('orgPage.edit')}
              </Button>
            ) : null}
            <Link to="/org/settings" className="btn ghost">
              <Icon name="settings" />
              {t('nav.orgSettings')}
            </Link>
          </div>
        </div>
      </section>

      {stats.length > 0 ? (
        <div className="profile-stats">
          {stats.map((stat) => (
            <Link key={stat.to} to={stat.to} className="profile-stat">
              <span className="quick-action-icon">
                <Icon name={stat.icon} />
              </span>
              <span>
                <span className="profile-stat-value">{stat.value}</span>
                <span className="profile-stat-label">{stat.label}</span>
              </span>
            </Link>
          ))}
        </div>
      ) : null}

      <div className="profile-grid">
        <div className="stack g-4">
          <Card title={t('orgPage.about')} actions={editable ? <button type="button" className="link-btn" onClick={() => setEditing(true)}>{t('orgPage.edit')}</button> : undefined}>
            <dl className="details">
              <dt>{t('org.name')}</dt>
              <dd>{org.name}</dd>
              <dt>{t('org.contactEmail')}</dt>
              <dd>{org.email ? <a href={`mailto:${org.email}`}>{org.email}</a> : <span className="muted">{t('profilePage.notSet')}</span>}</dd>
              <dt>{t('org.contactPhone')}</dt>
              <dd>{org.phone ? <a href={`tel:${org.phone}`}>{org.phone}</a> : <span className="muted">{t('profilePage.notSet')}</span>}</dd>
              <dt>{t('orgPage.created')}</dt>
              <dd>{relativeTimeIn(locale, org.created_at)}</dd>
              <dt>{t('org.code')}</dt>
              <dd>
                <CopyField value={org.code} short />
              </dd>
              <dt>{t('orgPage.id')}</dt>
              <dd>
                <CopyField value={String(org.id)} />
              </dd>
            </dl>
          </Card>

          {membersAllowed ? (
            <Card
              title={t('orgPage.people')}
              description={members.data ? t('orgPage.peopleCount', { count: members.data.meta.total }) : undefined}
              actions={<Link to="/org/members" className="link-btn">{t('dashboard.seeAll')}</Link>}
            >
              <ul className="people-grid">
                {(members.data?.data ?? []).map((member) => (
                  <li key={member.id} className="person">
                    <Avatar name={member.name} photo={member.photo} size="sm" />
                    <span className="member-meta">
                      <span className="member-name">{member.name}</span>
                      <span className="small muted">{member.roles?.map((role) => role.name).join(', ') || t('role.member')}</span>
                    </span>
                  </li>
                ))}
              </ul>
            </Card>
          ) : null}
        </div>

        <div className="stack g-4">
          <Card title={t('org.ownerTitle')}>
            {org.owner ? (
              <div className="profile-org">
                <Avatar name={org.owner.name} />
                <div className="member-meta">
                  <span className="member-name">{org.owner.name}</span>
                  <a className="small" href={`mailto:${org.owner.email}`}>
                    {org.owner.email}
                  </a>
                </div>
              </div>
            ) : (
              <p className="small muted">{t('org.ownerNone')}</p>
            )}
            {org.is_owner ? (
              <p className="small muted mt-8">
                {t('orgPage.transferHint')} <Link to="/org/settings">{t('nav.orgSettings')}</Link>
              </p>
            ) : null}
          </Card>
          {billing ? <PlanCard usage={usage.data} status={subscription.data} /> : null}
        </div>
      </div>

      <Modal open={editing} title={t('orgPage.edit')} description={t('orgPage.editHint')} onClose={() => setEditing(false)}>
        <OrganizationForm
          organization={org}
          onSaved={() => setEditing(false)}
          actions={
            <button type="button" className="btn ghost" onClick={() => setEditing(false)}>
              {t('common.cancel')}
            </button>
          }
        />
      </Modal>
    </div>
  )
}
