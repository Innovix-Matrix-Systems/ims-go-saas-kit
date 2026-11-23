import { Link, createFileRoute } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { api, type Schemas } from '../api/client'
import { useSession } from '../auth/session'
import { localeLabel, intlLocale, useLocale, useT } from '../i18n'
import { relativeTimeIn } from '../format'
import { organizationQuery } from '../components/organization'
import { usePhotoUpload } from '../components/PhotoUpload'
import { inOrganization, OrgMark } from '../components/WorkspaceMenu'
import { Avatar, Badge, Card, CopyField, Icon, Spinner } from '../components/ui'
import { FormSkeleton } from '../components/Skeleton'

export const Route = createFileRoute('/_app/profile')({
  component: ProfilePage,
})

type TwoFactorStatus = Schemas['TwoFactorStatus']
type DeviceSession = Schemas['Session']

/** The signed-in person's profile: who they are, where they belong, how their account is protected. */
function ProfilePage() {
  const t = useT()
  const locale = intlLocale(useLocale())
  const session = useSession()
  const photo = usePhotoUpload()
  const inside = inOrganization(session)
  const organization = useQuery({ ...organizationQuery, enabled: inside })
  const twoFactor = useQuery({ queryKey: ['two-factor'], queryFn: () => api<TwoFactorStatus>('/two-factor') })
  const sessions = useQuery({ queryKey: ['sessions'], queryFn: () => api<DeviceSession[]>('/sessions') })
  if (!session) return <FormSkeleton fields={4} />
  const user = session.user
  const date = (value: string) => new Intl.DateTimeFormat(locale, { dateStyle: 'long' }).format(new Date(value))
  const fullName = [user.first_name, user.last_name].filter(Boolean).join(' ')
  const roles = user.roles ?? []
  const current = sessions.data?.find((item) => item.current)

  return (
    <div className="profile">
      <section className="profile-hero card">
        <div className="profile-cover" aria-hidden="true" />
        <div className="profile-hero-body">
          <div className="profile-photo">
            <Avatar name={user.name} photo={user.photo} size="xl" />
            {photo.input}
            <button
              type="button"
              className="profile-photo-edit"
              title={t('profilePage.changePhoto')}
              aria-label={t('profilePage.changePhoto')}
              disabled={photo.uploading}
              onClick={photo.choose}
            >
              {photo.uploading ? <Spinner /> : <Icon name="edit" />}
            </button>
          </div>
          <div className="profile-identity">
            <h1>{user.name}</h1>
            <div className="profile-line">
              <Icon name="mail" />
              <span>{user.email}</span>
              {user.email_verified_at ? <Badge tone="ok">{t('profilePage.verified')}</Badge> : <Badge tone="warn">{t('profilePage.unverified')}</Badge>}
            </div>
            <div className="profile-badges">
              {session.isPlatformUser && !session.acting ? <Badge tone="violet">{t('role.superAdmin')}</Badge> : null}
              {roles.map((role) => (
                <Badge key={role.id} tone={role.is_system ? 'info' : 'neutral'}>
                  {role.name}
                </Badge>
              ))}
            </div>
          </div>
          <div className="profile-actions">
            <Link to="/settings/profile" className="btn">
              <Icon name="edit" />
              {t('profilePage.edit')}
            </Link>
          </div>
        </div>
      </section>

      <div className="profile-grid">
        <Card title={t('profilePage.about')}>
          <dl className="details">
            <dt>{t('profilePage.fullName')}</dt>
            <dd>{fullName || <span className="muted">{t('profilePage.notSet')}</span>}</dd>
            <dt>{t('profile.displayName')}</dt>
            <dd>{user.name}</dd>
            <dt>{t('profilePage.email')}</dt>
            <dd>
              <a href={`mailto:${user.email}`}>{user.email}</a>
            </dd>
            <dt>{t('profilePage.phone')}</dt>
            <dd>{user.phone ? <a href={`tel:${user.phone}`}>{user.phone}</a> : <span className="muted">{t('profilePage.notSet')}</span>}</dd>
            <dt>{t('profile.language')}</dt>
            <dd>{user.locale ? localeLabel(user.locale) : t('profile.languageDefault')}</dd>
            <dt>{t('profilePage.memberSince')}</dt>
            <dd>{date(user.created_at)}</dd>
            <dt>{t('home.lastSignIn')}</dt>
            <dd>{user.last_login_at ? relativeTimeIn(locale, user.last_login_at) : t('home.never')}</dd>
            <dt>{t('profilePage.userId')}</dt>
            <dd>
              <CopyField value={String(user.id)} />
            </dd>
          </dl>
        </Card>

        <div className="stack g-4">
          {inside ? (
            <Card title={t('profilePage.organization')} actions={<Link to="/org" className="link-btn">{t('profilePage.view')}</Link>}>
              {organization.data ? (
                <div className="profile-org">
                  <OrgMark name={organization.data.name} logo={organization.data.branding.logo_url} size="lg" />
                  <div className="member-meta">
                    <span className="member-name">{organization.data.name}</span>
                    <span className="small muted">
                      {organization.data.is_owner ? t('org.ownerYou') : roles.map((role) => role.name).join(', ') || t('role.member')}
                    </span>
                  </div>
                </div>
              ) : (
                <Spinner />
              )}
            </Card>
          ) : null}

          <Card title={t('settings.security')} actions={<Link to="/settings/security" className="link-btn">{t('profilePage.manage')}</Link>}>
            <ul className="check-list">
              <li>
                <span className={`check-dot ${twoFactor.data?.enabled ? 'ok' : 'warn'}`} aria-hidden="true">
                  <Icon name={twoFactor.data?.enabled ? 'check' : 'alert'} />
                </span>
                <span className="check-text">
                  <span>{t('profilePage.twoFactor')}</span>
                  <span className="small muted">
                    {!twoFactor.data
                      ? '…'
                      : twoFactor.data.enabled
                        ? twoFactor.data.method === 'email'
                          ? t('profilePage.twoFactorEmail')
                          : t('profilePage.twoFactorApp')
                        : t('profilePage.twoFactorOff')}
                  </span>
                </span>
              </li>
              <li>
                <span className={`check-dot ${user.email_verified_at ? 'ok' : 'warn'}`} aria-hidden="true">
                  <Icon name={user.email_verified_at ? 'check' : 'alert'} />
                </span>
                <span className="check-text">
                  <span>{t('profilePage.emailCheck')}</span>
                  <span className="small muted">{user.email_verified_at ? t('profilePage.verified') : t('profilePage.unverified')}</span>
                </span>
              </li>
              <li>
                <span className="check-dot neutral" aria-hidden="true">
                  <Icon name="user" />
                </span>
                <span className="check-text">
                  <span>{t('profilePage.devices', { count: sessions.data?.length ?? 0 })}</span>
                  <span className="small muted">{current ? t('profilePage.thisDevice', { device: current.browser || current.device }) : '…'}</span>
                </span>
              </li>
            </ul>
          </Card>
        </div>
      </div>
    </div>
  )
}
