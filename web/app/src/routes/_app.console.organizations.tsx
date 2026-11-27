import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState, type FormEvent } from 'react'
import { api, ApiError, type Schemas, type User } from '../api/client'
import { can, useSession } from '../auth/session'
import { atCycle, CycleSwitch, featureName, formatMB, useDefaultCurrency, useMoney, usePrice, visibleLimits, type Package, type SubscriptionStatus } from '../components/billing'
import { useStartActing, type Tenant } from '../components/console'
import { Field, useApiErrors } from '../components/form'
import { MoneyInput, Select } from '../components/inputs'
import { fromMinor, toMinor } from '../money'
import { Pagination } from '../components/Pagination'
import { useToast } from '../components/Toasts'
import { Badge, Button, Card, ConfirmDialog, Drawer, EmptyState, Icon, Modal, PageHeader, Spinner } from '../components/ui'
import { intlLocale, useLocale, useT, type MessageKey } from '../i18n'
import { ListSkeleton, TableSkeleton } from '../components/Skeleton'
import { OrgMark } from '../components/WorkspaceMenu'

interface OrganizationsSearch {
  /** An organization to open on arrival (the subscriptions panel links here). */
  org?: number
}

export const Route = createFileRoute('/_app/console/organizations')({
  validateSearch: (search: Record<string, unknown>): OrganizationsSearch => ({
    org: Number(search.org) > 0 ? Number(search.org) : undefined,
  }),
  component: OrganizationsPage,
})

interface TenantPage {
  data: Tenant[]
  meta: Schemas['PaginationMeta']
}
interface Organization {
  owner: { id: number; name: string; email: string } | null
}
interface Paged<T> {
  data: T[]
  meta: Schemas['PaginationMeta']
}

/** How many payments and members the drawer shows; "See all" opens the full list, filtered. */
const DRAWER_ROWS = 5

function useDate() {
  const locale = useLocale()
  return (value?: string | null) => (value ? new Intl.DateTimeFormat(intlLocale(locale), { dateStyle: 'medium' }).format(new Date(value)) : '—')
}

function OrganizationsPage() {
  const t = useT()
  const session = useSession()
  const date = useDate()
  const [search, setSearch] = useState('')
  const [query, setQuery] = useState('')
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState(20)
  const [creating, setCreating] = useState(false)
  const [selected, setSelected] = useState<Tenant | null>(null)
  const { org: linked } = Route.useSearch()
  const navigate = useNavigate()
  const linkedTenant = useQuery({
    queryKey: ['console', 'tenant', linked],
    queryFn: () => api<Tenant>(`/tenant/${linked}`, { asStaff: true }),
    enabled: linked != null,
  })
  // A linked organization opens once it has loaded.
  useEffect(() => {
    if (linkedTenant.data) setSelected(linkedTenant.data)
  }, [linkedTenant.data])
  const close = () => {
    setSelected(null)
    if (linked != null) void navigate({ to: '/console/organizations', search: {}, replace: true })
  }

  useEffect(() => {
    const id = window.setTimeout(() => {
      setQuery(search.trim())
      setPage(1)
    }, 300)
    return () => window.clearTimeout(id)
  }, [search])

  const tenants = useQuery({
    queryKey: ['console', 'tenants', query, page, perPage],
    queryFn: () => api<TenantPage>('/tenant', { asStaff: true, envelope: true, query: { search: query, page, per_page: perPage } }),
    placeholderData: keepPreviousData,
  })

  return (
    <>
      <PageHeader
        title={t('console.organizations')}
        description={t('console.orgSubtitle')}
        actions={
          can(session, 'tenant.create') ? (
            <Button variant="primary" icon="plus" onClick={() => setCreating(true)}>
              {t('console.newOrg')}
            </Button>
          ) : null
        }
      />
      <div className="toolbar">
        <input className="input toolbar-search" type="search" placeholder={t('console.searchOrgs')} value={search} onChange={(e) => setSearch(e.target.value)} />
      </div>
      <Card flush>
        {tenants.isLoading ? (
          <TableSkeleton columns={4} avatar={false} />
        ) : (tenants.data?.data ?? []).length === 0 ? (
          <EmptyState icon="tenants" title={t('branches.empty')} />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>{t('console.colOrg')}</th>
                  <th>{t('console.colContact')}</th>
                  <th>{t('console.colStatus')}</th>
                  <th>{t('console.colCreated')}</th>
                </tr>
              </thead>
              <tbody>
                {tenants.data!.data.map((tenant) => (
                  <tr key={tenant.id} className="clickable" onClick={() => setSelected(tenant)}>
                    <td>
                      <span className="org-cell">
                        <OrgMark name={tenant.name} logo={tenant.branding?.logo_url} />
                        <span className="member-meta">
                          <span className="member-name">{tenant.name}</span>
                          <span className="small muted mono">#{tenant.id}</span>
                        </span>
                      </span>
                    </td>
                    <td className="small muted">{tenant.email || tenant.phone || '—'}</td>
                    <td>{tenant.is_active ? <Badge tone="ok">{t('console.active')}</Badge> : <Badge tone="danger">{t('console.suspended')}</Badge>}</td>
                    <td className="small muted">{date(tenant.created_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {tenants.data ? (
          <Pagination
            page={page}
            perPage={perPage}
            total={tenants.data.meta.total}
            onPage={setPage}
            onPerPage={(size) => {
              setPerPage(size)
              setPage(1)
            }}
          />
        ) : null}
      </Card>
      {creating ? <CreateOrganization onClose={() => setCreating(false)} onCreated={setSelected} /> : null}
      {selected ? <OrganizationDrawer tenant={selected} onClose={close} onChanged={setSelected} /> : null}
    </>
  )
}

function CreateOrganization({ onClose, onCreated }: { onClose: () => void; onCreated: (tenant: Tenant) => void }) {
  const t = useT()
  const client = useQueryClient()
  const errors = useApiErrors()
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [phone, setPhone] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    errors.clear()
    try {
      const tenant = await api<Tenant>('/tenant', {
        method: 'POST',
        asStaff: true,
        idempotent: true,
        body: { name: name.trim(), email: email.trim() || null, phone: phone.trim() || null, is_active: true },
      })
      await client.invalidateQueries({ queryKey: ['console', 'tenants'] })
      onClose()
      onCreated(tenant)
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Modal open title={t('console.newOrg')} onClose={onClose}>
      <form onSubmit={submit} className="stack">
        <Field label={t('org.name')} error={errors.field('name')}>
          <input className="input" name="organization" autoComplete="off" required value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <div className="form-grid-2">
          <Field label={`${t('org.contactEmail')} (${t('form.optional')})`} error={errors.field('email')}>
            <input className="input" type="email" autoComplete="off" value={email} onChange={(e) => setEmail(e.target.value)} />
          </Field>
          <Field label={`${t('org.contactPhone')} (${t('form.optional')})`} error={errors.field('phone')}>
            <input className="input" type="tel" autoComplete="off" value={phone} onChange={(e) => setPhone(e.target.value)} />
          </Field>
        </div>
        {errors.box}
        <div className="modal-actions">
          <Button variant="ghost" type="button" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" type="submit" disabled={busy || name.trim().length < 2}>
            {busy ? <Spinner /> : <Icon name="plus" />} {t('console.newOrg')}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

function OrganizationDrawer({ tenant, onClose, onChanged }: { tenant: Tenant; onClose: () => void; onChanged: (tenant: Tenant) => void }) {
  const t = useT()
  const session = useSession()
  const client = useQueryClient()
  const { push } = useToast()
  const price = usePrice()
  const date = useDate()
  const acting = useStartActing()
  const [confirm, setConfirm] = useState<'impersonate' | 'suspend' | null>(null)
  const [loginAs, setLoginAs] = useState<User | null>(null)
  const [assigning, setAssigning] = useState(false)
  const [adjusting, setAdjusting] = useState(false)
  const [refunding, setRefunding] = useState<Schemas['Payment'] | null>(null)
  const [busy, setBusy] = useState(false)
  const money = useMoney()
  // Everything below reads this one organization as staff (X-Acting-Tenant for these calls only).
  const inTenant = { actingTenant: tenant.id }
  const organization = useQuery({ queryKey: ['console', 'tenant', tenant.id, 'org'], queryFn: () => api<Organization>('/organization', inTenant) })
  const status = useQuery({ queryKey: ['console', 'tenant', tenant.id, 'status'], queryFn: () => api<SubscriptionStatus>('/billing/subscription/status', inTenant) })
  const usage = useQuery({ queryKey: ['console', 'tenant', tenant.id, 'usage'], queryFn: () => api<Schemas['BillingUsage']>('/billing/usage', inTenant) })
  // The latest few only, with the total: the full history is on the Payments and Users pages.
  const payments = useQuery({
    queryKey: ['console', 'tenant', tenant.id, 'payments'],
    queryFn: () => api<Paged<Schemas['Payment']>>('/billing/payments', { ...inTenant, envelope: true, query: { per_page: DRAWER_ROWS } }),
  })
  const members = useQuery({
    queryKey: ['console', 'tenant', tenant.id, 'members'],
    queryFn: () => api<Paged<User>>('/user', { ...inTenant, envelope: true, query: { per_page: DRAWER_ROWS, order_by: 'name', order_direction: 'asc' } }),
  })
  const shown = (list?: Paged<unknown>) =>
    list && list.meta.total > list.data.length ? t('console.shownOf', { shown: list.data.length, total: list.meta.total }) : undefined

  const setActive = async (active: boolean) => {
    setBusy(true)
    try {
      const updated = await api<Tenant>(`/tenant/${tenant.id}`, { method: 'PATCH', asStaff: true, body: { name: tenant.name, is_active: active } })
      await client.invalidateQueries({ queryKey: ['console', 'tenants'] })
      onChanged(updated)
      push(t('form.saved'), 'success')
    } catch (cause) {
      push(cause instanceof Error ? cause.message : String(cause), 'error')
    } finally {
      setBusy(false)
      setConfirm(null)
    }
  }

  const failed = (cause: unknown) => push(cause instanceof ApiError || cause instanceof Error ? cause.message : String(cause), 'error')
  const subscription = status.data?.subscription
  const pkg = status.data?.package

  return (
    <Drawer
      open
      title={
        <span className="org-cell">
          <OrgMark name={tenant.name} logo={tenant.branding?.logo_url} size="lg" />
          {tenant.name}
        </span>
      }
      subtitle={`#${tenant.id} · ${tenant.code}`}
      onClose={onClose}
      actions={tenant.is_active ? <Badge tone="ok">{t('console.active')}</Badge> : <Badge tone="danger">{t('console.suspended')}</Badge>}
    >
      <div className="stack g-4">
        <div className="flex wrap">
          {can(session, 'user.tenant.switch') ? (
            <Button variant="primary" icon="tenants" onClick={() => setConfirm('impersonate')}>
              {t('console.impersonate')}
            </Button>
          ) : null}
          {can(session, 'subscription.manage') ? (
            <Button icon="package" onClick={() => setAssigning(true)}>
              {t('console.assignPlan')}
            </Button>
          ) : null}
          <span className="spacer" />
          {can(session, 'tenant.update') ? (
            tenant.is_active ? (
              <Button variant="danger" icon="pause" disabled={busy} onClick={() => setConfirm('suspend')}>
                {t('console.suspend')}
              </Button>
            ) : (
              <Button icon="play" disabled={busy} onClick={() => void setActive(true)}>
                {t('console.reactivate')}
              </Button>
            )
          ) : null}
        </div>

        <dl className="kv audit-kv">
          <dt>{t('console.owner')}</dt>
          <dd>{organization.data?.owner ? `${organization.data.owner.name} (${organization.data.owner.email})` : '—'}</dd>
          <dt>{t('console.plan')}</dt>
          <dd>
            {pkg && subscription ? (
              <span className="flex wrap">
                {pkg.name} · {price(atCycle(pkg, status.data?.billing_cycle))}
                <Badge tone={subscription.status === 'active' ? 'ok' : 'warn'}>{t(`billing.status.${subscription.status}` as MessageKey)}</Badge>
              </span>
            ) : (
              t('console.noPlan')
            )}
          </dd>
          <dt>{t('billing.renews')}</dt>
          <dd>
            {subscription?.expires_at ? date(subscription.expires_at) : '—'}
            {subscription?.cancel_at_period_end ? <span className="small muted"> · {t('console.wontRenew')}</span> : null}
            {status.data?.next_package ? <span className="small muted"> · {t('billing.fromDate', { date: date(subscription?.expires_at) })}: {status.data.next_package.name}</span> : null}
          </dd>
          <dt>{t('billing.credit')}</dt>
          <dd>
            <span className="flex wrap">
              {money(status.data?.credit_balance ?? 0, status.data?.currency)}
              {can(session, 'payment.refund') ? (
                <Button size="sm" variant="ghost" onClick={() => setAdjusting(true)}>
                  {t('console.adjustCredit')}
                </Button>
              ) : null}
            </span>
          </dd>
          <dt>{t('org.contactEmail')}</dt>
          <dd>{tenant.email || '—'}</dd>
          <dt>{t('console.colCreated')}</dt>
          <dd>{date(tenant.created_at)}</dd>
        </dl>

        {usage.data ? (
          <div className="usage-grid">
            {visibleLimits(usage.data.limits).map((item) => (
              <div key={item.feature_key} className="stat-card">
                <div className="stat-label">{featureName(t, item.feature_key)}</div>
                <div className="stat-value">{item.feature_key === 'storage.mb' ? formatMB(item.used) : item.used}</div>
                <div className="stat-hint">
                  {item.limit === -1 ? t('billing.unlimited') : item.limit === 0 ? t('billing.notIncluded') : `/ ${item.feature_key === 'storage.mb' ? formatMB(item.limit) : item.limit}`}
                </div>
              </div>
            ))}
          </div>
        ) : null}

        <Card
          title={t('billing.payments')}
          description={shown(payments.data)}
          actions={
            can(session, 'payment.view.all') && payments.data?.meta.total ? (
              <Link to="/console/payments" search={{ org: tenant.id }} className="link-btn">
                {t('dashboard.seeAll')}
              </Link>
            ) : undefined
          }
          flush
        >
          {!payments.data ? (
            <ListSkeleton rows={2} />
          ) : payments.data.data.length === 0 ? (
            <EmptyState icon="list" title={t('billing.noPayments')} />
          ) : (
            <ul className="role-list">
              {payments.data.data.map((payment) => (
                <li key={payment.id} className="console-member">
                  <span className="member-meta">
                    <span className="member-name">
                      {money(payment.amount, payment.currency)} <Badge tone={payment.status === 'completed' ? 'ok' : 'neutral'}>{t(`billing.payment.${payment.status}` as MessageKey)}</Badge>
                    </span>
                    <span className="small muted mono">
                      {payment.invoice_number} · {t(`billing.method.${payment.payment_method}` as MessageKey)}
                      {payment.refunded_amount ? ` · ${t('billing.refundedAmount', { amount: money(payment.refunded_amount, payment.currency) })}` : ''}
                    </span>
                  </span>
                  {can(session, 'payment.refund') && payment.status === 'completed' && payment.amount > (payment.refunded_amount ?? 0) ? (
                    <Button size="sm" onClick={() => setRefunding(payment)}>
                      {t('console.refund')}
                    </Button>
                  ) : null}
                </li>
              ))}
            </ul>
          )}
        </Card>

        <Card
          title={t('console.members')}
          description={shown(members.data)}
          actions={
            can(session, 'user.view.all') && members.data?.meta.total ? (
              <Link to="/console/users" search={{ org: tenant.id }} className="link-btn">
                {t('dashboard.seeAll')}
              </Link>
            ) : undefined
          }
          flush
        >
          {!members.data ? (
            <ListSkeleton rows={3} avatar />
          ) : members.data.data.length === 0 ? (
            <EmptyState icon="users" title={t('members.empty')} />
          ) : (
            <ul className="role-list">
              {members.data.data.map((user) => (
                <li key={user.id} className="console-member">
                  <span className="member-meta">
                    <span className="member-name">
                      {user.name} {user.id === organization.data?.owner?.id ? <Badge tone="info">{t('members.owner')}</Badge> : null}
                      {!user.is_active ? <Badge tone="neutral">{t('members.inactive')}</Badge> : null}
                    </span>
                    <span className="small muted">
                      {user.email} · {(user.roles ?? []).map((role) => role.name).join(', ') || '—'}
                    </span>
                  </span>
                  {can(session, 'user.impersonate') && user.is_active ? (
                    <Button size="sm" icon="user" onClick={() => setLoginAs(user)}>
                      {t('console.loginAs')}
                    </Button>
                  ) : null}
                </li>
              ))}
            </ul>
          )}
        </Card>
      </div>

      <ConfirmDialog
        open={confirm === 'impersonate'}
        title={t('console.impersonate')}
        description={t('console.impersonateConfirm', { name: tenant.name })}
        confirmLabel={t('console.impersonate')}
        onConfirm={() => void acting.impersonate(tenant).catch(failed)}
        onCancel={() => setConfirm(null)}
      />
      <ConfirmDialog
        open={confirm === 'suspend'}
        title={t('console.suspend')}
        description={t('console.suspendConfirm', { name: tenant.name })}
        confirmLabel={t('console.suspend')}
        danger
        busy={busy}
        onConfirm={() => void setActive(false)}
        onCancel={() => setConfirm(null)}
      />
      <ConfirmDialog
        open={loginAs != null}
        title={t('console.loginAs')}
        description={t('console.loginAsConfirm', { name: loginAs?.name ?? '' })}
        confirmLabel={t('console.loginAs')}
        danger
        onConfirm={() => {
          if (loginAs) void acting.loginAs(loginAs, tenant.name).catch(failed)
          setLoginAs(null)
        }}
        onCancel={() => setLoginAs(null)}
      />
      {adjusting ? (
        <AdjustCredit
          tenant={tenant}
          onClose={() => setAdjusting(false)}
          onDone={() => {
            setAdjusting(false)
            void client.invalidateQueries({ queryKey: ['console', 'tenant', tenant.id] })
          }}
        />
      ) : null}
      {refunding ? (
        <RefundPayment
          payment={refunding}
          onClose={() => setRefunding(null)}
          onDone={() => {
            setRefunding(null)
            void client.invalidateQueries({ queryKey: ['console', 'tenant', tenant.id] })
          }}
        />
      ) : null}
      {assigning ? (
        <AssignPlan
          tenant={tenant}
          currentId={pkg?.id}
          currentCycle={status.data?.billing_cycle}
          onClose={() => setAssigning(false)}
          onDone={() => {
            setAssigning(false)
            void client.invalidateQueries({ queryKey: ['console', 'tenant', tenant.id] })
          }}
        />
      ) : null}
    </Drawer>
  )
}

function AssignPlan({
  tenant,
  currentId,
  currentCycle,
  onClose,
  onDone,
}: {
  tenant: Tenant
  currentId?: number
  currentCycle?: string
  onClose: () => void
  onDone: () => void
}) {
  const t = useT()
  const money = useMoney()
  const currency = useDefaultCurrency()
  const { push } = useToast()
  const errors = useApiErrors()
  const [amountError, setAmountError] = useState<string>()
  const packages = useQuery({ queryKey: ['console', 'packages'], queryFn: () => api<Package[]>('/packages', { asStaff: true }) })
  const [packageId, setPackageId] = useState(currentId ? String(currentId) : '')
  const [record, setRecord] = useState(false)
  const [rules, setRules] = useState(currentId != null)
  const [method, setMethod] = useState<'cash' | 'bank_transfer'>('bank_transfer')
  const [amount, setAmount] = useState('')
  const [busy, setBusy] = useState(false)
  // Monthly or yearly, for a monthly plan that has a yearly price.
  const [cycle, setCycle] = useState<'monthly' | 'yearly'>(currentCycle === 'yearly' ? 'yearly' : 'monthly')
  const base = packages.data?.find((pkg) => String(pkg.id) === packageId)
  const yearlyOffered = base?.billing_cycle === 'monthly' && base.yearly_price != null
  const chosen = base ? atCycle(base, yearlyOffered ? cycle : null) : undefined

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    errors.clear()
    setAmountError(undefined)
    const minor = amount.trim() ? toMinor(amount, currency) : null
    if (record && amount.trim() && minor === null) {
      setAmountError(t('inputs.amountInvalid', { example: fromMinor(150000, currency) }))
      return
    }
    setBusy(true)
    try {
      const body: Record<string, unknown> = { package_id: Number(packageId), billing_cycle: chosen?.billing_cycle, mode: rules ? 'change' : 'replace' }
      if (record) {
        body.payment_method = method
        if (minor !== null) body.amount = minor
      }
      await api(`/billing/internal/tenants/${tenant.id}/subscription`, { method: 'POST', asStaff: true, idempotent: true, body })
      push(t('form.saved'), 'success')
      onDone()
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open title={`${t('console.assignPlan')} · ${tenant.name}`} description={t('console.assignHint')} onClose={onClose}>
      <form onSubmit={submit} className="stack">
        <Field label={t('console.plan')} error={errors.field('package_id')}>
          <Select
            value={packageId}
            ariaLabel={t('console.plan')}
            onChange={setPackageId}
            options={(packages.data ?? []).map((pkg) => ({
              value: String(pkg.id),
              label: pkg.name,
              hint: pkg.price === 0 ? t('onboarding.free') : money(pkg.price),
              disabled: !pkg.is_active,
            }))}
          />
        </Field>
        {yearlyOffered ? (
          <Field label={t('console.cycle')} error={errors.field('billing_cycle')}>
            <CycleSwitch value={cycle} onChange={(next) => setCycle(next === 'yearly' ? 'yearly' : 'monthly')} packages={base ? [base] : []} />
          </Field>
        ) : null}
        <label className="check-row">
          <input type="checkbox" checked={rules} onChange={(e) => setRules(e.target.checked)} />
          {t('console.assignChange')}
        </label>
        <label className="check-row">
          <input type="checkbox" checked={record} onChange={(e) => setRecord(e.target.checked)} />
          {t('console.recordPayment')}
        </label>
        {record ? (
          <div className="form-grid-2">
            <Field label={t('console.method')} error={errors.field('payment_method')}>
              <Select
                value={method}
                ariaLabel={t('console.method')}
                onChange={setMethod}
                options={[
                  { value: 'bank_transfer', label: t('billing.method.bank_transfer') },
                  { value: 'cash', label: t('billing.method.cash') },
                ]}
              />
            </Field>
            <Field label={t('console.amount')} error={amountError ?? errors.field('amount')}>
              <MoneyInput
                currency={currency}
                ariaLabel={t('console.amount')}
                placeholder={chosen ? fromMinor(chosen.price, currency) : undefined}
                value={amount}
                onChange={setAmount}
              />
            </Field>
          </div>
        ) : null}
        {errors.box}
        <div className="modal-actions">
          <Button variant="ghost" type="button" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" type="submit" disabled={busy || !packageId}>
            {busy ? <Spinner /> : <Icon name="check" />} {t('console.assignPlan')}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

/** Adds account credit to an organization, or takes it back (never below zero). */
function AdjustCredit({ tenant, onClose, onDone }: { tenant: Tenant; onClose: () => void; onDone: () => void }) {
  const t = useT()
  const currency = useDefaultCurrency()
  const { push } = useToast()
  const errors = useApiErrors()
  const [direction, setDirection] = useState<'add' | 'remove'>('add')
  const [amount, setAmount] = useState('')
  const [reason, setReason] = useState('')
  const [amountError, setAmountError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    errors.clear()
    setAmountError(undefined)
    const minor = toMinor(amount, currency)
    if (minor === null || minor === 0) {
      setAmountError(t('inputs.amountInvalid', { example: fromMinor(50000, currency) }))
      return
    }
    setBusy(true)
    try {
      await api(`/billing/internal/tenants/${tenant.id}/credits`, {
        method: 'POST',
        asStaff: true,
        idempotent: true,
        body: { amount: direction === 'add' ? minor : -minor, reason: reason.trim() },
      })
      push(t('form.saved'), 'success')
      onDone()
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Modal open title={`${t('console.adjustCredit')} · ${tenant.name}`} description={t('console.adjustCreditHint')} onClose={onClose}>
      <form onSubmit={submit} className="stack">
        <div className="form-grid-2">
          <Field label={t('console.creditAction')}>
            <Select
              value={direction}
              ariaLabel={t('console.creditAction')}
              onChange={setDirection}
              options={[
                { value: 'add', label: t('console.creditAdd') },
                { value: 'remove', label: t('console.creditRemove') },
              ]}
            />
          </Field>
          <Field label={t('console.amount')} error={amountError ?? errors.field('amount')}>
            <MoneyInput currency={currency} ariaLabel={t('console.amount')} value={amount} onChange={setAmount} required />
          </Field>
        </div>
        <Field label={t('console.reason')} error={errors.field('reason')}>
          <input className="input" required minLength={3} maxLength={500} value={reason} onChange={(e) => setReason(e.target.value)} />
        </Field>
        {errors.box}
        <div className="modal-actions">
          <Button variant="ghost" type="button" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" type="submit" disabled={busy || !amount.trim() || reason.trim().length < 3}>
            {busy ? <Spinner /> : <Icon name="check" />} {t('console.adjustCredit')}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

/** Gives money back for a payment: as account credit, or the way it was paid. */
function RefundPayment({ payment, onClose, onDone }: { payment: Schemas['Payment']; onClose: () => void; onDone: () => void }) {
  const t = useT()
  const money = useMoney()
  const currency = useDefaultCurrency()
  const { push } = useToast()
  const errors = useApiErrors()
  const left = payment.amount - (payment.refunded_amount ?? 0)
  const [amount, setAmount] = useState('')
  const [reason, setReason] = useState('')
  const [to, setTo] = useState<'credit' | 'original'>('credit')
  const [end, setEnd] = useState(false)
  const [amountError, setAmountError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    errors.clear()
    setAmountError(undefined)
    const minor = amount.trim() ? toMinor(amount, currency) : left
    if (minor === null || minor <= 0 || minor > left) {
      setAmountError(t('console.refundLeft', { amount: money(left, payment.currency) }))
      return
    }
    setBusy(true)
    try {
      await api(`/billing/internal/payments/${payment.id}/refund`, {
        method: 'POST',
        asStaff: true,
        idempotent: true,
        body: { amount: minor, reason: reason.trim(), to, end_subscription: end },
      })
      push(t('form.saved'), 'success')
      onDone()
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Modal open title={t('console.refundTitle', { invoice: payment.invoice_number })} description={t('console.refundLeft', { amount: money(left, payment.currency) })} onClose={onClose}>
      <form onSubmit={submit} className="stack">
        <div className="form-grid-2">
          <Field label={t('console.amount')} error={amountError ?? errors.field('amount')}>
            <MoneyInput currency={currency} ariaLabel={t('console.amount')} placeholder={fromMinor(left, currency)} value={amount} onChange={setAmount} />
          </Field>
          <Field label={t('console.refundTo')}>
            <Select
              value={to}
              ariaLabel={t('console.refundTo')}
              onChange={setTo}
              options={[
                { value: 'credit', label: t('console.refundToCredit') },
                { value: 'original', label: t('console.refundToOriginal') },
              ]}
            />
          </Field>
        </div>
        <Field label={t('console.reason')} error={errors.field('reason')}>
          <input className="input" required minLength={3} maxLength={500} value={reason} onChange={(e) => setReason(e.target.value)} />
        </Field>
        <label className="check-row">
          <input type="checkbox" checked={end} onChange={(e) => setEnd(e.target.checked)} />
          {t('console.refundEnd')}
        </label>
        {errors.box}
        <div className="modal-actions">
          <Button variant="ghost" type="button" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="danger" type="submit" disabled={busy || reason.trim().length < 3}>
            {busy ? <Spinner /> : null} {t('console.refund')}
          </Button>
        </div>
      </form>
    </Modal>
  )
}
