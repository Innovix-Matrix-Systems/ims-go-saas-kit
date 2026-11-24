import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { ActivityLog, validateActivitySearch } from '../components/ActivityLog'

export const Route = createFileRoute('/_app/org/activity')({
  validateSearch: validateActivitySearch,
  component: OrganizationActivity,
})

function OrganizationActivity() {
  const search = Route.useSearch()
  const navigate = useNavigate({ from: '/org/activity' })
  return (
    <ActivityLog
      search={search}
      onSearch={(patch) => void navigate({ search: (prev) => (patch ? { ...prev, ...patch } : {}), replace: true })}
    />
  )
}
