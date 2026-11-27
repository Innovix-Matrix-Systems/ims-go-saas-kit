import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { api, type Schemas } from '../api/client'
import { useMoney, type Package } from '../components/billing'
import { Select } from '../components/inputs'
import { Pagination } from '../components/Pagination'
import { Badge, Card, EmptyState, PageHeader, type BadgeTone } from '../components/ui'
import { intlLocale, useLocale, useT, type MessageKey } from '../i18n'
import { TableSkeleton } from '../components/Skeleton'

export const Route = createFileRoute('/_app/console/subscriptions')({
  component: SubscriptionsPage,
})

type Row = Schemas['StaffSubscription']
type Counts = Schemas['StaffSubscriptionCountsEnvelope']['data']
interface RowPage {
  data: Row[]
  meta: Schemas['PaginationMeta']
}

type View = keyof Counts
const views: { value: View; label: MessageKey }[] = [
  { value: 'current', label: 'console.view.current' },
  { value: 'active', label: 'console.view.active' },
  { value: 'trial', label: 'console.view.trial' },
  { value: 'grace', label: 'console.view.grace' },
  { value: 'read_only', label: 'console.view.readOnly' },
  { value: 'pending', label: 'console.view.pending' },
  { value: 'scheduled', label: 'console.view.scheduled' },
  { value: 'all', label: 'console.view.all' },
]

const statusTones: Record<Row['status'], BadgeTone> = {
  trial: 'info',
  pending: 'warn',
  scheduled: 'info',
  active: 'ok',
  past_due: 'danger',
  cancelled: 'neutral',
  expired: 'danger',
}

const cycleLabels: Record<Row['price_cycle'], MessageKey> = {
  monthly: 'console.monthly',
  yearly: 'console.yearly',
  one_time: 'console.oneTime',
}

const packagesQuery = { queryKey: ['console', 'packages'], queryFn: () => api<Package[]>('/packages', { asStaff: true }) }

/**
 * Every organization's subscription in one list: tabs by standing, search and filters, and a row
 * opens the organization (its plan, payments, credit and the staff actions there).
 */
function SubscriptionsPage() {
  const t = useT()
  const locale = useLocale()
  const money = useMoney()
  const navigate = useNavigate()
  const [view, setView] = useState<View>('current')
  const [search, setSearch] = useState('')
  const [query, setQuery] = useState('')
  const [packageId, setPackageId] = useState('')
  const [cycle, setCycle] = useState('')
  const [within, setWithin] = useState('')
  const [sort, setSort] = useState<'newest' | 'expires'>('newest')
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState(20)

  useEffect(() => {
    const id = window.setTimeout(() => {
      setQuery(search.trim())
      setPage(1)
    }, 300)
    return () => window.clearTimeout(id)
  }, [search])

  const packages = useQuery(packagesQuery)
  const counts = useQuery({
    queryKey: ['console', 'subscriptions', 'summary'],
    queryFn: () => api<Counts>('/billing/internal/subscriptions/summary', { asStaff: true }),
  })
  const rows = useQuery({
    queryKey: ['console', 'subscriptions', view, query, packageId, cycle, within, sort, page, perPage],
    queryFn: () =>
      api<RowPage>('/billing/internal/subscriptions', {
        asStaff: true,
        envelope: true,
        query: {
          view,
          search: query || undefined,
          package_id: packageId || undefined,
          billing_cycle: cycle || undefined,
          expires_within_days: within || undefined,
          // The kit's ordering parameters: the soonest end first, or the newest first.
          order_by: sort === 'expires' ? 'expires_at' : 'id',
          order_direction: sort === 'expires' ? 'asc' : 'desc',
          page,
          per_page: perPage,
        },
      }),
    placeholderData: keepPreviousData,
  })

  const date = (value?: string | null) => (value ? new Intl.DateTimeFormat(intlLocale(locale), { dateStyle: 'medium' }).format(new Date(value)) : '—')
  const planName = (id?: number | null) => packages.data?.find((pkg) => pkg.id === id)?.name ?? ''
  const filter = <T,>(set: (value: T) => void) => (value: T) => {
    set(value)
    setPage(1)
  }

  // What happens when the period ends: renews, stops, or moves to another plan.
  const renewal = (row: Row) => {
    if (row.status !== 'active' && row.status !== 'trial') return <span className="muted">—</span>
    if (row.cancel_at_period_end) return t('console.renewal.stops')
    if (row.next_package_id) return t('console.renewal.changesTo', { name: planName(row.next_package_id) })
    return row.price_cycle === 'one_time' ? <span className="muted">—</span> : t('console.renewal.renews')
  }

  return (
    <>
      <PageHeader title={t('console.subscriptions')} description={t('console.subscriptionsSubtitle')} />
      <nav className="tabs" aria-label={t('console.subscriptions')}>
        {views.map((item) => (
          <button key={item.value} type="button" className={`tab ${view === item.value ? 'active' : ''}`} aria-pressed={view === item.value} onClick={() => filter(setView)(item.value)}>
            {t(item.label)}
            {counts.data ? <span className="count">{counts.data[item.value]}</span> : null}
          </button>
        ))}
      </nav>
      <div className="toolbar">
        <input className="input toolbar-search" type="search" placeholder={t('console.searchOrgs')} value={search} onChange={(e) => setSearch(e.target.value)} />
        <Select
          value={packageId}
          ariaLabel={t('console.colPlan')}
          onChange={filter(setPackageId)}
          options={[{ value: '', label: t('console.filter.allPlans') }, ...(packages.data ?? []).map((pkg) => ({ value: String(pkg.id), label: pkg.name }))]}
        />
        <Select
          value={cycle}
          ariaLabel={t('console.cycle')}
          onChange={filter(setCycle)}
          options={[
            { value: '', label: t('console.filter.allCycles') },
            { value: 'monthly', label: t('console.monthly') },
            { value: 'yearly', label: t('console.yearly') },
            { value: 'one_time', label: t('console.oneTime') },
          ]}
        />
        <Select
          value={within}
          ariaLabel={t('console.filter.ends')}
          onChange={filter(setWithin)}
          options={[
            { value: '', label: t('console.filter.anyEnd') },
            { value: '7', label: t('console.filter.within7') },
            { value: '30', label: t('console.filter.within30') },
            { value: '90', label: t('console.filter.within90') },
          ]}
        />
        <Select
          value={sort}
          ariaLabel={t('console.filter.sort')}
          onChange={filter(setSort)}
          options={[
            { value: 'newest', label: t('console.filter.newest') },
            { value: 'expires', label: t('console.filter.endingFirst') },
          ]}
        />
      </div>
      <Card flush>
        {rows.isLoading ? (
          <TableSkeleton columns={7} avatar={false} />
        ) : (rows.data?.data ?? []).length === 0 ? (
          <EmptyState icon="refresh" title={t('console.noSubscriptions')} />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>{t('console.colOrg')}</th>
                  <th>{t('console.colPlan')}</th>
                  <th className="end">{t('console.colAmount')}</th>
                  <th>{t('console.colStatus')}</th>
                  <th>{t('console.colStarted')}</th>
                  <th>{t('console.colEnds')}</th>
                  <th>{t('console.colRenewal')}</th>
                </tr>
              </thead>
              <tbody>
                {rows.data!.data.map((row) => (
                  <tr key={row.id} className="clickable" onClick={() => void navigate({ to: '/console/organizations', search: { org: row.organization.id } })}>
                    <td>
                      <span className="member-meta">
                        <span className="member-name">{row.organization.name}</span>
                        <span className="small muted mono">#{row.organization.id}</span>
                      </span>
                    </td>
                    <td>
                      {row.package.name}
                      <div className="small muted">{t(cycleLabels[row.price_cycle])}</div>
                    </td>
                    <td className="end nowrap">{row.price === 0 ? t('onboarding.free') : money(row.price)}</td>
                    <td>
                      <div className="flex wrap g-1">
                        <Badge tone={statusTones[row.status]}>{t(`billing.status.${row.status}` as MessageKey)}</Badge>
                        {row.in_grace ? <Badge tone="danger">{t('console.badge.grace', { date: date(row.grace_ends_at) })}</Badge> : null}
                        {row.read_only ? <Badge tone="danger">{t('console.badge.readOnly')}</Badge> : null}
                      </div>
                    </td>
                    <td className="small muted">{date(row.starts_at)}</td>
                    <td className="small">{row.price_cycle === 'one_time' ? <span className="muted">—</span> : date(row.expires_at)}</td>
                    <td className="small">{renewal(row)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {rows.data ? (
          <Pagination
            page={page}
            perPage={perPage}
            total={rows.data.meta.total}
            onPage={setPage}
            onPerPage={(size) => {
              setPerPage(size)
              setPage(1)
            }}
          />
        ) : null}
      </Card>
    </>
  )
}
