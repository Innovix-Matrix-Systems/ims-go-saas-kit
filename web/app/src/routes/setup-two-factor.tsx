import { createFileRoute, redirect, useNavigate } from '@tanstack/react-router'
import { setToken } from '../api/client'
import { getSetupOnlySession, signOut } from '../auth/session'
import { AuthCard } from '../components/form'
import { TwoFactorSetup } from '../components/TwoFactorSetup'
import { Button } from '../components/ui'
import { useT } from '../i18n'
import { queryClient } from '../query'

// The organization requires two-factor authentication and the sign-in only
// produced a setup-only token: the user turns it on here, and the server
// answers with a full session token.

export const Route = createFileRoute('/setup-two-factor')({
  beforeLoad: () => {
    if (!getSetupOnlySession()) throw redirect({ to: '/login' })
  },
  component: SetupTwoFactorPage,
})

function SetupTwoFactorPage() {
  const t = useT()
  const navigate = useNavigate()
  const setup = getSetupOnlySession()

  return (
    <AuthCard
      title={t('twoFactor.setupTitle')}
      subtitle={t('twoFactor.setupSubtitle')}
      footer={
        <Button variant="ghost" icon="logout" onClick={() => void signOut().then(() => navigate({ to: '/login' }))}>
          {t('common.signOut')}
        </Button>
      }
    >
      <TwoFactorSetup
        token={setup?.token}
        onDone={(result) => {
          if (result.token) {
            setToken(result.token, setup?.remember ?? false)
            queryClient.clear()
            void navigate({ to: '/' })
          } else {
            void navigate({ to: '/login' })
          }
        }}
      />
    </AuthCard>
  )
}
