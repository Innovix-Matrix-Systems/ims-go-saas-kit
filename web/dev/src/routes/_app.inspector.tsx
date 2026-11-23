import { useQuery } from '@tanstack/react-query'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useMemo, useState, type ReactNode } from 'react'
import { api } from '../api/client'
import type { DevAccessReport, DevConfigSection, DevPackagesReport, DevRouteReport } from '../api/types'
import { Select } from '../components/inputs'
import { SystemTab } from '../components/SystemTab'
import {
  Card,
  CardHeader,
  Badge,
  CopyButton,
  EmptyState,
  ErrorBox,
  Icon,
  LoadingBlock,
  PageHeader,
  StatTile,
  Tabs,
} from '../components/ui'

type InspectorTab = 'system' | 'routes' | 'access' | 'config' | 'packages'
const tabIds: InspectorTab[] = ['system', 'routes', 'access', 'config', 'packages']

export const Route = createFileRoute('/_app/inspector')({
  validateSearch: (search: Record<string, unknown>): { tab: InspectorTab; q?: string } => ({
    tab: tabIds.includes(search.tab as InspectorTab) ? (search.tab as InspectorTab) : 'system',
    q: typeof search.q === 'string' && search.q ? search.q : undefined,
  }),
  component: InspectorPage,
})

const placeholders: Record<InspectorTab, string> = {
  system: 'Search health checks',
  routes: 'Search path, handler, permission or middleware',
  access: 'Search roles and permissions',
  config: 'Search config keys and values',
  packages: 'Search packages',
}

function matches(needle: string, ...values: (string | undefined | null)[]): boolean {
  if (!needle) return true
  return values.some((value) => value?.toLowerCase().includes(needle))
}

function InspectorPage() {
  const { tab, q } = Route.useSearch()
  const navigate = useNavigate({ from: '/inspector' })
  const [search, setSearch] = useState(q ?? '')
  const needle = search.trim().toLowerCase()

  const setTab = (next: InspectorTab) => {
    setSearch('')
    void navigate({ search: { tab: next } })
  }

  return (
    <>
      <PageHeader
        title="Inspector"
        description="System health, routes, roles and permissions, configuration (secrets masked) and installed packages."
      />
      <div className="inspector-bar">
        <Tabs
          tabs={[
            { id: 'system', label: 'System' },
            { id: 'routes', label: 'Routes' },
            { id: 'access', label: 'Roles & permissions' },
            { id: 'config', label: 'Config' },
            { id: 'packages', label: 'Packages' },
          ]}
          active={tab}
          onChange={setTab}
        />
        <label className="inspector-search">
          <Icon name="search" />
          <input
            className="input"
            placeholder={placeholders[tab]}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
        </label>
      </div>
      {tab === 'system' ? <SystemTab needle={needle} /> : null}
      {tab === 'routes' ? <RoutesTab needle={needle} /> : null}
      {tab === 'access' ? <AccessTab needle={needle} /> : null}
      {tab === 'config' ? <ConfigTab needle={needle} /> : null}
      {tab === 'packages' ? <PackagesTab needle={needle} /> : null}
    </>
  )
}

function Loadable<T>({ query, children }: { query: { isLoading: boolean; error: unknown; data?: T }; children: (data: T) => ReactNode }) {
  if (query.isLoading) return <LoadingBlock label="Inspecting…" />
  if (query.error || !query.data) {
    return <ErrorBox message={query.error instanceof Error ? query.error.message : 'Unable to load.'} />
  }
  return <>{children(query.data)}</>
}

// ── Routes ───────────────────────────────────────────────────────────────

const methodFilters = ['', 'GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'ANY']

function RoutesTab({ needle }: { needle: string }) {
  const [method, setMethod] = useState('')
  const [group, setGroup] = useState('')
  const query = useQuery({
    queryKey: ['inspector', 'routes'],
    queryFn: ({ signal }) => api<DevRouteReport>('/dev/api/inspector/routes', { signal }),
    staleTime: 60_000,
  })
  return (
    <Loadable query={query}>
      {(report) => {
        const groups = [...new Set(report.routes.map((route) => routeGroup(route.path)))].sort()
        const rows = report.routes.filter(
          (route) =>
            (!method || route.method === method) &&
            (!group || routeGroup(route.path) === group) &&
            matches(needle, route.path, route.handler, route.method, ...route.middleware),
        )
        const unprotected = report.routes.filter(
          (route) => route.path.startsWith('/api/v1/') && route.middleware.includes('auth') && route.permissions.length === 0,
        ).length
        return (
          <>
            <div className="grid cols-4">
              <StatTile label="Routes" value={report.routes.length} hint="registered on the router" />
              <StatTile
                label="With a permission"
                value={report.routes.filter((route) => route.permissions.length > 0).length}
                hint="can: middleware on the route"
              />
              <StatTile label="Authenticated, no can:" value={unprotected} hint="checked inside the handler or open to any user" />
              <StatTile label="Global middleware" value={report.global.length} hint="runs on every request" />
            </div>
            <Card className="mt-16" tight>
              <div className="inspector-global">
                <span className="field-label">Every request</span>
                {report.global.map((name, index) => (
                  <span key={`${name}-${index}`} className="mw-chip">
                    {name}
                  </span>
                ))}
              </div>
            </Card>
            <Card className="mt-16" tight>
              <CardHeader
                title={`${rows.length} of ${report.routes.length} routes`}
                actions={
                  <div className="flex g-2">
                    <Select
                      size="sm"
                      value={group}
                      onChange={setGroup}
                      ariaLabel="Route group"
                      options={[{ value: '', label: 'All groups' }, ...groups.map((value) => ({ value, label: value }))]}
                    />
                    <Select
                      size="sm"
                      value={method}
                      onChange={setMethod}
                      ariaLabel="Method"
                      options={methodFilters.map((value) => ({ value, label: value || 'All methods' }))}
                    />
                  </div>
                }
              />
              {rows.length === 0 ? (
                <EmptyState title="No routes match" description="Try another search or filter." icon="route" />
              ) : (
                <div className="table-wrap flat">
                  <table className="table">
                    <thead>
                      <tr>
                        <th className="w-xs">Method</th>
                        <th>URI</th>
                        <th>Permission</th>
                        <th>Action</th>
                      </tr>
                    </thead>
                    <tbody>
                      {rows.map((route) => (
                        <tr key={`${route.method} ${route.path}`}>
                          <td>
                            <span className={`method-pill method ${route.method}`}>{route.method}</span>
                          </td>
                          <td>
                            <div className="mono route-path">{route.path}</div>
                            <div className="route-mw">
                              {route.middleware
                                .filter((name) => !name.startsWith('can:'))
                                .map((name, index) => (
                                  <span key={`${name}-${index}`}>{name}</span>
                                ))}
                            </div>
                          </td>
                          <td>
                            {route.permissions.length > 0 ? (
                              <div className="flex wrap g-1">
                                {route.permissions.map((permission) => (
                                  <span key={permission} className="code-inline">
                                    {permission}
                                  </span>
                                ))}
                              </div>
                            ) : (
                              <span className="small dim">—</span>
                            )}
                          </td>
                          <td className="mono small dim">{route.handler}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </Card>
          </>
        )
      }}
    </Loadable>
  )
}

function routeGroup(path: string): string {
  const parts = path.split('/').filter(Boolean)
  if (parts[0] === 'api' && parts[1] === 'v1') return parts[2] === 'internal' ? `internal/${parts[3] ?? ''}` : parts[2] ?? 'api'
  if (parts[0] === 'dev' && parts[1] === 'api') return `dev/${parts[2] ?? ''}`
  return parts[0] ?? '/'
}

// ── Roles & permissions ──────────────────────────────────────────────────

function AccessTab({ needle }: { needle: string }) {
  const query = useQuery({
    queryKey: ['inspector', 'access'],
    queryFn: ({ signal }) => api<DevAccessReport>('/dev/api/inspector/access', { signal }),
  })
  return (
    <Loadable query={query}>
      {(report) => {
        const groups = new Map<string, DevAccessReport['permissions']>()
        for (const permission of report.permissions) {
          if (!matches(needle, permission.name, permission.group, permission.description)) continue
          const list = groups.get(permission.group || 'other') ?? []
          list.push(permission)
          groups.set(permission.group || 'other', list)
        }
        const roles = report.roles.filter((role) => matches(needle, role.name))
        return (
          <>
            <div className="grid cols-3">
              <DriftCard
                label="Declared, not in database"
                names={report.declared_not_stored}
                ok="Catalog and database match"
                fix="Run permission:sync"
              />
              <DriftCard
                label="In database, not declared"
                names={report.stored_not_declared}
                ok="Nothing stale"
                fix="Remove them or add them to catalog.go"
              />
              <StatTile label="Tenant roles" value={report.tenant_roles} hint="created inside tenants" />
            </div>
            <Card className="mt-16" tight>
              <CardHeader title="Platform roles" sub="Roles without a tenant (seeded system roles)" />
              <div className="table-wrap flat">
                <table className="table">
                  <thead>
                    <tr>
                      <th>Role</th>
                      <th>Guard</th>
                      <th className="num">Permissions</th>
                      <th className="num">Users</th>
                    </tr>
                  </thead>
                  <tbody>
                    {roles.map((role) => (
                      <tr key={role.id}>
                        <td>
                          <b>{role.name}</b> {role.is_system ? <Badge tone="violet">system</Badge> : null}
                        </td>
                        <td>
                          <span className="code-inline">{role.guard}</span>
                        </td>
                        <td className="num">{role.permissions}</td>
                        <td className="num">{role.users}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </Card>
            <div className="perm-grid mt-16">
              {[...groups.entries()].map(([group, permissions]) => (
                <Card key={group} tight>
                  <div className="perm-group">{group}</div>
                  {permissions.map((permission) => (
                    <div key={permission.name} className="perm-item">
                      <span
                        className={`dot ${permission.declared && permission.stored ? '' : permission.declared ? 'yellow' : 'red'}`}
                        title={
                          permission.declared && permission.stored
                            ? 'Declared and stored'
                            : permission.declared
                              ? 'Declared, not synced yet'
                              : 'In the database only'
                        }
                      />
                      <div>
                        <div className="mono small">
                          {permission.name} {permission.is_system ? <Badge tone="violet">system</Badge> : null}
                          {report.admin_permissions.includes(permission.name) ? <Badge tone="info">admin</Badge> : null}
                        </div>
                        <div className="small dim">{permission.description}</div>
                      </div>
                    </div>
                  ))}
                </Card>
              ))}
            </div>
          </>
        )
      }}
    </Loadable>
  )
}

function DriftCard({ label, names, ok, fix }: { label: string; names: string[]; ok: string; fix: string }) {
  return (
    <div className="stat-card">
      <div className="stat-label">{label}</div>
      <div className={`stat-value ${names.length ? 'warn' : ''}`}>
        {names.length}
      </div>
      <div className="stat-hint" title={names.join(', ')}>
        {names.length ? `${names.slice(0, 3).join(', ')}${names.length > 3 ? '…' : ''} — ${fix}` : ok}
      </div>
    </div>
  )
}

// ── Config ───────────────────────────────────────────────────────────────

function ConfigTab({ needle }: { needle: string }) {
  const [open, setOpen] = useState<Record<string, boolean>>({ app: true })
  const query = useQuery({
    queryKey: ['inspector', 'config'],
    queryFn: ({ signal }) => api<DevConfigSection[]>('/dev/api/inspector/config', { signal }),
  })
  return (
    <Loadable query={query}>
      {(sections) => (
        <>
          <div className="small dim mb-12">
            Values whose key looks like a secret (password, secret, key, token, DSN…) and credentials in URLs are
            masked.
          </div>
          <div className="stack">
            {sections.map((section) => {
              const entries = section.entries.filter((entry) => matches(needle, entry.key, entry.masked ? '' : entry.value))
              if (needle && entries.length === 0) return null
              const expanded = needle ? true : open[section.name]
              return (
                <Card key={section.name} tight>
                  <button
                    type="button"
                    className="accordion-head"
                    aria-expanded={expanded}
                    onClick={() => setOpen({ ...open, [section.name]: !open[section.name] })}
                  >
                    <Icon name="chevron" className={`accordion-caret ${expanded ? 'open' : ''}`} />
                    <b>{section.name}</b>
                    <span className="small dim">({entries.length})</span>
                  </button>
                  {expanded ? (
                    <div className="config-rows">
                      {entries.map((entry) => (
                        <div key={entry.key} className="config-row">
                          <span className="mono small dim">{entry.key}</span>
                          <span className={`mono small ${entry.masked ? 'masked' : ''}`}>
                            {entry.value === '' ? <span className="dim">(empty)</span> : entry.value}
                          </span>
                          {!entry.masked && entry.value ? <CopyButton value={entry.value} /> : <span />}
                        </div>
                      ))}
                    </div>
                  ) : null}
                </Card>
              )
            })}
          </div>
        </>
      )}
    </Loadable>
  )
}

// ── Packages ─────────────────────────────────────────────────────────────

function PackagesTab({ needle }: { needle: string }) {
  const [runtime, setRuntime] = useState('')
  const [kind, setKind] = useState('direct')
  const query = useQuery({
    queryKey: ['inspector', 'packages'],
    queryFn: ({ signal }) => api<DevPackagesReport>('/dev/api/inspector/packages', { signal }),
    staleTime: Number.POSITIVE_INFINITY,
  })
  const counts = useMemo(() => {
    const packages = query.data?.packages ?? []
    return {
      go: packages.filter((item) => item.runtime === 'go' && item.kind === 'require').length,
      indirect: packages.filter((item) => item.kind === 'indirect').length,
      npm: packages.filter((item) => item.runtime === 'npm').length,
    }
  }, [query.data])
  return (
    <Loadable query={query}>
      {(report) => {
        const rows = report.packages.filter(
          (item) =>
            (!runtime || item.runtime === runtime) &&
            (kind === 'all' || item.kind !== 'indirect') &&
            matches(needle, item.name, item.version),
        )
        return (
          <>
            <div className="grid cols-4">
              <StatTile label="Go" value={report.go_version} hint={report.module} />
              <StatTile label="Go modules" value={counts.go} hint="direct requirements" />
              <StatTile label="Indirect" value={counts.indirect} hint="pulled in by other modules" />
              <StatTile label="Dashboard (npm)" value={counts.npm} hint="web/dev/package.json" />
            </div>
            <Card className="mt-16" tight>
              <CardHeader
                title={`${rows.length} packages`}
                actions={
                  <div className="flex g-2">
                    <Select
                      size="sm"
                      value={runtime}
                      onChange={setRuntime}
                      ariaLabel="Ecosystem"
                      options={[
                        { value: '', label: 'Go and npm' },
                        { value: 'go', label: 'Go modules' },
                        { value: 'npm', label: 'npm packages' },
                      ]}
                    />
                    <Select
                      size="sm"
                      value={kind}
                      onChange={setKind}
                      ariaLabel="Kind"
                      options={[
                        { value: 'direct', label: 'Direct only' },
                        { value: 'all', label: 'Include indirect' },
                      ]}
                    />
                  </div>
                }
              />
              <div className="table-wrap flat">
                <table className="table">
                  <thead>
                    <tr>
                      <th>Package</th>
                      <th>Version</th>
                      <th>Kind</th>
                    </tr>
                  </thead>
                  <tbody>
                    {rows.map((item) => (
                      <tr key={`${item.runtime}:${item.name}`}>
                        <td className="mono">
                          <span className={`eco ${item.runtime}`}>{item.runtime}</span> {item.name}
                        </td>
                        <td>
                          <span className="code-inline">{item.version}</span>
                        </td>
                        <td className="small dim">{item.kind}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </Card>
          </>
        )
      }}
    </Loadable>
  )
}
