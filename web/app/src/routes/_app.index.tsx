import { Link, createFileRoute } from '@tanstack/react-router'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { onboardingQuery } from './_app.onboarding'
import { api, type Schemas } from '../api/client'
import { can, useSession, type Session } from '../auth/session'
import { formatBytes, initials, relativeTimeIn } from '../format'
import { intlLocale, useLocale, useT, type MessageKey } from '../i18n'
import { actionLabel } from '../components/ActivityLog'
import { subscriptionStatusQuery, useMoney } from '../components/billing'
import { DonutChart, Delta, KpiCard, RankedBarChart, TrendChart } from '../components/charts'
import { PlanCard } from '../components/PlanCard'
import { GettingStarted } from '../components/GettingStarted'
import { fetchFeed, feedKey } from '../components/notifications'
import { organizationQuery } from '../components/organization'
import { Avatar, Badge, Card, EmptyState, Icon, type IconName } from '../components/ui'

export const Route = createFileRoute('/_app/')({
  component: HomePage,
})

type TenantDashboard = Schemas['TenantDashboard']
type PlatformDashboard = Schemas['PlatformDashboard']
type Usage = Schemas['BillingUsage']
type ActivityEntry = Schemas['ActivityEntry']

const fileTypeLabels: Record<string, MessageKey> = {
  image: 'files.image',
  video: 'files.video',
  document: 'files.document',
  other: 'files.other',
}

const PERIOD_KEY = 'ims.app.dashboard.days'
const periods = [7, 30, 90] as const
type Days = (typeof periods)[number]

/** The chosen period, remembered on this device. */
function usePeriod() {
  const [days, setDays] = useState<Days>(() => {
    try {
      const stored = Number(localStorage.getItem(PERIOD_KEY))
      return (periods as readonly number[]).includes(stored) ? (stored as Days) : 30
    } catch {
      return 30
    }
  })
  useEffect(() => {
    try {
      localStorage.setItem(PERIOD_KEY, String(days))
    } catch {
      // Storage is off; the period is just not remembered.
    }
  }, [days])
  return [days, setDays] as const
}

const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'

function useNumbers() {
  const locale = intlLocale(useLocale())
  return useMemo(() => {
    const plain = new Intl.NumberFormat(locale)
    const compact = new Intl.NumberFormat(locale, { notation: 'compact', maximumFractionDigits: 1 })
    return { plain: (value: number) => plain.format(value), compact: (value: number) => compact.format(value) }
  }, [locale])
}

function PeriodPicker({ days, onChange }: { days: Days; onChange: (days: Days) => void }) {
  const t = useT()
  return (
    <div className="segmented" role="group" aria-label={t('dashboard.period')}>
      {periods.map((value) => (
        <button key={value} type="button" className={days === value ? 'active' : ''} aria-pressed={days === value} onClick={() => onChange(value)}>
          {t('dashboard.days', { count: value })}
        </button>
      ))}
    </div>
  )
}

function Hero({ title, subtitle, actions }: { title: ReactNode; subtitle: ReactNode; actions?: ReactNode }) {
  const session = useSession()
  return (
    <div className="dash-hero">
      <Avatar name={session?.user.name ?? ''} photo={session?.user.photo} size="lg" />
      <div className="dash-hero-text">
        <h1>{title}</h1>
        <p>{subtitle}</p>
      </div>
      {actions ? <div className="dash-hero-actions">{actions}</div> : null}
    </div>
  )
}

function DashboardSkeleton() {
  return (
    <div className="dash-skeleton" aria-busy="true">
      <div className="dash-kpis">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="kpi-card skeleton" />
        ))}
      </div>
      <div className="dash-row wide">
        <div className="card skeleton" />
        <div className="card skeleton" />
      </div>
    </div>
  )
}

interface QuickAction {
  to: string
  icon: IconName
  label: MessageKey
  hint: MessageKey
  show: boolean
}

function QuickActions({ actions }: { actions: QuickAction[] }) {
  const t = useT()
  const shown = actions.filter((action) => action.show)
  if (shown.length === 0) return null
  return (
    <Card title={t('dashboard.quickActions')}>
      <ul className="quick-actions">
        {shown.map((action) => (
          <li key={action.to + action.label}>
            <Link to={action.to} className="quick-action">
              <span className="quick-action-icon">
                <Icon name={action.icon} />
              </span>
              <span className="quick-action-text">
                <span className="quick-action-label">{t(action.label)}</span>
                <span className="quick-action-hint">{t(action.hint)}</span>
              </span>
              <Icon name="chevron" className="quick-action-go" />
            </Link>
          </li>
        ))}
      </ul>
    </Card>
  )
}

function HomePage() {
  const session = useSession()
  const onboarding = useQuery(onboardingQuery)
  const t = useT()
  if (!session) return null
  // Platform staff (not impersonating or logged in as someone) see the platform.
  const staff = session.isPlatformUser && !session.acting
  return (
    <>
      {onboarding.data && onboarding.data.is_owner && !onboarding.data.completed ? (
        <div className="banner info mb-16">
          <span>{t('onboarding.banner')}</span>
          <Link to="/onboarding" className="btn primary">
            {t('onboarding.resume')}
          </Link>
        </div>
      ) : null}
      {staff ? <PlatformHome session={session} /> : <OrganizationHome session={session} />}
    </>
  )
}

/* ── Staff ─────────────────────────────────────────────── */

function PlatformHome({ session }: { session: Session }) {
  const t = useT()
  const locale = useLocale()
  const n = useNumbers()
  const money = useMoney()
  const [days, setDays] = usePeriod()
  const dashboard = useQuery({
    queryKey: ['dashboard', 'platform', days, timeZone],
    queryFn: () => api<PlatformDashboard>('/internal/dashboard', { asStaff: true, query: { days, tz: timeZone } }),
    placeholderData: keepPreviousData,
  })
  const data = dashboard.data
  const lead = data?.revenue?.currencies[0]
  const pending = data?.revenue?.pending ?? []
  const pendingCount = pending.reduce((sum, row) => sum + row.count, 0)
  const moneyCompact = (currency: string) => (value: number) => money(value, currency, { compact: true })

  return (
    <>
      <Hero
        title={t('home.greeting', { name: session.user.first_name || session.user.name })}
        subtitle={t('home.staffSubtitle')}
        actions={<PeriodPicker days={days} onChange={setDays} />}
      />
      {!data ? (
        <DashboardSkeleton />
      ) : (
        <div className={`dash ${dashboard.isPlaceholderData ? 'is-updating' : ''}`}>
          <div className="dash-kpis">
            {data.organizations ? (
              <KpiCard
                label={t('console.organizations')}
                value={n.plain(data.organizations.total)}
                delta={<Delta current={data.organizations.added.total} previous={data.organizations.added.previous_total} />}
                sub={t('dashboard.newInPeriod', { count: n.plain(data.organizations.added.total) })}
                points={data.organizations.added.points}
                to="/console/organizations"
              />
            ) : null}
            {data.users ? (
              <KpiCard
                label={t('dashboard.members')}
                value={n.plain(data.users.total)}
                delta={<Delta current={data.users.added.total} previous={data.users.added.previous_total} />}
                sub={t('dashboard.signedIn', { count: n.plain(data.users.signed_in_recently) })}
                points={data.users.added.points}
                to="/console/users"
              />
            ) : null}
            {data.revenue ? (
              <KpiCard
                label={t('dashboard.revenue')}
                value={lead ? money(lead.amount.total, lead.currency) : '—'}
                delta={lead ? <Delta current={lead.amount.total} previous={lead.amount.previous_total} /> : undefined}
                sub={lead ? t('dashboard.payments', { count: n.plain(lead.payments) }) : t('dashboard.noRevenue')}
                points={lead?.amount.points}
              />
            ) : null}
            {data.revenue ? (
              <KpiCard
                label={t('home.paymentsToReview')}
                value={<span className={pendingCount > 0 ? 'kpi-attention' : ''}>{n.plain(pendingCount)}</span>}
                sub={pending.length > 0 ? pending.map((row) => money(row.amount, row.currency)).join(' · ') : t('dashboard.allClear')}
                to="/console/approvals"
              />
            ) : null}
          </div>

          <div className="dash-row wide">
            {data.revenue ? (
              <Card title={t('dashboard.revenue')} description={lead ? t('dashboard.revenueHint', { currency: lead.currency }) : t('dashboard.noRevenue')}>
                {lead ? (
                  <TrendChart points={lead.amount.points} label={t('dashboard.revenue')} format={moneyCompact(lead.currency)} />
                ) : (
                  <EmptyState icon="package" title={t('dashboard.noRevenue')} description={t('dashboard.noRevenueHint')} />
                )}
              </Card>
            ) : data.organizations ? (
              <Card title={t('dashboard.newOrganizations')}>
                <TrendChart points={data.organizations.added.points} label={t('dashboard.newOrganizations')} format={n.compact} />
              </Card>
            ) : null}
            {data.plans ? (
              <Card title={t('dashboard.plans')} description={t('dashboard.plansHint')} actions={<Link to="/console/plans" className="link-btn">{t('dashboard.manage')}</Link>}>
                <DonutChart
                  slices={data.plans.map((plan) => ({ key: String(plan.package_id), label: plan.name, value: plan.organizations }))}
                  format={n.plain}
                  totalLabel={t('dashboard.organizationsShort')}
                  empty={t('dashboard.noPlans')}
                />
              </Card>
            ) : null}
          </div>

          <div className="dash-row">
            {data.organizations && data.revenue ? (
              <Card title={t('dashboard.newOrganizations')}>
                <TrendChart points={data.organizations.added.points} label={t('dashboard.newOrganizations')} format={n.compact} height={200} />
              </Card>
            ) : null}
            {data.users ? (
              <Card title={t('dashboard.newMembers')}>
                <TrendChart points={data.users.added.points} label={t('dashboard.newMembers')} format={n.compact} height={200} />
              </Card>
            ) : null}
            {data.organizations ? (
              <Card title={t('dashboard.orgStatus')} description={t('dashboard.orgStatusHint')}>
                <DonutChart
                  slices={[
                    { key: 'active', label: t('dashboard.active'), value: data.organizations.active, to: '/console/organizations' },
                    { key: 'suspended', label: t('dashboard.suspended'), value: data.organizations.suspended, to: '/console/organizations' },
                  ]}
                  format={n.plain}
                  totalLabel={t('dashboard.organizationsShort')}
                  height={170}
                />
              </Card>
            ) : null}
          </div>

          {data.activity ? (
            <Card title={t('dashboard.platformActivity')} description={t('dashboard.platformActivityHint')} actions={<Link to="/console/activity" className="link-btn">{t('dashboard.openLog')}</Link>}>
              <TrendChart points={data.activity.points} label={t('dashboard.events')} format={n.compact} height={220} />
            </Card>
          ) : null}

          {data.organizations ? (
            <Card title={t('dashboard.newestOrganizations')} actions={<Link to="/console/organizations" className="link-btn">{t('dashboard.seeAll')}</Link>} flush>
              <div className="table-wrap">
                <table className="table">
                  <thead>
                    <tr>
                      <th>{t('dashboard.colOrganization')}</th>
                      <th className="end">{t('dashboard.members')}</th>
                      <th>{t('dashboard.colStatus')}</th>
                      <th>{t('dashboard.colJoined')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.organizations.recent.map((org) => (
                      <tr key={org.id}>
                        <td>
                          <span className="org-cell">
                            <span className="org-mark" aria-hidden="true">
                              {initials(org.name)}
                            </span>
                            <span className="member-meta">
                              <span className="member-name">{org.name}</span>
                              <span className="small muted mono">{org.code}</span>
                            </span>
                          </span>
                        </td>
                        <td className="end num">{n.plain(org.members)}</td>
                        <td>{org.active ? <Badge tone="ok">{t('dashboard.active')}</Badge> : <Badge tone="warn">{t('dashboard.suspended')}</Badge>}</td>
                        <td className="small muted nowrap">{relativeTimeIn(intlLocale(locale), org.created_at)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </Card>
          ) : null}
        </div>
      )}
    </>
  )
}

/* ── Organization ──────────────────────────────────────── */

function OrganizationHome({ session }: { session: Session }) {
  const t = useT()
  const locale = useLocale()
  const n = useNumbers()
  const [days, setDays] = usePeriod()
  const organization = useQuery(organizationQuery)
  const dashboard = useQuery({
    queryKey: ['dashboard', 'org', days, timeZone],
    queryFn: () => api<TenantDashboard>('/dashboard', { query: { days, tz: timeZone } }),
    placeholderData: keepPreviousData,
  })
  const billing = can(session, 'billing.view')
  const usage = useQuery({ queryKey: ['billing', 'usage'], queryFn: () => api<Usage>('/billing/usage'), enabled: billing })
  const subscription = useQuery({ ...subscriptionStatusQuery, enabled: billing })
  const activityAllowed = can(session, 'activity.view')
  const recent = useQuery({
    queryKey: ['activity', 'org', 'recent'],
    queryFn: () => api<{ data: ActivityEntry[] }>('/activity', { envelope: true, query: { per_page: 6 } }),
    enabled: activityAllowed,
  })
  const notifications = useQuery({ queryKey: [...feedKey, 'home'], queryFn: () => fetchFeed({ limit: 5 }) })
  const data = dashboard.data
  const name = session.user.first_name || session.user.name
  const orgName = organization.data?.name
  const lastSignIn = session.user.last_login_at
    ? new Intl.DateTimeFormat(intlLocale(locale), { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(session.user.last_login_at))
    : t('home.never')
  const hasFigures = !!data && (data.members || data.activity || data.files)

  const quick: QuickAction[] = [
    { to: '/org/members', icon: 'users', label: 'dashboard.qInvite', hint: 'dashboard.qInviteHint', show: can(session, 'user.create') },
    { to: '/org/files', icon: 'files', label: 'dashboard.qUpload', hint: 'dashboard.qUploadHint', show: can(session, 'file.create') },
    { to: '/org/branches', icon: 'tenants', label: 'dashboard.qBranches', hint: 'dashboard.qBranchesHint', show: can(session, 'branch.view.all') },
    { to: '/org/billing', icon: 'package', label: 'dashboard.qBilling', hint: 'dashboard.qBillingHint', show: billing },
    { to: '/profile', icon: 'user', label: 'dashboard.qProfile', hint: 'dashboard.qProfileHint', show: true },
    { to: '/settings/security', icon: 'shield', label: 'dashboard.qSecurity', hint: 'dashboard.qSecurityHint', show: true },
  ]

  return (
    <>
      <Hero
        title={t('home.greeting', { name })}
        subtitle={orgName ? t('dashboard.orgSubtitle', { org: orgName }) : t('home.subtitle')}
        actions={hasFigures ? <PeriodPicker days={days} onChange={setDays} /> : undefined}
      />
      {!data ? (
        <DashboardSkeleton />
      ) : (
        <div className={`dash ${dashboard.isPlaceholderData ? 'is-updating' : ''}`}>
          <GettingStarted members={data.members?.total} />
          <div className="dash-kpis">
            {data.members ? (
              <KpiCard
                label={t('dashboard.members')}
                value={n.plain(data.members.total)}
                delta={<Delta current={data.members.added.total} previous={data.members.added.previous_total} />}
                sub={t('dashboard.activeMembers', { count: n.plain(data.members.active) })}
                points={data.members.added.points}
                to="/org/members"
              />
            ) : null}
            {data.members ? (
              <KpiCard
                label={t('dashboard.pendingInvitations')}
                value={n.plain(data.members.pending_invitations)}
                sub={t('dashboard.signedIn', { count: n.plain(data.members.signed_in_recently) })}
                to="/org/members"
              />
            ) : null}
            {data.activity ? (
              <KpiCard
                label={t('dashboard.events')}
                value={n.plain(data.activity.events.total)}
                delta={<Delta current={data.activity.events.total} previous={data.activity.events.previous_total} />}
                sub={t('dashboard.inPeriod', { count: days })}
                points={data.activity.events.points}
                to="/org/activity"
              />
            ) : null}
            {data.files ? (
              <KpiCard
                label={t('dashboard.storage')}
                value={formatBytes(data.files.bytes)}
                sub={t('dashboard.files', { count: n.plain(data.files.files) })}
                to="/org/files"
              />
            ) : null}
            {!data.members && !data.activity && !data.files ? (
              <>
                <KpiCard label={t('home.lastSignIn')} value={<span className="kpi-text">{lastSignIn}</span>} />
                <KpiCard label={t('dashboard.unread')} value={n.plain(notifications.data?.meta.unread_count ?? 0)} to="/notifications" />
                <KpiCard label={t('home.roles')} value={<span className="kpi-text">{(session.user.roles ?? []).map((role) => role.name).join(', ') || t('role.member')}</span>} />
              </>
            ) : null}
          </div>

          {data.activity || billing ? (
            <div className="dash-row wide">
              {data.activity ? (
                <Card title={t('dashboard.activity')} description={t('dashboard.activityHint')} actions={<Link to="/org/activity" className="link-btn">{t('dashboard.openLog')}</Link>}>
                  <TrendChart points={data.activity.events.points} label={t('dashboard.events')} format={n.compact} />
                </Card>
              ) : null}
              {billing ? <PlanCard usage={usage.data} status={subscription.data} /> : null}
            </div>
          ) : null}

          {data.members || data.files || data.activity ? (
            <div className="dash-row">
              {data.members ? (
                <Card title={t('dashboard.membersByStatus')} actions={<Link to="/org/members" className="link-btn">{t('dashboard.seeAll')}</Link>}>
                  <DonutChart
                    slices={[
                      { key: 'active', label: t('dashboard.statusActive'), value: data.members.active, to: '/org/members' },
                      { key: 'invited', label: t('dashboard.statusInvited'), value: data.members.pending_invitations, to: '/org/members' },
                      {
                        key: 'inactive',
                        label: t('dashboard.statusInactive'),
                        value: Math.max(0, data.members.total - data.members.active - data.members.pending_invitations),
                        to: '/org/members',
                      },
                    ]}
                    format={n.plain}
                    totalLabel={t('dashboard.membersShort')}
                    height={170}
                  />
                </Card>
              ) : null}
              {data.files ? (
                <Card title={t('dashboard.storageByType')} actions={<Link to="/org/files" className="link-btn">{t('dashboard.seeAll')}</Link>}>
                  <DonutChart
                    slices={data.files.by_type.map((row) => ({ key: row.type, label: t(fileTypeLabels[row.type] ?? 'files.other'), value: row.bytes, to: '/org/files' }))}
                    format={formatBytes}
                    totalLabel={t('dashboard.files', { count: n.plain(data.files.files) })}
                    totalValue={formatBytes(data.files.bytes)}
                    empty={t('dashboard.noFiles')}
                    height={170}
                  />
                </Card>
              ) : null}
              {data.activity ? (
                <Card title={t('dashboard.topActors')} description={t('dashboard.inPeriod', { count: days })}>
                  <RankedBarChart
                    label={t('dashboard.topActors')}
                    items={data.activity.top_actors.map((row) => ({
                      key: String(row.id),
                      label: row.name,
                      value: row.count,
                      to: '/org/activity',
                      search: { who: row.id },
                    }))}
                    format={n.plain}
                    empty={t('dashboard.quiet')}
                  />
                </Card>
              ) : null}
            </div>
          ) : null}

          <div className="dash-row">
            {data.activity ? (
              <Card title={t('dashboard.topActions')} description={t('dashboard.inPeriod', { count: days })}>
                <RankedBarChart
                  label={t('dashboard.topActions')}
                  items={data.activity.top_actions.map((row) => ({
                    key: row.action,
                    label: actionLabel(row.action),
                    value: row.count,
                    to: '/org/activity',
                    search: { action: row.action },
                  }))}
                  format={n.plain}
                  empty={t('dashboard.quiet')}
                />
              </Card>
            ) : null}
            {activityAllowed ? <RecentActivity entries={recent.data?.data} /> : <RecentNotifications items={notifications.data?.data} />}
            <QuickActions actions={quick} />
          </div>
        </div>
      )}
    </>
  )
}

function RecentActivity({ entries }: { entries?: ActivityEntry[] }) {
  const t = useT()
  const locale = intlLocale(useLocale())
  return (
    <Card title={t('dashboard.recentActivity')} actions={<Link to="/org/activity" className="link-btn">{t('dashboard.seeAll')}</Link>} flush>
      {!entries ? (
        <div className="p-4 small muted">…</div>
      ) : entries.length === 0 ? (
        <EmptyState icon="activity" title={t('dashboard.quiet')} />
      ) : (
        <ul className="feed-list">
          {entries.map((entry) => (
            <li key={entry.id} className="feed-item">
              <span className="avatar xs" aria-hidden="true">
                {initials(entry.actor?.name ?? '·')}
              </span>
              <span className="feed-text">
                <span>
                  <span className="strong">{entry.actor?.name ?? t('dashboard.system')}</span> <span className="muted">·</span> {actionLabel(entry.action)}
                </span>
                <span className="small muted">
                  {entry.subject_id ? `${entry.subject_type} #${entry.subject_id} · ` : ''}
                  {relativeTimeIn(locale, entry.created_at)}
                </span>
              </span>
            </li>
          ))}
        </ul>
      )}
    </Card>
  )
}

function RecentNotifications({ items }: { items?: Schemas['FeedItem'][] }) {
  const t = useT()
  const locale = intlLocale(useLocale())
  return (
    <Card title={t('notifications.title')} actions={<Link to="/notifications" className="link-btn">{t('dashboard.seeAll')}</Link>} flush>
      {!items ? (
        <div className="p-4 small muted">…</div>
      ) : items.length === 0 ? (
        <EmptyState icon="bell" title={t('dashboard.noNotifications')} />
      ) : (
        <ul className="feed-list">
          {items.map((item) => (
            <li key={item.id} className={`feed-item ${item.is_read ? '' : 'unread'}`}>
              <span className="feed-dot" aria-hidden="true" />
              <span className="feed-text">
                <span className={item.is_read ? '' : 'strong'}>{item.title}</span>
                <span className="small muted">{relativeTimeIn(locale, item.created_at)}</span>
              </span>
            </li>
          ))}
        </ul>
      )}
    </Card>
  )
}

