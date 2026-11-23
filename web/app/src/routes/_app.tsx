import { createFileRoute, redirect, type ErrorComponentProps } from '@tanstack/react-router'
import { ApiError, getActing, setActing } from '../api/client'
import { forgetSession, isSignedIn, sessionQuery } from '../auth/session'
import { AppShell } from '../components/AppShell'
import { StatusScreen } from '../components/StatusScreen'
import { Button } from '../components/ui'
import { useT } from '../i18n'
import { queryClient } from '../query'

export const Route = createFileRoute('/_app')({
  beforeLoad: ({ location }) => {
    if (!isSignedIn()) {
      throw redirect({ to: '/login', search: { next: location.pathname === '/' ? undefined : location.pathname } })
    }
  },
  // The profile and permissions load before any page renders, so pages can
  // rely on useSession() having data. A rejected token (revoked, expired) goes
  // back to sign-in here: this runs before the app-wide listener mounts.
  loader: async () => {
    try {
      return await queryClient.ensureQueryData(sessionQuery)
    } catch (error) {
      if (error instanceof ApiError && error.status === 401) {
        forgetSession(true)
        throw redirect({ to: '/login' })
      }
      if (error instanceof ApiError && error.code === 'TWO_FACTOR_SETUP_REQUIRED') throw redirect({ to: '/setup-two-factor' })
      if (error instanceof ApiError && error.code === 'TENANT_SUSPENDED') throw redirect({ to: '/suspended' })
      if (error instanceof ApiError && error.code === 'MAINTENANCE_MODE') throw redirect({ to: '/maintenance' })
      // Acting as someone who can no longer be acted as (deactivated, gone):
      // drop back to the staff account instead of failing every page.
      if (getActing()) {
        setActing(null)
        queryClient.clear()
        return await queryClient.ensureQueryData(sessionQuery)
      }
      throw error
    }
  },
  component: AppShell,
  errorComponent: AppError,
})

function AppError({ error, reset }: ErrorComponentProps) {
  const t = useT()
  const reference = error instanceof ApiError && error.traceId ? t('status.reference', { id: error.traceId }) : undefined
  return (
    <StatusScreen
      icon="alert"
      tone="danger"
      title={t('status.errorTitle')}
      body={t('status.errorBody')}
      reference={reference}
      actions={
        <>
          <a href="/" className="btn ghost">
            {t('status.home')}
          </a>
          <Button variant="primary" icon="refresh" onClick={reset}>
            {t('common.retry')}
          </Button>
        </>
      }
    />
  )
}
