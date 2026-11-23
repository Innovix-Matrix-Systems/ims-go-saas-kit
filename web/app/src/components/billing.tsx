import { useQuery } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { api, type Schemas } from '../api/client'
import { intlLocale, useLocale, useT, type MessageKey } from '../i18n'
import { Field, useApiErrors } from './form'
import { Icon, Spinner } from './ui'

export type Package = Schemas['Package']
export type SubscriptionStatus = Schemas['SubscriptionStatusEnvelope']['data']

export const publicPackagesQuery = { queryKey: ['public-packages'], queryFn: () => api<Package[]>('/public/packages', { token: null }) }
export const subscriptionStatusQuery = { queryKey: ['subscription-status'], queryFn: () => api<SubscriptionStatus>('/billing/subscription/status') }

/** Formats minor units (paisa) as money in the app's language. */
export function useMoney() {
  const locale = useLocale()
  return (minor: number, currency = 'BDT') =>
    new Intl.NumberFormat(intlLocale(locale), { style: 'currency', currency, currencyDisplay: 'narrowSymbol', maximumFractionDigits: 2 }).format(minor / 100)
}

const cycleLabels: Record<Package['billing_cycle'], MessageKey> = {
  monthly: 'onboarding.perMonth',
  yearly: 'onboarding.perYear',
  one_time: 'onboarding.oneTime',
}

/** "৳300.00 / month", or "Free". */
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

/** "50 members", "Unlimited branches", "1 GB storage", "API and webhooks". */
export function useFeatureText() {
  const t = useT()
  return (key: string, limit: number) => {
    const label = featureLabels[key] ? t(featureLabels[key]) : key
    if (key === 'storage.mb') return limit === -1 ? `${t('billing.unlimited')} ${label}` : `${formatMB(limit)} ${label}`
    if (key.endsWith('.max')) return limit === -1 ? `${t('billing.unlimited')} ${label}` : `${limit} ${label}`
    return label
  }
}

/** A feature's name on its own, as a label ("Members", not "members"). */
export function featureName(t: (key: MessageKey) => string, key: string): string {
  const name = featureLabels[key] ? t(featureLabels[key]) : key
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
export function BankTransferForm({ pkg, onSubmitted }: { pkg: Package; onSubmitted: () => void }) {
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
      <p className="small muted">{t('onboarding.payBody', { amount: money(pkg.price) })}</p>
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
