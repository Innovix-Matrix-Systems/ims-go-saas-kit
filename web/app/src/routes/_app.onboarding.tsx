import { createFileRoute, redirect, useNavigate } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { api, ApiError, type Schemas } from '../api/client'
import { useSession } from '../auth/session'
import { atCycle, BankTransferForm, CycleSwitch, offersYearly, publicPackagesQuery, subscriptionStatusQuery, type Cycle, type Package, PricingCard } from '../components/billing'
import { Field } from '../components/form'
import { TwoFactorSetup } from '../components/TwoFactorSetup'
import { Button, Card, Icon, PageHeader, Spinner } from '../components/ui'
import { useT, type MessageKey } from '../i18n'
import { queryClient } from '../query'
import { CardsSkeleton, FormSkeleton } from '../components/Skeleton'

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

function PackageStep({ onNext }: { onNext: (skip: boolean) => void }) {
  const t = useT()
  const packages = useQuery(publicPackagesQuery)
  const status = useQuery(subscriptionStatusQuery)
  const [chosen, setChosen] = useState<Package | null>(null)
  const [pending, setPending] = useState(false)
  const [cycle, setCycle] = useState<Cycle>('monthly')
  const currentId = status.data?.package?.id

  if (!packages.data || !status.data) {
    return (
      <Card title={t('onboarding.step.package')}>
        <CardsSkeleton count={3} />
      </Card>
    )
  }

  return (
    <Card title={t('onboarding.step.package')}>
      <div className="stack">
        {offersYearly(packages.data) ? (
          <CycleSwitch
            value={cycle}
            onChange={(next) => {
              setCycle(next)
              setChosen(null)
            }}
            packages={packages.data}
          />
        ) : null}
        <div className="plan-grid">
          {packages.data.map((base) => {
            const pkg = atCycle(base, cycle)
            const current = pkg.id === currentId
            return (
              <PricingCard
                key={pkg.id}
                pkg={pkg}
                current={current}
                chosen={chosen?.id === pkg.id}
                action={
                  <Button
                    variant={current ? 'ghost' : chosen?.id === pkg.id ? 'primary' : 'default'}
                    disabled={current || pkg.price === 0}
                    onClick={() => {
                      setChosen(pkg)
                      setPending(false)
                    }}
                  >
                    {current ? t('onboarding.chosen') : t('onboarding.choose')}
                  </Button>
                }
              />
            )
          })}
        </div>
        {chosen && chosen.price > 0 ? (
          pending ? (
            <div className="success-box">{t('onboarding.paymentPending')}</div>
          ) : (
            <div className="stack form-narrow">
              <h3 className="card-title">{t('onboarding.payTitle')}</h3>
              <BankTransferForm key={`${chosen.id}-${chosen.billing_cycle}`} pkg={chosen} onSubmitted={() => setPending(true)} />
            </div>
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
        <FormSkeleton fields={2} />
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
