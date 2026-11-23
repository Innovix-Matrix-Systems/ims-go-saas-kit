import { useNavigate } from '@tanstack/react-router'
import { api, getActing, type Schemas, type User } from '../api/client'
import { changeActing } from '../auth/session'

export type Tenant = Schemas['Tenant']

/** Every organization, for names in console lists (staff only). */
export const allTenantsQuery = {
  queryKey: ['console', 'tenants', 'all'],
  queryFn: () => api<Tenant[]>('/tenant', { asStaff: true, query: { per_page: 100 } }),
  staleTime: 60_000,
}

/** Starts impersonating an organization or logging in as a user, then opens the app's home. */
export function useStartActing() {
  const navigate = useNavigate()
  // A refusal (the target is staff, inactive, or the caller may not act)
  // puts the staff account back and is thrown for the caller to show.
  const start = async (acting: Parameters<typeof changeActing>[0]) => {
    try {
      await changeActing(acting)
    } catch (cause) {
      await changeActing(null)
      throw cause
    }
    await navigate({ to: '/' })
  }
  return {
    impersonate: (tenant: Pick<Tenant, 'id' | 'name'>) => start({ kind: 'tenant', tenantId: tenant.id, name: tenant.name }),
    loginAs: (user: Pick<User, 'id' | 'name' | 'email'>, tenantName?: string) =>
      start({ kind: 'user', userId: user.id, name: user.name, email: user.email, tenantName }),
  }
}

/** Ends impersonation or logging in as someone, back to where it started. */
export function useStopActing() {
  const navigate = useNavigate()
  return async () => {
    const acting = getActing()
    await changeActing(null)
    await navigate({ to: acting?.kind === 'user' ? '/console/users' : '/console/organizations' })
  }
}
