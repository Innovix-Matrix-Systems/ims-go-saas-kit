import { useQuery } from '@tanstack/react-query'
import { useState, type FormEvent, type ReactNode } from 'react'
import { api, type Schemas } from '../api/client'
import { intlLocale, useLocale, useT, type MessageKey } from '../i18n'
import { currenciesQuery, findCurrency, formatMoney, type Currency } from '../money'
import { Field, useApiErrors } from './form'
import { Badge, Icon, Spinner } from './ui'

export type Package = Schemas['Package']
export type SubscriptionStatus = Schemas['SubscriptionStatusEnvelope']['data']

export const publicPackagesQuery = { queryKey: ['public-packages'], queryFn: () => api<Package[]>('/public/packages', { token: null }) }
export const subscriptionStatusQuery = { queryKey: ['subscription-status'], queryFn: () => api<SubscriptionStatus>('/billing/subscription/status') }

/**
 * Writes minor units the way their currency says, in the app's language's digits. Without a code
 * it is the default currency (plan prices); a payment passes its own.
 */
export function useMoney() {
  const locale = useLocale()
  const { data } = useQuery(currenciesQuery)
  return (minor: number, code?: string | null, options?: { compact?: boolean }) => formatMoney(minor, findCurrency(data, code), locale, options)
}

/** The currency of plan prices and new payments. */
export function useDefaultCurrency(): Currency {
  const { data } = useQuery(currenciesQuery)
  return findCurrency(data)
}

const cycleLabels: Record<Package['billing_cycle'], MessageKey> = {
  monthly: 'onboarding.perMonth',
  yearly: 'onboarding.perYear',
  one_time: 'onboarding.oneTime',
}

/** "$300.00 / month", or "Free". */
export function usePrice() {
  const t = useT()
  const money = useMoney()
  return (pkg: Package) => (pkg.price === 0 ? t('onboarding.free') : `${money(pkg.price)} ${t(cycleLabels[pkg.billing_cycle])}`)
}

// Readable names for plan features; unknown keys fall back to the key.
const featureLabels: Record<string, MessageKey> = {
  'users.max': 'billing.feature.users',
  'branches.max': 'billing.feature.branches',
  'storage.mb': 'billing.feature.storage',
  'api.access': 'billing.feature.api',
  'audit.log': 'billing.feature.audit',
  'custom.branding': 'billing.feature.branding',
  'sms.notifications': 'billing.feature.sms',
  'reports.basic': 'billing.feature.reportsBasic',
  'reports.advanced': 'billing.feature.reportsAdvanced',
  'reports.export': 'billing.feature.reportsExport',
}

const verbs = new Set(['manage', 'view', 'export', 'create', 'edit', 'delete', 'import', 'send'])

/** A feature key the app has no label for, made readable: "patients.manage" → "Manage patients". */
export function humanizeFeature(key: string): string {
  const words = key.split(/[._-]+/).filter(Boolean)
  if (words.length > 1 && verbs.has(words[words.length - 1])) words.unshift(words.pop()!)
  const text = words.join(' ')
  return text.charAt(0).toLocaleUpperCase() + text.slice(1)
}

/** "50 members", "Unlimited branches", "1 GB storage", "API and webhooks". */
export function useFeatureText() {
  const t = useT()
  return (key: string, limit: number) => {
    const label = featureLabels[key] ? t(featureLabels[key]) : humanizeFeature(key)
    if (key === 'storage.mb') return limit === -1 ? `${t('billing.unlimited')} ${label}` : `${formatMB(limit)} ${label}`
    if (key.endsWith('.max')) return limit === -1 ? `${t('billing.unlimited')} ${label}` : `${limit} ${label}`
    return label
  }
}

/** A feature's name on its own, as a label ("Members", not "members"). */
export function featureName(t: (key: MessageKey) => string, key: string): string {
  const name = featureLabels[key] ? t(featureLabels[key]) : humanizeFeature(key)
  return name.charAt(0).toLocaleUpperCase() + name.slice(1)
}

export function formatMB(mb: number): string {
  return mb >= 1024 && mb % 1024 === 0 ? `${mb / 1024} GB` : `${mb} MB`
}

/** The platform's bank details for transfers (the billing.payment_instructions setting). */
export function usePaymentInstructions() {
  return useQuery({
    queryKey: ['settings', 'billing'],
    queryFn: () => api<Schemas['SettingsGroupSection'][]>('/settings', { query: { group: 'billing' } }),
    select: (sections) => {
      const value = sections.flatMap((section) => section.settings).find((item) => item.key === 'billing.payment_instructions')?.value
      return typeof value === 'string' ? value.trim() : ''
    },
    staleTime: 300_000,
  })
}

/**
 * Pays for a plan by bank transfer: shows where to send the money and takes
 * the transfer reference. The plan starts once staff confirm the payment.
 */
export function BankTransferForm({ pkg, amount, onSubmitted }: { pkg: Package; amount?: number; onSubmitted: () => void }) {
  const t = useT()
  const money = useMoney()
  const errors = useApiErrors()
  const instructions = usePaymentInstructions()
  const [reference, setReference] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    errors.clear()
    try {
      await api('/billing/subscribe', {
        method: 'POST',
        idempotent: true,
        body: { package_id: pkg.id, payment_method: 'bank_transfer', payment_data: { reference_number: reference.trim() } },
      })
      onSubmitted()
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit} className="stack">
      <p className="small muted">{t('onboarding.payBody', { amount: money(amount ?? pkg.price) })}</p>
      {instructions.data ? (
        <div className="pay-instructions">
          <div className="field-label">{t('billing.payTo')}</div>
          <div className="pay-instructions-text">{instructions.data}</div>
        </div>
      ) : null}
      <Field label={t('onboarding.reference')} hint={t('billing.referenceHint')} error={errors.field('payment_data')}>
        <input className="input mono" required autoComplete="off" value={reference} onChange={(e) => setReference(e.target.value)} />
      </Field>
      {errors.box}
      <div>
        <button type="submit" className="btn primary" disabled={busy || !reference.trim()}>
          {busy ? <Spinner /> : <Icon name="check" />} {t('onboarding.submitPayment')}
        </button>
      </div>
    </form>
  )
}

/** How many features a plan card lists before "Show more". */
const FEATURES_SHOWN = 6

/**
 * One plan to choose: its name, price and cycle, what it includes (the
 * first few, the rest behind "Show more") and the action, which stays at the
 * bottom so cards side by side line up.
 */
export function PricingCard({ pkg, current, chosen, action }: { pkg: Package; current?: boolean; chosen?: boolean; action?: ReactNode }) {
  const t = useT()
  const money = useMoney()
  const featureText = useFeatureText()
  const [expanded, setExpanded] = useState(false)
  const features = pkg.features.filter((feature) => feature.is_enabled)
  const shown = expanded ? features : features.slice(0, FEATURES_SHOWN)
  const hidden = features.length - shown.length
  return (
    <div className={`plan-card ${current ? 'current' : ''} ${chosen ? 'chosen' : ''}`}>
      <div className="plan-head">
        <span className="plan-title">{pkg.name}</span>
        {current ? <Badge tone="accent">{t('onboarding.currentPlan')}</Badge> : null}
      </div>
      <div className="plan-amount">
        {pkg.price === 0 ? (
          <span className="plan-figure">{t('onboarding.free')}</span>
        ) : (
          <>
            <span className="plan-figure">{money(pkg.price)}</span>
            <span className="plan-cycle">{t(cycleLabels[pkg.billing_cycle])}</span>
          </>
        )}
      </div>
      {pkg.description ? <p className="plan-description">{pkg.description}</p> : null}
      <ul className="plan-includes">
        {shown.map((feature) => (
          <li key={feature.feature_key}>
            <Icon name="check" />
            <span>{featureText(feature.feature_key, feature.limit_value)}</span>
          </li>
        ))}
      </ul>
      {hidden > 0 || expanded ? (
        <button type="button" className="link-btn plan-more" onClick={() => setExpanded((value) => !value)} aria-expanded={expanded}>
          {expanded ? t('billing.showFewer') : t('billing.showMore', { count: hidden })}
        </button>
      ) : null}
      {action ? <div className="plan-foot">{action}</div> : null}
    </div>
  )
}

export type Change = Schemas['Change']
type InvoiceLine = Schemas['InvoiceLine']

/** A date the way the reader writes it ("25 Dec 2026"). */
export function useShortDate() {
  const locale = useLocale()
  return (value?: string | null) => (value ? new Intl.DateTimeFormat(intlLocale(locale), { dateStyle: 'medium' }).format(new Date(value)) : '—')
}

/** One invoice line in words: the plan, unused days credited, account credit. */
export function useLineText() {
  const t = useT()
  return (line: InvoiceLine) => {
    switch (line.type) {
      case 'unused_time':
        return t('billing.line.unused', { name: line.package_name ?? '', count: line.days ?? 0 })
      case 'account_credit':
        return t('billing.line.credit')
      default:
        return line.package_name ?? ''
    }
  }
}

/** The lines of a quote or an invoice, with the total. */
export function ChangeLines({ lines, total, currency, totalLabel }: { lines: InvoiceLine[]; total: number; currency?: string; totalLabel: string }) {
  const money = useMoney()
  const lineText = useLineText()
  return (
    <dl className="change-lines">
      {lines.map((line, index) => (
        <div key={index} className={`change-line ${line.amount < 0 ? 'credit' : ''}`}>
          <dt>{lineText(line)}</dt>
          <dd>{money(line.amount, currency)}</dd>
        </div>
      ))}
      <div className="change-line total">
        <dt>{totalLabel}</dt>
        <dd>{money(total, currency)}</dd>
      </div>
    </dl>
  )
}

/**
 * Moving to another plan (or renewing): asks the API what it costs and when
 * it starts, says so, and takes the payment when there is one. Upgrades start
 * when paid with the unused days credited; downgrades and renewals start when
 * the current period ends.
 */
export function PlanChange({ pkg, onDone }: { pkg: Package; onDone: (change: Change) => void }) {
  const t = useT()
  const money = useMoney()
  const date = useShortDate()
  const errors = useApiErrors()
  const [busy, setBusy] = useState(false)
  const preview = useQuery({
    queryKey: ['billing', 'change-preview', pkg.id],
    queryFn: () => api<Change>('/billing/change-preview', { query: { package_id: pkg.id } }),
    staleTime: 0,
    gcTime: 0,
  })

  if (preview.isLoading) {
    return (
      <div className="flex">
        <Spinner /> <span className="muted">{t('billing.change.pricing')}</span>
      </div>
    )
  }
  if (preview.error || !preview.data) {
    return (
      <div className="error-box" role="alert">
        <Icon name="alert" />
        <span className="error-box-text">{preview.error instanceof Error ? preview.error.message : t('billing.change.failed')}</span>
      </div>
    )
  }
  const change = preview.data
  const current = change.current_package?.name ?? ''
  const currentEnd = date(change.current_period_end)

  // Nothing to pay now, or the account credit pays it: one confirm button.
  const confirm = async () => {
    setBusy(true)
    errors.clear()
    try {
      await api('/billing/subscribe', { method: 'POST', idempotent: true, body: { package_id: pkg.id } })
      onDone(change)
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  let summary: string
  if (change.kind === 'downgrade') {
    summary =
      change.due_at_renewal > 0
        ? t('billing.change.downgrade', { current, date: currentEnd, name: pkg.name, amount: money(change.due_at_renewal) })
        : t('billing.change.downgradeFree', { current, date: currentEnd, name: pkg.name })
  } else if (change.effective === 'period_end') {
    summary = t('billing.change.startsAt', { name: pkg.name, date: date(change.period_start) })
  } else if (!change.requires_payment) {
    summary = t('billing.change.freeNow', { name: pkg.name })
  } else if (change.amount_due === 0) {
    summary = t('billing.change.covered', { name: pkg.name })
  } else {
    summary = t('billing.change.startsNow', { name: pkg.name })
  }

  return (
    <div className="stack">
      <p>{summary}</p>
      {change.requires_payment ? <ChangeLines lines={change.lines} total={change.amount_due} currency={change.currency} totalLabel={t('billing.change.total')} /> : null}
      {change.surplus > 0 ? <p className="small muted">{t('billing.change.surplus', { amount: money(change.surplus, change.currency) })}</p> : null}
      {change.over_limits.length > 0 ? (
        <div className="banner">
          <span className="status-dot warn" />
          <span className="fill">
            {t('billing.change.overLimits', {
              name: pkg.name,
              items: change.over_limits.map((item) => `${featureName(t, item.feature_key)} ${item.used}/${item.limit}`).join(', '),
            })}
          </span>
        </div>
      ) : null}
      {(change.overrides_dropped ?? 0) > 0 ? <p className="small muted">{t('billing.change.overrides', { name: pkg.name })}</p> : null}
      {change.requires_payment && change.amount_due > 0 ? (
        <BankTransferForm pkg={pkg} amount={change.amount_due} onSubmitted={() => onDone(change)} />
      ) : (
        <>
          {errors.box}
          <div>
            <button type="button" className="btn primary" disabled={busy} onClick={() => void confirm()}>
              {busy ? <Spinner /> : <Icon name="check" />} {change.kind === 'downgrade' ? t('billing.change.confirmLater') : t('billing.change.confirm')}
            </button>
          </div>
        </>
      )}
    </div>
  )
}
