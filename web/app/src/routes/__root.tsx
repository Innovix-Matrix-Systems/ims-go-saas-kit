import { Link, Outlet, createRootRoute, useNavigate, useRouterState } from '@tanstack/react-router'
import { useEffect } from 'react'
import { onApiEvent } from '../api/client'
import { forgetSession } from '../auth/session'
import { StatusScreen } from '../components/StatusScreen'
import { Icon } from '../components/ui'
import { useT } from '../i18n'

export const Route = createRootRoute({
  component: Root,
  notFoundComponent: NotFound,
})

/**
 * Listens for answers that change the whole app (session ended, 2FA setup
 * required, organization suspended, maintenance) and moves to the matching
 * screen, whatever page made the request.
 */
function Root() {
  const navigate = useNavigate()
  const pathname = useRouterState({ select: (state) => state.location.pathname })

  useEffect(
    () =>
      onApiEvent((event) => {
        switch (event.type) {
          case 'unauthenticated':
            forgetSession(true)
            if (pathname !== '/login') void navigate({ to: '/login' })
            return
          case 'two-factor-setup-required':
            void navigate({ to: '/setup-two-factor' })
            return
          case 'tenant-suspended':
            void navigate({ to: '/suspended' })
            return
          case 'maintenance':
            void navigate({ to: '/maintenance' })
            return
        }
      }),
    [navigate, pathname],
  )

  return <Outlet />
}

function NotFound() {
  const t = useT()
  return (
    <StatusScreen
      icon="search"
      title={t('status.notFoundTitle')}
      body={t('status.notFoundBody')}
      actions={
        <Link to="/" className="btn primary">
          <Icon name="home" /> {t('status.home')}
        </Link>
      }
    />
  )
}
