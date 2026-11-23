import { Link, createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { onboardingQuery } from './_app.onboarding'
import { useSession } from '../auth/session'
import { initials } from '../format'
import { intlLocale, useLocale, useT } from '../i18n'
import { Badge, Card, Grid, StatTile } from '../components/ui'

export const Route = createFileRoute('/_app/')({
  component: HomePage,
})

function HomePage() {
  const t = useT()
  const locale = useLocale()
  const session = useSession()
  const onboarding = useQuery(onboardingQuery)
  if (!session) return null
  const { user } = session
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
          <p>{t('home.subtitle')}</p>
        </div>
      </div>

      <Grid min="sm">
        <StatTile label={t('home.permissions')} value={new Intl.NumberFormat(intlLocale(locale)).format(session.permissions.size)} />
        <StatTile label={t('home.lastSignIn')} value={lastSignIn} />
        <StatTile label={t('home.branch')} value={user.active_branch_id ? `#${user.active_branch_id}` : '—'} />
      </Grid>

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
