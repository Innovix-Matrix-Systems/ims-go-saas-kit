import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { api, type Schemas } from '../api/client'
import { useMoney } from '../components/billing'
import { allTenantsQuery } from '../components/console'
import { Select } from '../components/inputs'
import { Pagination } from '../components/Pagination'
import { Badge, Card, EmptyState, PageHeader, type BadgeTone } from '../components/ui'
import { intlLocale, useLocale, useT, type MessageKey } from '../i18n'
import { TableSkeleton } from '../components/Skeleton'

interface PaymentsSearch {
  /** One organization's payments (an organization's drawer links here). */
  org?: number
}

export const Route = createFileRoute('/_app/console/payments')({
  validateSearch: (search: Record<string, unknown>): PaymentsSearch => ({
    org: Number(search.org) > 0 ? Number(search.org) : undefined,
  }),
  component: PaymentsPage,
})

type Payment = Schemas['Payment']
interface PaymentPage {
  data: Payment[]
  meta: Schemas['PaginationMeta']
}

const statuses: Payment['status'][] = ['pending', 'awaiting_approval', 'processing', 'completed', 'failed', 'cancelled', 'refunded']
const paymentTones: Record<Payment['status'], BadgeTone> = {
  pending: 'warn',
  processing: 'warn',
  awaiting_approval: 'warn',
  completed: 'ok',
  failed: 'danger',
  cancelled: 'neutral',
  refunded: 'neutral',
}

/** Every organization's payments, newest first; a row opens the organization (refunds and credit there). */
function PaymentsPage() {
  const t = useT()
  const locale = useLocale()
  const money = useMoney()
  const { org } = Route.useSearch()
  const navigate = useNavigate({ from: '/console/payments' })
  const [status, setStatus] = useState('')
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState(20)

  const tenants = useQuery(allTenantsQuery)
  const tenantNames = useMemo(() => new Map((tenants.data ?? []).map((tenant) => [tenant.id, tenant.name])), [tenants.data])
  const payments = useQuery({
    queryKey: ['console', 'payments', org ?? 0, status, page, perPage],
    queryFn: () =>
      api<PaymentPage>('/billing/internal/payments', {
        asStaff: true,
        envelope: true,
        query: { tenant_id: org, status: status || undefined, page, per_page: perPage },
      }),
    placeholderData: keepPreviousData,
  })

  // The organization filter lives in the URL, so a link can open it set.
  const setOrg = (value: string) => {
    setPage(1)
    void navigate({ search: (prev) => ({ ...prev, org: value ? Number(value) : undefined }), replace: true })
  }
  const when = (value: string) => new Intl.DateTimeFormat(intlLocale(locale), { dateStyle: 'medium' }).format(new Date(value))

  return (
    <>
      <PageHeader title={t('console.payments')} description={t('console.paymentsSubtitle')} />
      <div className="toolbar">
        <Select
          value={org ? String(org) : ''}
          ariaLabel={t('console.colOrg')}
          searchable
          onChange={setOrg}
          options={[{ value: '', label: t('console.filter.allOrgs') }, ...(tenants.data ?? []).map((tenant) => ({ value: String(tenant.id), label: tenant.name }))]}
        />
        <Select
          value={status}
          ariaLabel={t('billing.colStatus')}
          onChange={(value) => {
            setStatus(value)
            setPage(1)
          }}
          options={[{ value: '', label: t('console.filter.allStatuses') }, ...statuses.map((value) => ({ value, label: t(`billing.payment.${value}` as MessageKey) }))]}
        />
      </div>
      <Card flush>
        {payments.isLoading ? (
          <TableSkeleton columns={6} avatar={false} />
        ) : (payments.data?.data ?? []).length === 0 ? (
          <EmptyState icon="list" title={t('billing.noPayments')} />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>{t('console.colOrg')}</th>
                  <th>{t('billing.colInvoice')}</th>
                  <th className="end">{t('billing.colAmount')}</th>
                  <th>{t('billing.colMethod')}</th>
                  <th>{t('billing.colStatus')}</th>
                  <th>{t('billing.colDate')}</th>
                </tr>
              </thead>
              <tbody>
                {payments.data!.data.map((payment) => (
                  <tr key={payment.id} className="clickable" onClick={() => void navigate({ to: '/console/organizations', search: { org: payment.tenant_id } })}>
                    <td>{tenantNames.get(payment.tenant_id) ?? `#${payment.tenant_id}`}</td>
                    <td className="mono small">{payment.invoice_number}</td>
                    <td className="end nowrap">{money(payment.amount, payment.currency)}</td>
                    <td className="small">{t(`billing.method.${payment.payment_method}` as MessageKey)}</td>
                    <td>
                      <Badge tone={paymentTones[payment.status]}>{t(`billing.payment.${payment.status}` as MessageKey)}</Badge>
                    </td>
                    <td className="small muted">{when(payment.paid_at ?? payment.created_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {payments.data ? (
          <Pagination
            page={page}
            perPage={perPage}
            total={payments.data.meta.total}
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
