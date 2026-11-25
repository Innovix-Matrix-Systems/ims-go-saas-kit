import { createFileRoute } from '@tanstack/react-router'
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api, apiBlob, saveBlob, type Schemas } from '../api/client'
import { can, useSession } from '../auth/session'
import { formatBytes } from '../format'
import { featureName, formatMB, PlanChange, publicPackagesQuery, subscriptionStatusQuery, useMoney, usePrice, type Change, type Package, PricingCard } from '../components/billing'
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
  scheduled: 'info',
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
  refunded: 'neutral',
}

/** Renew is offered in the last half of the period, at most this many days before it ends. */
const RENEW_WINDOW_DAYS = 30

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
          <PlanCard status={status.data} canManage={can(session, 'billing.manage')} waiting={waiting != null} />
          <UsageCard usage={usage.data} />
        </div>
        <PlansCard status={status.data} canManage={can(session, 'billing.manage')} waiting={waiting != null} />
        <PaymentsCard />
      </div>
    </>
  )
}

function PlanCard({ status, canManage, waiting }: { status?: Schemas['SubscriptionStatusEnvelope']['data']; canManage: boolean; waiting: boolean }) {
  const t = useT()
  const price = usePrice()
  const date = useDate()
  const money = useMoney()
  const client = useQueryClient()
  const { push } = useToast()
  const [renewing, setRenewing] = useState<Package | null>(null)
  const [stopping, setStopping] = useState(false)
  const [busy, setBusy] = useState(false)
  if (!status) {
    return (
      <Card title={t('billing.currentPlan')}>
        <ListSkeleton rows={3} />
      </Card>
    )
  }
  const subscription = status.subscription
  const pkg = status.package
  const live = status.active && subscription && pkg
  const refresh = async () => {
    await client.invalidateQueries({ queryKey: ['subscription-status'] })
    await client.invalidateQueries({ queryKey: ['payments'] })
    await client.invalidateQueries({ queryKey: ['billing'] })
  }
  const planEnd = async (action: 'cancel' | 'keep') => {
    setBusy(true)
    try {
      await api(`/billing/subscription/${action}`, { method: 'POST' })
      await refresh()
      push(action === 'cancel' ? t('billing.cancelled') : t('billing.kept'), 'success')
    } catch (cause) {
      push(cause instanceof Error ? cause.message : String(cause), 'error')
    } finally {
      setBusy(false)
      setStopping(false)
    }
  }

  // Renew from RENEW_WINDOW_DAYS before the end, unless the next period is arranged or stopped.
  const day = 86_400_000
  const ends = subscription?.expires_at ? new Date(subscription.expires_at).getTime() : Infinity
  const starts = subscription?.starts_at ? new Date(subscription.starts_at).getTime() : ends - 30 * day
  const renewWindow = Math.min(RENEW_WINDOW_DAYS, (ends - starts) / day / 2)
  const renewTo = status.next_package ?? pkg
  const canRenew =
    live &&
    canManage &&
    !waiting &&
    renewTo &&
    renewTo.price > 0 &&
    pkg.billing_cycle !== 'one_time' &&
    !status.scheduled &&
    !subscription.cancel_at_period_end &&
    (ends - Date.now()) / day <= renewWindow
  const canStop = live && canManage && !subscription.cancel_at_period_end && subscription.status !== 'trial'
  const arranged = live && (subscription.cancel_at_period_end || status.next_package || status.scheduled)

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
            {status.credit_balance > 0 ? (
              <>
                <dt>{t('billing.credit')}</dt>
                <dd title={t('billing.creditHint')}>{money(status.credit_balance, status.currency)}</dd>
              </>
            ) : null}
          </dl>
          {live && subscription.cancel_at_period_end ? (
            <div className="plan-note">
              <span className="status-dot warn" />
              <span className="fill">{t('billing.endsOn', { date: date(subscription.expires_at) })}</span>
            </div>
          ) : null}
          {live && status.next_package ? (
            <div className="plan-note">
              <span className="status-dot" />
              <span className="fill">
                {t('billing.movesTo', { name: status.next_package.name, date: date(subscription.expires_at), amount: money(status.next_package.price) })}
              </span>
            </div>
          ) : null}
          {live && status.scheduled && status.scheduled_package ? (
            <div className="plan-note">
              <span className="status-dot ok" />
              <span className="fill">
                {status.scheduled_package.price === 0
                  ? t('billing.movesToFree', { name: status.scheduled_package.name, date: date(status.scheduled.starts_at) })
                  : t('billing.nextPeriod', { name: status.scheduled_package.name, start: date(status.scheduled.starts_at), end: date(status.scheduled.expires_at) })}
              </span>
            </div>
          ) : null}
          {canManage && (canRenew || canStop || arranged) ? (
            <div className="plan-actions">
              {canRenew ? (
                <Button size="sm" variant="primary" icon="refresh" onClick={() => setRenewing(renewTo)}>
                  {t('billing.renew')}
                </Button>
              ) : null}
              {arranged && (subscription.cancel_at_period_end || status.next_package || status.scheduled_package?.price === 0) ? (
                <Button size="sm" disabled={busy} onClick={() => void planEnd('keep')}>
                  {t('billing.keep')}
                </Button>
              ) : null}
              {canStop ? (
                <Button size="sm" variant="ghost" disabled={busy} onClick={() => setStopping(true)}>
                  {t('billing.cancelRenewal')}
                </Button>
              ) : null}
            </div>
          ) : null}
        </div>
      )}
      <ConfirmDialog
        open={stopping}
        title={t('billing.cancelTitle')}
        description={pkg && subscription ? t('billing.cancelConfirm', { name: pkg.name, date: date(subscription.expires_at) }) : ''}
        confirmLabel={t('billing.cancelRenewal')}
        danger
        busy={busy}
        onConfirm={() => void planEnd('cancel')}
        onCancel={() => setStopping(false)}
      />
      <Modal open={renewing != null} title={renewing ? t('billing.renewTitle', { name: renewing.name }) : ''} onClose={() => setRenewing(null)}>
        {renewing ? (
          <PlanChange
            key={renewing.id}
            pkg={renewing}
            onDone={() => {
              setRenewing(null)
              push(t('onboarding.paymentPending'), 'success')
              void refresh()
            }}
          />
        ) : null}
      </Modal>
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

function PlansCard({ status, canManage, waiting }: { status?: Schemas['SubscriptionStatusEnvelope']['data']; canManage: boolean; waiting: boolean }) {
  const t = useT()
  const client = useQueryClient()
  const { push } = useToast()
  const date = useDate()
  const packages = useQuery(publicPackagesQuery)
  const [chosen, setChosen] = useState<Package | null>(null)
  const currentId = status?.active ? status.package?.id : undefined
  const nextId = status?.active ? (status.next_package?.id ?? status.scheduled_package?.id) : undefined

  const done = (pkg: Package, change: Change) => {
    setChosen(null)
    void client.invalidateQueries({ queryKey: ['subscription-status'] })
    void client.invalidateQueries({ queryKey: ['billing'] })
    void client.invalidateQueries({ queryKey: ['payments'] })
    if (change.kind === 'downgrade' || (change.effective === 'period_end' && !change.requires_payment)) {
      push(t('billing.change.later', { name: pkg.name, date: date(change.period_start) }), 'success')
    } else if (!change.requires_payment || change.amount_due === 0) {
      push(change.effective === 'now' ? t('billing.switched', { name: pkg.name }) : t('billing.change.later', { name: pkg.name, date: date(change.period_start) }), 'success')
    } else {
      push(t('onboarding.paymentPending'), 'success')
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
                chosen={pkg.id === nextId}
                action={
                  canManage ? (
                    current ? (
                      <span className="plan-current-note">
                        <Icon name="check" />
                        {t('billing.yourPlan')}
                      </span>
                    ) : pkg.id === nextId ? (
                      <span className="plan-current-note">{t('billing.fromDate', { date: date(status?.subscription?.expires_at) })}</span>
                    ) : (
                      <Button variant="primary" disabled={waiting} title={waiting ? t('billing.waitingHint') : undefined} onClick={() => setChosen(pkg)}>
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
      <Modal open={chosen != null} title={chosen ? t('billing.checkoutTitle', { name: chosen.name }) : ''} onClose={() => setChosen(null)}>
        {chosen ? <PlanChange key={chosen.id} pkg={chosen} onDone={(change) => done(chosen, change)} /> : null}
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
                <th className="end">{t('billing.colAmount')}</th>
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
                  <td className="end">
                    {money(payment.amount, payment.currency)}
                    {payment.kind && payment.kind !== 'new' ? <div className="small muted">{t(`billing.kind.${payment.kind}` as MessageKey)}</div> : null}
                    {payment.refunded_amount ? <div className="small muted">{t('billing.refundedAmount', { amount: money(payment.refunded_amount, payment.currency) })}</div> : null}
                  </td>
                  <td>
                    <Badge tone={paymentTones[payment.status]}>{t(`billing.payment.${payment.status}` as MessageKey)}</Badge>
                  </td>
                  <td className="end">
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
