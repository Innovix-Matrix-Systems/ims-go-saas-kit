import { createFileRoute } from '@tanstack/react-router'
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api, type Schemas } from '../api/client'
import { can, useSession } from '../auth/session'
import { formatBytes } from '../format'
import {
  BankTransferForm,
  featureName,
  formatMB,
  publicPackagesQuery,
  subscriptionStatusQuery,
  useFeatureText,
  useMoney,
  usePrice,
  type Package,
} from '../components/billing'
import { Pagination } from '../components/Pagination'
import { useToast } from '../components/Toasts'
import { Badge, Button, Card, EmptyState, Modal, PageHeader, Spinner, type BadgeTone } from '../components/ui'
import { intlLocale, useLocale, useT, type MessageKey } from '../i18n'

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
  const usage = useQuery({ queryKey: ['billing-usage'], queryFn: () => api<Usage>('/billing/usage') })
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
            {t('billing.pendingNotice', { amount: money(waiting.amount, waiting.currency), invoice: waiting.invoice_number })}
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
        <Spinner />
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
        <Spinner />
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
  const price = usePrice()
  const featureText = useFeatureText()
  const client = useQueryClient()
  const { push } = useToast()
  const packages = useQuery(publicPackagesQuery)
  const [checkout, setCheckout] = useState<Package | null>(null)

  return (
    <Card title={t('billing.plans')} description={canManage ? t('billing.freeHint') : t('billing.manageHint')}>
      {!packages.data ? (
        <Spinner />
      ) : (
        <div className="plan-grid">
          {packages.data.map((pkg) => {
            const current = pkg.id === currentId
            return (
              <div key={pkg.id} className={`plan-card ${current ? 'current' : ''}`}>
                <div className="flex g-2 align-center">
                  <strong>{pkg.name}</strong>
                  {current ? <Badge tone="info">{t('onboarding.currentPlan')}</Badge> : null}
                </div>
                <div className="plan-price">{price(pkg)}</div>
                {pkg.description ? <p className="small muted">{pkg.description}</p> : null}
                <ul className="plan-features">
                  {pkg.features
                    .filter((feature) => feature.is_enabled)
                    .map((feature) => (
                      <li key={feature.feature_key}>{featureText(feature.feature_key, feature.limit_value)}</li>
                    ))}
                </ul>
                {canManage ? (
                  <Button variant={current ? 'ghost' : 'default'} disabled={current || pkg.price === 0} onClick={() => setCheckout(pkg)}>
                    {current ? t('onboarding.chosen') : t('billing.switch')}
                  </Button>
                ) : null}
              </div>
            )
          })}
        </div>
      )}
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

function PaymentsCard() {
  const t = useT()
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
        <div className="p-4">
          <Spinner />
        </div>
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
