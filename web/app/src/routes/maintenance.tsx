import { createFileRoute } from '@tanstack/react-router'
import { StatusScreen } from '../components/StatusScreen'
import { useT } from '../i18n'

export const Route = createFileRoute('/maintenance')({
  component: MaintenancePage,
})

function MaintenancePage() {
  const t = useT()
  return (
    <StatusScreen
      icon="clock"
      tone="warn"
      title={t('status.maintenanceTitle')}
      body={t('status.maintenanceBody')}
      actions={
        <a href="/" className="btn primary">
          {t('common.retry')}
        </a>
      }
    />
  )
}
