import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { signOut } from '../auth/session'
import { StatusScreen } from '../components/StatusScreen'
import { Button } from '../components/ui'
import { useT } from '../i18n'

export const Route = createFileRoute('/suspended')({
  component: SuspendedPage,
})

function SuspendedPage() {
  const t = useT()
  const navigate = useNavigate()
  return (
    <StatusScreen
      icon="alert"
      tone="warn"
      title={t('status.suspendedTitle')}
      body={t('status.suspendedBody')}
      actions={
        <Button variant="primary" icon="logout" onClick={() => void signOut().then(() => navigate({ to: '/login' }))}>
          {t('common.signOut')}
        </Button>
      }
    />
  )
}
