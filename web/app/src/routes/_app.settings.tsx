import { Link, Outlet, createFileRoute, redirect } from '@tanstack/react-router'
import { PageHeader } from '../components/ui'
import { useT, type MessageKey } from '../i18n'

const tabs: { to: string; label: MessageKey }[] = [
  { to: '/settings/profile', label: 'settings.profile' },
  { to: '/settings/security', label: 'settings.security' },
  { to: '/settings/notifications', label: 'settings.notifications' },
  { to: '/settings/connections', label: 'connections.tab' },
  { to: '/settings/privacy', label: 'settings.privacy' },
]

export const Route = createFileRoute('/_app/settings')({
  beforeLoad: ({ location }) => {
    if (location.pathname === '/settings' || location.pathname === '/settings/') throw redirect({ to: '/settings/profile' })
  },
  component: SettingsLayout,
})

function SettingsLayout() {
  const t = useT()
  return (
    <>
      <PageHeader title={t('settings.title')} />
      <nav className="tabs" aria-label={t('settings.title')}>
        {tabs.map((tab) => (
          <Link key={tab.to} to={tab.to} className="tab" activeProps={{ className: 'tab active' }}>
            {t(tab.label)}
          </Link>
        ))}
      </nav>
      <div className="settings-body">
        <Outlet />
      </div>
    </>
  )
}
