import { Link, createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { onboardingQuery } from './_app.onboarding'
import { api, type Schemas } from '../api/client'
import { can, useSession, type Session } from '../auth/session'
import { initials } from '../format'
import { intlLocale, useLocale, useT } from '../i18n'
import { Badge, Card, Grid, StatTile } from '../components/ui'

export const Route = createFileRoute('/_app/')({
  component: HomePage,
})

interface Counted {
  meta: Schemas['PaginationMeta']
}

/** Staff home: what needs attention on the platform, each tile a way in. */
function StaffTiles({ session, lastSignIn }: { session: Session; lastSignIn: string }) {
  const t = useT()
  const locale = useLocale()
  const number = (value?: number) => (value == null ? '—' : new Intl.NumberFormat(intlLocale(locale)).format(value))
  const count = (path: string, query: Record<string, string | number | boolean> = {}) => () =>
    api<Counted>(path, { asStaff: true, envelope: true, query: { per_page: 1, ...query } }).then((page) => page.meta.total)
  const organizations = useQuery({ queryKey: ['home', 'tenants'], queryFn: count('/tenant'), enabled: can(session, 'tenant.view.all') })
  const approvals = useQuery({
    queryKey: ['home', 'approvals'],
    queryFn: count('/billing/internal/payment-approvals', { status: 'pending' }),
    enabled: can(session, 'payment_approval.view.all'),
  })
  const users = useQuery({ queryKey: ['home', 'users'], queryFn: count('/user'), enabled: can(session, 'user.view.all') })
  return (
    <Grid min="sm">
      <StatTile label={t('console.organizations')} value={number(organizations.data)} to="/console/organizations" />
      <StatTile
        label={t('home.paymentsToReview')}
        value={number(approvals.data)}
        hint={approvals.data ? t('console.pending') : undefined}
        to="/console/approvals"
      />
      <StatTile label={t('console.users')} value={number(users.data)} to="/console/users" />
      <StatTile label={t('home.lastSignIn')} value={lastSignIn} />
    </Grid>
  )
}

function HomePage() {
  const t = useT()
  const locale = useLocale()
  const session = useSession()
  const onboarding = useQuery(onboardingQuery)
  if (!session) return null
  const { user } = session
  // Platform staff (not impersonating or logged in as someone) see the platform.
  const staff = session.isPlatformUser && !session.acting
  const lastSignIn = user.last_login_at
    ? new Intl.DateTimeFormat(intlLocale(locale), { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(user.last_login_at))
    : t('home.never')

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
      <div className="home-hero">
        <div className="avatar">{initials(user.name)}</div>
        <div>
          <h1>{t('home.greeting', { name: user.first_name || user.name })}</h1>
          <p>{staff ? t('home.staffSubtitle') : t('home.subtitle')}</p>
        </div>
      </div>

      {staff ? (
        <StaffTiles session={session} lastSignIn={lastSignIn} />
      ) : (
        <Grid min="sm">
          <StatTile label={t('home.permissions')} value={new Intl.NumberFormat(intlLocale(locale)).format(session.permissions.size)} />
          <StatTile label={t('home.lastSignIn')} value={lastSignIn} />
          <StatTile label={t('home.branch')} value={user.active_branch_id ? `#${user.active_branch_id}` : '—'} />
        </Grid>
      )}

      <div className="mt-16">
        <Card title={t('home.roles')}>
          <div className="home-roles">
            {(user.roles ?? []).length > 0 ? (
              user.roles!.map((role) => (
                <Badge key={role.id} tone={role.is_system ? 'info' : 'neutral'}>
                  {role.name}
                </Badge>
              ))
            ) : (
              <span className="muted">{session.isPlatformUser ? t('role.superAdmin') : t('role.member')}</span>
            )}
          </div>
        </Card>
      </div>
    </>
  )
}
