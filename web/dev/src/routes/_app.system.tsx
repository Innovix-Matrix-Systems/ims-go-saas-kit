import { createFileRoute, redirect } from '@tanstack/react-router'

// System health lives in Inspector → System; keep old links working.
export const Route = createFileRoute('/_app/system')({
  beforeLoad: () => {
    throw redirect({ to: '/inspector', search: { tab: 'system' } })
  },
})
