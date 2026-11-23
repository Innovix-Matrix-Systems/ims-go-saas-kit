import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useDeferredValue, useState } from 'react'
import { api } from '../api/client'
import type { DevTenant, DevTenantDetail, Page } from '../api/types'
import { Select } from '../components/inputs'
import {
  Button,
  Card,
  Badge,
  Drawer,
  EmptyState,
  ErrorBox,
  LoadingBlock,
  PageHeader,
  StatTile,
  type BadgeTone,
} from '../components/ui'
import { formatBytes, formatDateTime, relativeDate } from '../format'

export const Route = createFileRoute('/_app/tenants')({
  validateSearch: (search: Record<string, unknown>): { open?: number } => ({
    open: typeof search.open === 'number' ? search.open : undefined,
  }),
  component: TenantsPage,
})

const perPage = 25

function subscriptionTone(status?: string | null): BadgeTone {
  switch (status) {
    case 'active':
      return 'ok'
    case 'trial':
      return 'info'
    case 'pending':
    case 'past_due':
      return 'warn'
    case 'cancelled':
    case 'expired':
      return 'danger'
    default:
      return 'neutral'
  }
}

const label = (status?: string | null) => (status ? status.replace('_', ' ') : 'none')

function TenantsPage() {
  const { open } = Route.useSearch()
  const navigate = useNavigate({ from: '/tenants' })
  const [search, setSearch] = useState('')
  const [active, setActive] = useState('')
  const [subscription, setSubscription] = useState('')
  const [page, setPage] = useState(1)
  const deferred = useDeferredValue(search.trim())

  const tenants = useQuery({
    queryKey: ['tenants', deferred, active, subscription, page],
    queryFn: ({ signal }) =>
      api<Page<DevTenant>>('/dev/api/tenants', {
        query: { search: deferred, active, subscription, page, per_page: perPage },
        signal,
        unwrap: false,
      }),
    placeholderData: keepPreviousData,
  })

  const openTenant = (id: number | undefined) => void navigate({ search: id ? { open: id } : {} })
  const rows = tenants.data?.data ?? []
  const meta = tenants.data?.meta

  return (
    <>
      <PageHeader
        title="Tenants"
        description="Every tenant: subscription, plan features, usage against limits, setting overrides."
      />
      <div className="filters">
        <label className="field grow">
          <span className="field-label">Search</span>
          <input
            className="input"
            placeholder="Name, code, email, phone or domain"
            value={search}
            onChange={(event) => {
              setSearch(event.target.value)
              setPage(1)
            }}
          />
        </label>
        <label className="field minw-field">
          <span className="field-label">Tenant</span>
          <Select
            value={active}
            onChange={(value) => {
              setActive(value)
              setPage(1)
            }}
            options={[
              { value: '', label: 'All' },
              { value: 'active', label: 'Active' },
              { value: 'inactive', label: 'Inactive' },
            ]}
          />
        </label>
        <label className="field minw-field">
          <span className="field-label">Subscription</span>
          <Select
            value={subscription}
            onChange={(value) => {
              setSubscription(value)
              setPage(1)
            }}
            options={[
              { value: '', label: 'Any' },
              { value: 'active', label: 'Active' },
              { value: 'trial', label: 'Trial' },
              { value: 'pending', label: 'Pending' },
              { value: 'past_due', label: 'Past due' },
              { value: 'cancelled', label: 'Cancelled' },
              { value: 'expired', label: 'Expired' },
              { value: 'none', label: 'No subscription' },
            ]}
          />
        </label>
      </div>

      <Card tight>
        {tenants.isLoading ? (
          <LoadingBlock label="Loading tenants…" />
        ) : tenants.isError ? (
          <ErrorBox message={tenants.error instanceof Error ? tenants.error.message : 'Unable to load tenants.'} />
        ) : rows.length === 0 ? (
          <EmptyState title="No tenants match" description="Try another search or filter." icon="sessions" />
        ) : (
          <div className="table-wrap flat">
            <table className="table">
              <thead>
                <tr>
                  <th>Tenant</th>
                  <th>Subscription</th>
                  <th>Expires</th>
                  <th className="num">Users</th>
                  <th className="num">Branches</th>
                  <th className="num">Overrides</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((tenant) => (
                  <tr key={tenant.id} className="clickable" onClick={() => openTenant(tenant.id)}>
                    <td>
                      <div className="strong">
                        {tenant.name} {!tenant.is_active ? <Badge tone="danger">inactive</Badge> : null}
                      </div>
                      <div className="small dim">{tenant.domain || tenant.code}</div>
                    </td>
                    <td>
                      <Badge tone={subscriptionTone(tenant.subscription_status)}>{label(tenant.subscription_status)}</Badge>
                      <div className="small dim">{tenant.package_name ?? '—'}</div>
                    </td>
                    <td className="small dim" title={tenant.expires_at ? formatDateTime(tenant.expires_at) : undefined}>
                      {tenant.expires_at ? relativeDate(tenant.expires_at) : '—'}
                    </td>
                    <td className="num">{tenant.users}</td>
                    <td className="num">{tenant.branches}</td>
                    <td className="num">{tenant.overrides}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
      {meta && meta.last_page > 1 ? (
        <div className="flex mt-16 justify-end g-2">
          <Button size="sm" variant="ghost" disabled={page <= 1} onClick={() => setPage(page - 1)}>
            Previous
          </Button>
          <span className="small dim">
            Page {meta.current_page} of {meta.last_page} · {meta.total} tenants
          </span>
          <Button size="sm" variant="ghost" disabled={page >= meta.last_page} onClick={() => setPage(page + 1)}>
            Next
          </Button>
        </div>
      ) : null}

      <TenantDrawer id={open ?? null} onClose={() => openTenant(undefined)} />
    </>
  )
}

function TenantDrawer({ id, onClose }: { id: number | null; onClose: () => void }) {
  const navigate = useNavigate()
  const detail = useQuery({
    queryKey: ['tenants', 'detail', id],
    queryFn: ({ signal }) => api<DevTenantDetail>(`/dev/api/tenants/${id}`, { signal }),
    enabled: id !== null,
  })
  if (id === null) return null
  const tenant = detail.data
  return (
    <Drawer
      open
      title={tenant?.name ?? 'Tenant'}
      subtitle={tenant ? [tenant.domain ?? tenant.code, tenant.email, tenant.phone].filter(Boolean).join(' · ') : undefined}
      onClose={onClose}
    >
      {detail.isLoading || !tenant ? (
        detail.isError ? (
          <ErrorBox message={detail.error instanceof Error ? detail.error.message : 'Unable to load the tenant.'} />
        ) : (
          <LoadingBlock label="Loading tenant…" />
        )
      ) : (
        <div className="stack">
          <div className="grid cols-4">
            <StatTile label="Users" value={tenant.users} />
            <StatTile label="Branches" value={tenant.branches} />
            <StatTile label="Overrides" value={tenant.overrides} />
            <StatTile label="Storage" value={formatBytes(tenant.storage_bytes)} />
          </div>
          <div className="flex wrap g-2">
            <Button
              size="sm"
              icon="settings"
              onClick={() => void navigate({ to: '/settings', search: { tenant: tenant.id } })}
            >
              Open setting overrides
            </Button>
            <Button size="sm" icon="flags" onClick={() => void navigate({ to: '/flags', search: { tenant: tenant.id } })}>
              Open feature flags
            </Button>
          </div>

          <div>
            <div className="section-label">Plan features</div>
            <div className="feature-list">
              {tenant.features.map((feature) => (
                <div key={feature.key} className="feature-row">
                  <div className="min-0">
                    <div className="mono small">
                      {feature.key} {feature.overridden ? <Badge tone="violet">override</Badge> : null}
                    </div>
                    <div className="small dim">{feature.description}</div>
                  </div>
                  <FeatureValue feature={feature} />
                </div>
              ))}
            </div>
          </div>

          <div>
            <div className="section-label">Subscriptions</div>
            {tenant.subscriptions.length === 0 ? (
              <div className="small dim">This tenant has never subscribed.</div>
            ) : (
              <div className="feature-list">
                {tenant.subscriptions.map((sub) => (
                  <div key={sub.id} className="feature-row">
                    <div className="flex g-3">
                      <Badge tone={subscriptionTone(sub.status)}>{label(sub.status)}</Badge>
                      <b>{sub.package_name}</b>
                    </div>
                    <span className="small dim">
                      {sub.starts_at ? formatDateTime(sub.starts_at) : formatDateTime(sub.created_at)}
                      {' → '}
                      {sub.expires_at ? formatDateTime(sub.expires_at) : sub.trial_ends_at ? formatDateTime(sub.trial_ends_at) : 'open'}
                    </span>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}
    </Drawer>
  )
}

function FeatureValue({ feature }: { feature: DevTenantDetail['features'][number] }) {
  if (!feature.is_limit) {
    return <Badge tone={feature.enabled ? 'ok' : 'neutral'}>{feature.enabled ? 'on' : 'off'}</Badge>
  }
  if (!feature.enabled) return <Badge>off</Badge>
  const used = feature.used ?? 0
  const limit = feature.limit
  const over = limit !== null && used >= limit
  const unit = feature.key === 'storage.mb' ? ' MB' : ''
  return (
    <div className="usage">
      <span className={`small ${over ? 'red' : ''}`}>
        {used}
        {unit} / {limit === null ? 'unlimited' : `${limit}${unit}`}
      </span>
      {limit !== null && limit > 0 ? (
        <div className="usage-bar">
          <span style={{ width: `${Math.min(100, (used / limit) * 100)}%` }} className={over ? 'over' : ''} />
        </div>
      ) : null}
    </div>
  )
}
