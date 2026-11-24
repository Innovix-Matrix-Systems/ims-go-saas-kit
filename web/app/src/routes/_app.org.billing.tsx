import { createFileRoute } from '@tanstack/react-router'
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api, apiBlob, saveBlob, type Schemas } from '../api/client'
import { can, useSession } from '../auth/session'
import { formatBytes } from '../format'
import { BankTransferForm, featureName, formatMB, publicPackagesQuery, subscriptionStatusQuery, useMoney, usePrice, type Package, PricingCard } from '../components/billing'
import { Pagination } from '../components/Pagination'
import { useToast } from '../components/Toasts'
import { Badge, Button, Card, ConfirmDialog, EmptyState, Icon, Modal, PageHeader, type BadgeTone } from '../components/ui'
import { intlLocale, useLocale, useT, type MessageKey } from '../i18n'
import { CardsSkeleton, ListSkeleton, TableSkeleton } from '../components/Skeleton'

export const Route = createFileRoute('/_app/org/billing')({
  component: BillingPage,
})

type Payment = Schemas['Payment']
type Subscription = Schemas['Subscription']
type Usage = Schemas['BillingUsage']
interface PaymentPage {
  data: Payment[]
  meta: Schemas['PaginationMeta']
}

const subscriptionTones: Record<Subscription['status'], BadgeTone> = {
  trial: 'info',
  pending: 'warn',
  active: 'ok',
  past_due: 'danger',
  cancelled: 'neutral',
  expired: 'danger',
}
const paymentTones: Record<Payment['status'], BadgeTone> = {
  pending: 'warn',
  processing: 'warn',
  awaiting_approval: 'warn',
  completed: 'ok',
  failed: 'danger',
  cancelled: 'neutral',
}

function useDate() {
  const locale = useLocale()
  return (value?: string | null) => (value ? new Intl.DateTimeFormat(intlLocale(locale), { dateStyle: 'medium' }).format(new Date(value)) : '—')
}

function BillingPage() {
  const t = useT()
  const session = useSession()
  const money = useMoney()
  const status = useQuery(subscriptionStatusQuery)
  const usage = useQuery({ queryKey: ['billing', 'usage'], queryFn: () => api<Usage>('/billing/usage') })
  const latest = useQuery({
    queryKey: ['payments', 'latest'],
    queryFn: () => api<PaymentPage>('/billing/payments', { envelope: true, query: { page: 1, per_page: 1 } }),
  })
  const waiting = latest.data?.data.find((payment) => payment.status === 'awaiting_approval' || payment.status === 'pending')

  return (
    <>
      <PageHeader title={t('org.billing')} description={t('billing.subtitle')} />
      <div className="stack g-4">
        {waiting ? (
          <div className="banner">
            <span className="status-dot warn" />
            <span className="fill">{t('billing.pendingNotice', { amount: money(waiting.amount, waiting.currency), invoice: waiting.invoice_number })}</span>
            {can(session, 'billing.manage') ? <WithdrawButton payment={waiting} /> : null}
          </div>
        ) : null}
        <div className="billing-top">
          <PlanCard status={status.data} />
          <UsageCard usage={usage.data} />
        </div>
        <PlansCard currentId={status.data?.active ? status.data.package?.id : undefined} canManage={can(session, 'billing.manage')} />
        <PaymentsCard />
      </div>
    </>
  )
}

function PlanCard({ status }: { status?: Schemas['SubscriptionStatusEnvelope']['data'] }) {
  const t = useT()
  const price = usePrice()
  const date = useDate()
  if (!status) {
    return (
      <Card title={t('billing.currentPlan')}>
        <ListSkeleton rows={3} />
      </Card>
    )
  }
  const subscription = status.subscription
  const pkg = status.package
  return (
    <Card title={t('billing.currentPlan')}>
      {!pkg || !subscription ? (
        <p className="muted">{t('billing.noPlan')}</p>
      ) : (
        <div className="stack">
          <div className="flex wrap">
            <span className="plan-name">{pkg.name}</span>
            <Badge tone={subscriptionTones[subscription.status]}>{t(`billing.status.${subscription.status}` as MessageKey)}</Badge>
          </div>
          <div className="plan-price">{price(pkg)}</div>
          <dl className="kv billing-kv">
            <dt>{t('billing.started')}</dt>
            <dd>{date(subscription.starts_at ?? subscription.created_at)}</dd>
            {subscription.trial_ends_at ? (
              <>
                <dt>{t('billing.trialEnds')}</dt>
                <dd>{date(subscription.trial_ends_at)}</dd>
              </>
            ) : null}
            <dt>{t('billing.renews')}</dt>
            <dd>{subscription.expires_at ? date(subscription.expires_at) : t('billing.noEnd')}</dd>
          </dl>
        </div>
      )}
    </Card>
  )
}

function UsageCard({ usage }: { usage?: Usage }) {
  const t = useT()
  return (
    <Card title={t('billing.usage')}>
      {!usage ? (
        <ListSkeleton rows={3} />
      ) : (
        <div className="stack">
          {usage.limits.map((item) => {
            const storage = item.feature_key === 'storage.mb'
            const used = storage ? formatBytes(usage.storage_bytes) : String(item.used)
            const ratio = item.limit > 0 ? Math.min(1, item.used / item.limit) : 0
            const tone = ratio >= 1 ? 'danger' : ratio >= 0.8 ? 'warn' : 'ok'
            return (
              <div key={item.feature_key} className="usage-row">
                <div className="flex">
                  <span className="usage-name">{featureName(t, item.feature_key)}</span>
                  <span className="spacer" />
                  <span className="small muted">
                    {item.limit === -1
                      ? t('billing.usedUnlimited', { used })
                      : item.limit === 0
                        ? t('billing.notIncluded')
                        : t('billing.usedOf', { used, limit: storage ? formatMB(item.limit) : String(item.limit) })}
                  </span>
                </div>
                {item.limit > 0 ? (
                  <div className="meter" role="meter" aria-valuemin={0} aria-valuemax={item.limit} aria-valuenow={item.used}>
                    <span className={`meter-fill ${tone}`} style={{ width: `${Math.max(2, ratio * 100)}%` }} />
                  </div>
                ) : null}
              </div>
            )
          })}
        </div>
      )}
    </Card>
  )
}

function PlansCard({ currentId, canManage }: { currentId?: number; canManage: boolean }) {
  const t = useT()
  const client = useQueryClient()
  const { push } = useToast()
  const packages = useQuery(publicPackagesQuery)
  const [checkout, setCheckout] = useState<Package | null>(null)
  const [free, setFree] = useState<Package | null>(null)
  const [busy, setBusy] = useState(false)

  // A free plan has nothing to pay: it starts as soon as it is chosen.
  const switchToFree = async (pkg: Package) => {
    setBusy(true)
    try {
      await api('/billing/subscribe', { method: 'POST', idempotent: true, body: { package_id: pkg.id } })
      await client.invalidateQueries({ queryKey: ['subscription-status'] })
      await client.invalidateQueries({ queryKey: ['billing', 'usage'] })
      await client.invalidateQueries({ queryKey: ['payments'] })
      push(t('billing.switched', { name: pkg.name }), 'success')
    } catch (cause) {
      push(cause instanceof Error ? cause.message : String(cause), 'error')
    } finally {
      setBusy(false)
      setFree(null)
    }
  }

  return (
    <Card title={t('billing.plans')} description={canManage ? t('billing.plansHint') : t('billing.manageHint')}>
      {!packages.data ? (
        <CardsSkeleton count={3} />
      ) : (
        <div className="plan-grid">
          {packages.data.map((pkg) => {
            const current = pkg.id === currentId
            return (
              <PricingCard
                key={pkg.id}
                pkg={pkg}
                current={current}
                action={
                  canManage ? (
                    current ? (
                      <span className="plan-current-note">
                        <Icon name="check" />
                        {t('billing.yourPlan')}
                      </span>
                    ) : (
                      <Button variant="primary" onClick={() => (pkg.price === 0 ? setFree(pkg) : setCheckout(pkg))}>
                        {t('billing.switch')}
                      </Button>
                    )
                  ) : undefined
                }
              />
            )
          })}
        </div>
      )}
      <ConfirmDialog
        open={free != null}
        title={free ? t('billing.freeTitle', { name: free.name }) : ''}
        description={free ? t('billing.freeConfirm', { name: free.name }) : ''}
        confirmLabel={t('billing.switch')}
        busy={busy}
        onConfirm={() => free && void switchToFree(free)}
        onCancel={() => setFree(null)}
      />
      <Modal open={checkout != null} title={checkout ? t('billing.checkoutTitle', { name: checkout.name }) : ''} onClose={() => setCheckout(null)}>
        {checkout ? (
          <BankTransferForm
            key={checkout.id}
            pkg={checkout}
            onSubmitted={() => {
              setCheckout(null)
              push(t('onboarding.paymentPending'), 'success')
              void client.invalidateQueries({ queryKey: ['payments'] })
              void client.invalidateQueries({ queryKey: ['subscription-status'] })
            }}
          />
        ) : null}
      </Modal>
    </Card>
  )
}

// The invoice as a PDF, or as an HTML page (opens styled and prints).
async function downloadInvoice(payment: Payment, format: 'pdf' | 'html') {
  const path = format === 'pdf' ? `/billing/payments/${payment.id}/invoice.pdf` : `/billing/payments/${payment.id}/invoice`
  try {
    saveBlob(await apiBlob(path), `invoice-${payment.invoice_number}.${format}`)
  } catch {
    // apiBlob reports the error.
  }
}

/** Withdraws a payment nobody has confirmed yet, after asking. */
function WithdrawButton({ payment }: { payment: Payment }) {
  const t = useT()
  const money = useMoney()
  const client = useQueryClient()
  const { push } = useToast()
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const withdraw = async () => {
    setBusy(true)
    try {
      await api(`/billing/payments/${payment.id}/cancel`, { method: 'POST' })
      await client.invalidateQueries({ queryKey: ['payments'] })
      await client.invalidateQueries({ queryKey: ['subscription-status'] })
      push(t('billing.withdrawn'), 'success')
    } catch (cause) {
      push(cause instanceof Error ? cause.message : String(cause), 'error')
    } finally {
      setBusy(false)
      setOpen(false)
    }
  }
  return (
    <>
      <Button size="sm" variant="ghost" icon="x" onClick={() => setOpen(true)}>
        {t('billing.withdraw')}
      </Button>
      <ConfirmDialog
        open={open}
        title={t('billing.withdrawTitle')}
        description={t('billing.withdrawConfirm', { amount: money(payment.amount, payment.currency), invoice: payment.invoice_number })}
        confirmLabel={t('billing.withdraw')}
        danger
        busy={busy}
        onConfirm={() => void withdraw()}
        onCancel={() => setOpen(false)}
      />
    </>
  )
}

function PaymentsCard() {
  const t = useT()
  const session = useSession()
  const canManage = can(session, 'billing.manage')
  const money = useMoney()
  const date = useDate()
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState(10)
  const payments = useQuery({
    queryKey: ['payments', page, perPage],
    queryFn: () => api<PaymentPage>('/billing/payments', { envelope: true, query: { page, per_page: perPage } }),
    placeholderData: keepPreviousData,
  })
  return (
    <Card title={t('billing.payments')} flush>
      {payments.isLoading ? (
        <TableSkeleton columns={5} avatar={false} />
      ) : (payments.data?.data ?? []).length === 0 ? (
        <EmptyState icon="list" title={t('billing.noPayments')} />
      ) : (
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <th>{t('billing.colInvoice')}</th>
                <th>{t('billing.colDate')}</th>
                <th>{t('billing.colMethod')}</th>
                <th className="right">{t('billing.colAmount')}</th>
                <th>{t('billing.colStatus')}</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {payments.data!.data.map((payment) => (
                <tr key={payment.id}>
                  <td className="mono small">{payment.invoice_number}</td>
                  <td className="small muted">{date(payment.paid_at ?? payment.created_at)}</td>
                  <td className="small">{t(`billing.method.${payment.payment_method}` as MessageKey)}</td>
                  <td className="right">{money(payment.amount, payment.currency)}</td>
                  <td>
                    <Badge tone={paymentTones[payment.status]}>{t(`billing.payment.${payment.status}` as MessageKey)}</Badge>
                  </td>
                  <td className="right">
                    <div className="flex justify-end">
                      <Button size="sm" variant="ghost" icon="download" onClick={() => void downloadInvoice(payment, 'pdf')}>
                        {t('billing.invoice')}
                      </Button>
                      <Button size="sm" variant="ghost" title={t('billing.invoiceHtmlHint')} onClick={() => void downloadInvoice(payment, 'html')}>
                        {t('billing.invoiceHtml')}
                      </Button>
                      {canManage && payment.status === 'pending' ? <WithdrawButton payment={payment} /> : null}
                    </div>
                  </td>
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
  )
}
