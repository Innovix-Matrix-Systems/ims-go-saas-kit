import { createFileRoute, redirect, useNavigate } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { api, ApiError, type Schemas } from '../api/client'
import { useSession } from '../auth/session'
import { Field, useApiErrors } from '../components/form'
import { TwoFactorSetup } from '../components/TwoFactorSetup'
import { Badge, Button, Card, Icon, PageHeader, Spinner } from '../components/ui'
import { intlLocale, useLocale, useT, type MessageKey } from '../i18n'
import { queryClient } from '../query'

export interface OnboardingProgress {
  completed: boolean
  completed_at: string | null
  skipped_steps: string[]
  source: 'signup' | 'staff'
  is_owner: boolean
}

/** The organization's setup progress; null when it has none (older organizations). */
export const onboardingQuery = {
  queryKey: ['onboarding'] as const,
  queryFn: async (): Promise<OnboardingProgress | null> => {
    try {
      return await api<OnboardingProgress>('/onboarding', { quiet: true })
    } catch (cause) {
      if (cause instanceof ApiError && (cause.status === 404 || cause.status === 422)) return null
      throw cause
    }
  },
  staleTime: 60_000,
}

export const Route = createFileRoute('/_app/onboarding')({
  beforeLoad: async () => {
    const progress = await queryClient.ensureQueryData(onboardingQuery)
    if (!progress || !progress.is_owner || progress.completed) throw redirect({ to: '/' })
  },
  component: OnboardingPage,
})

type Step = 'package' | 'invite' | 'two_factor' | 'done'
const steps: { key: Step; label: MessageKey }[] = [
  { key: 'package', label: 'onboarding.step.package' },
  { key: 'invite', label: 'onboarding.step.invite' },
  { key: 'two_factor', label: 'onboarding.step.twoFactor' },
  { key: 'done', label: 'onboarding.step.done' },
]

type Package = Schemas['Package']
interface Status {
  subscription?: Schemas['Subscription']
  package?: Package
  active: boolean
}

function OnboardingPage() {
  const t = useT()
  const navigate = useNavigate()
  const client = useQueryClient()
  const [step, setStep] = useState<Step>('package')
  const [skipped, setSkipped] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const index = steps.findIndex((item) => item.key === step)

  const next = (skip: boolean) => {
    if (skip && step !== 'done') setSkipped((list) => [...new Set([...list, step])])
    setStep(steps[Math.min(index + 1, steps.length - 1)].key)
  }

  const finish = async () => {
    setBusy(true)
    try {
      await api('/onboarding/complete', { method: 'POST', body: { skipped_steps: skipped } })
      await client.invalidateQueries({ queryKey: ['onboarding'] })
      await navigate({ to: '/' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <PageHeader title={t('onboarding.title')} description={t('onboarding.subtitle')} />
      <div className="wizard-steps">
        {steps.map((item, i) => (
          <span key={item.key} className={`wizard-step ${i === index ? 'active' : i < index ? 'done' : ''}`}>
            {i + 1}. {t(item.label)}
          </span>
        ))}
      </div>
      {step === 'package' ? <PackageStep onNext={next} /> : null}
      {step === 'invite' ? <InviteStep onNext={next} /> : null}
      {step === 'two_factor' ? <TwoFactorStep onNext={next} /> : null}
      {step === 'done' ? (
        <Card title={t('onboarding.step.done')}>
          <div className="stack">
            <div className="success-box">{t('onboarding.doneBody')}</div>
            <div>
              <Button variant="primary" icon="home" disabled={busy} onClick={() => void finish()}>
                {t('onboarding.finish')}
              </Button>
            </div>
          </div>
        </Card>
      ) : null}
    </>
  )
}

function StepActions({ onNext, canContinue = true }: { onNext: (skip: boolean) => void; canContinue?: boolean }) {
  const t = useT()
  return (
    <div className="modal-actions">
      <Button variant="ghost" onClick={() => onNext(true)}>
        {t('onboarding.skip')}
      </Button>
      <Button variant="primary" disabled={!canContinue} onClick={() => onNext(false)}>
        {t('onboarding.next')}
      </Button>
    </div>
  )
}

function useMoney() {
  const locale = useLocale()
  return (minor: number, currency = 'BDT') =>
    new Intl.NumberFormat(intlLocale(locale), { style: 'currency', currency, maximumFractionDigits: 2 }).format(minor / 100)
}

function PackageStep({ onNext }: { onNext: (skip: boolean) => void }) {
  const t = useT()
  const money = useMoney()
  const errors = useApiErrors()
  const packages = useQuery({ queryKey: ['public-packages'], queryFn: () => api<Package[]>('/public/packages', { token: null }) })
  const status = useQuery({ queryKey: ['subscription-status'], queryFn: () => api<Status>('/billing/subscription/status') })
  const [chosen, setChosen] = useState<Package | null>(null)
  const [reference, setReference] = useState('')
  const [busy, setBusy] = useState(false)
  const [pending, setPending] = useState(false)
  const currentId = status.data?.package?.id
  const cycle = (pkg: Package) =>
    pkg.billing_cycle === 'monthly' ? t('onboarding.perMonth') : pkg.billing_cycle === 'yearly' ? t('onboarding.perYear') : t('onboarding.oneTime')

  const pay = async (event: FormEvent) => {
    event.preventDefault()
    if (!chosen) return
    setBusy(true)
    errors.clear()
    try {
      await api('/billing/subscribe', {
        method: 'POST',
        idempotent: true,
        body: { package_id: chosen.id, payment_method: 'bank_transfer', payment_data: { reference_number: reference.trim() } },
      })
      setPending(true)
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  if (!packages.data || !status.data) {
    return (
      <Card title={t('onboarding.step.package')}>
        <Spinner />
      </Card>
    )
  }

  return (
    <Card title={t('onboarding.step.package')}>
      <div className="stack">
        <div className="plan-grid">
          {packages.data.map((pkg) => {
            const current = pkg.id === currentId
            return (
              <div key={pkg.id} className={`plan-card ${current ? 'current' : ''}`}>
                <div className="flex g-2 align-center">
                  <strong>{pkg.name}</strong>
                  {current ? <Badge tone="info">{t('onboarding.currentPlan')}</Badge> : null}
                </div>
                <div className="plan-price">
                  {pkg.price === 0 ? t('onboarding.free') : money(pkg.price)}{' '}
                  {pkg.price > 0 ? <span className="small muted">{cycle(pkg)}</span> : null}
                </div>
                {pkg.description ? <p className="small muted">{pkg.description}</p> : null}
                <ul className="plan-features">
                  {pkg.features
                    .filter((feature) => feature.is_enabled)
                    .map((feature) => (
                      <li key={feature.feature_key}>
                        {feature.feature_key}
                        {feature.limit_value > 0 ? `: ${feature.limit_value}` : feature.limit_value === -1 ? ': ∞' : ''}
                      </li>
                    ))}
                </ul>
                <Button
                  variant={current ? 'ghost' : 'default'}
                  disabled={current || pkg.price === 0}
                  onClick={() => {
                    setChosen(pkg)
                    setPending(false)
                  }}
                >
                  {current ? t('onboarding.chosen') : t('onboarding.choose')}
                </Button>
              </div>
            )
          })}
        </div>
        {chosen && chosen.price > 0 ? (
          pending ? (
            <div className="success-box">{t('onboarding.paymentPending')}</div>
          ) : (
            <form onSubmit={pay} className="stack form-narrow">
              <h3 className="card-title">{t('onboarding.payTitle')}</h3>
              <p className="small muted">{t('onboarding.payBody', { amount: money(chosen.price) })}</p>
              <Field label={t('onboarding.reference')} error={errors.field('payment_data')}>
                <input className="input mono" required value={reference} onChange={(e) => setReference(e.target.value)} />
              </Field>
              {errors.box}
              <div>
                <button type="submit" className="btn primary" disabled={busy || !reference.trim()}>
                  {busy ? <Spinner /> : <Icon name="check" />} {t('onboarding.submitPayment')}
                </button>
              </div>
            </form>
          )
        ) : null}
        <StepActions onNext={onNext} />
      </div>
    </Card>
  )
}

function InviteStep({ onNext }: { onNext: (skip: boolean) => void }) {
  const t = useT()
  const session = useSession()
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  const [sent, setSent] = useState(0)
  const [failures, setFailures] = useState<string[]>([])
  const emails = [...new Set(text.split(/[\s,;]+/).map((value) => value.trim()).filter((value) => value.includes('@')))]

  const send = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    setFailures([])
    let ok = 0
    const failed: string[] = []
    for (const email of emails) {
      try {
        await api('/user', {
          method: 'POST',
          idempotent: true,
          body: {
            name: email.split('@')[0],
            email,
            send_invitation: true,
            branch_id: session?.user.active_branch_id ?? session?.user.branch_id ?? undefined,
          },
        })
        ok++
      } catch (cause) {
        failed.push(t('onboarding.inviteFailed', { email, reason: cause instanceof Error ? cause.message : String(cause) }))
      }
    }
    setSent((value) => value + ok)
    setFailures(failed)
    if (failed.length === 0) setText('')
    setBusy(false)
  }

  return (
    <Card title={t('onboarding.step.invite')} description={t('onboarding.inviteHint')}>
      <form onSubmit={send} className="stack form-narrow">
        <Field label={t('onboarding.inviteEmails')}>
          <textarea className="input" rows={5} value={text} onChange={(e) => setText(e.target.value)} placeholder="name@company.com" />
        </Field>
        {sent > 0 ? <div className="success-box">{t('onboarding.invited', { count: sent })}</div> : null}
        {failures.map((message) => (
          <div key={message} className="small danger-text">
            {message}
          </div>
        ))}
        <div>
          <button type="submit" className="btn" disabled={busy || emails.length === 0}>
            {busy ? <Spinner /> : <Icon name="mail" />} {t('onboarding.sendInvites')}
          </button>
        </div>
        <StepActions onNext={onNext} />
      </form>
    </Card>
  )
}

function TwoFactorStep({ onNext }: { onNext: (skip: boolean) => void }) {
  const t = useT()
  const status = useQuery({ queryKey: ['two-factor'], queryFn: () => api<Schemas['TwoFactorStatus']>('/two-factor') })
  const client = useQueryClient()
  return (
    <Card title={t('onboarding.step.twoFactor')} description={t('onboarding.twoFactorHint')}>
      {!status.data ? (
        <Spinner />
      ) : status.data.enabled ? (
        <div className="stack">
          <div className="success-box">{t('onboarding.twoFactorEnabled')}</div>
          <StepActions onNext={onNext} />
        </div>
      ) : (
        <div className="stack form-narrow">
          <TwoFactorSetup onDone={() => void client.invalidateQueries({ queryKey: ['two-factor'] })} />
          <StepActions onNext={onNext} canContinue={false} />
        </div>
      )}
    </Card>
  )
}
