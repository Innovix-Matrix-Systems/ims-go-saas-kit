import { api, type Schemas } from '../api/client'

/** The signed-in member's organization (or the one staff impersonate). */
export type Organization = Schemas['OrganizationEnvelope']['data']

export const organizationQuery = { queryKey: ['organization'], queryFn: () => api<Organization>('/organization'), staleTime: 60_000 }
