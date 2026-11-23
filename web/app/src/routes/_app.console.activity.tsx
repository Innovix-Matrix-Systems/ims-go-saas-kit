import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { ActivityLog, validateActivitySearch } from '../components/ActivityLog'

export const Route = createFileRoute('/_app/console/activity')({
  validateSearch: validateActivitySearch,
  component: PlatformActivity,
})

/** Every organization's activity and the system log (staff with activity.view.all). */
function PlatformActivity() {
  const search = Route.useSearch()
  const navigate = useNavigate({ from: '/console/activity' })
  return (
    <ActivityLog
      platform
      search={search}
      onSearch={(patch) => void navigate({ search: (prev) => (patch ? { ...prev, ...patch } : {}), replace: true })}
    />
  )
}
