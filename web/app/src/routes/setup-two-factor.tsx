import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { signOut } from '../auth/session'
import { StatusScreen } from '../components/StatusScreen'
import { Button } from '../components/ui'
import { useT } from '../i18n'

// The organization requires two-factor authentication and the sign-in only
// produced a setup-only token. The enrolment flow itself plugs in here.

export const Route = createFileRoute('/setup-two-factor')({
  component: SetupTwoFactorPage,
})

function SetupTwoFactorPage() {
  const t = useT()
  const navigate = useNavigate()
  return (
    <StatusScreen
      icon="shield"
      title={t('status.twoFactorTitle')}
      body={t('status.twoFactorBody')}
      actions={
        <Button variant="ghost" icon="logout" onClick={() => void signOut().then(() => navigate({ to: '/login' }))}>
          {t('common.signOut')}
        </Button>
      }
    />
  )
}
