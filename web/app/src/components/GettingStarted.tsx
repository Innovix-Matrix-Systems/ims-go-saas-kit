import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { api, type Schemas } from '../api/client'
import { can, useSession } from '../auth/session'
import { useT, type MessageKey } from '../i18n'
import { subscriptionStatusQuery } from './billing'
import { organizationQuery } from './organization'
import { Button, Card, Icon } from './ui'

interface Step {
  key: string
  label: MessageKey
  hint: MessageKey
  to: string
  done: boolean
}

const storageKey = (orgId: number) => `ims.app.getting-started.${orgId}`

function readDismissed(orgId: number | undefined): boolean {
  if (!orgId) return false
  try {
    return localStorage.getItem(storageKey(orgId)) === 'dismissed'
  } catch {
    return false
  }
}

/**
 * What is left to set up a new organization: shown to its admins on the
 * home page until every step is done or they put it away.
 */
export function GettingStarted({ members }: { members?: number }) {
  const t = useT()
  const session = useSession()
  const organization = useQuery(organizationQuery)
  const billing = can(session, 'billing.view')
  const subscription = useQuery({ ...subscriptionStatusQuery, enabled: billing })
  const twoFactor = useQuery({ queryKey: ['two-factor'], queryFn: () => api<Schemas['TwoFactorStatus']>('/two-factor') })
  const [dismissed, setDismissed] = useState(() => readDismissed(organization.data?.id))
  const org = organization.data
  if (!session || !org || session.acting || !can(session, 'organization.update')) return null
  if (dismissed || readDismissed(org.id)) return null

  const steps: Step[] = [
    { key: 'email', label: 'start.email', hint: 'start.emailHint', to: '/settings/profile', done: !!session.user.email_verified_at },
    ...(members != null ? [{ key: 'team', label: 'start.team' as MessageKey, hint: 'start.teamHint' as MessageKey, to: '/org/members', done: members > 1 }] : []),
    { key: 'twofactor', label: 'start.twoFactor', hint: 'start.twoFactorHint', to: '/settings/security', done: !!twoFactor.data?.enabled },
    ...(billing ? [{ key: 'plan', label: 'start.plan' as MessageKey, hint: 'start.planHint' as MessageKey, to: '/org/billing', done: !!subscription.data?.active }] : []),
    { key: 'brand', label: 'start.brand', hint: 'start.brandHint', to: '/org/settings', done: !!org.branding.logo_url },
  ]
  const done = steps.filter((step) => step.done).length
  if (done === steps.length) return null

  const dismiss = () => {
    try {
      localStorage.setItem(storageKey(org.id), 'dismissed')
    } catch {
      // Storage is off: it stays hidden until the page reloads.
    }
    setDismissed(true)
  }

  return (
    <Card
      title={t('start.title')}
      description={t('start.progress', { done, total: steps.length })}
      actions={
        <Button size="sm" variant="ghost" onClick={dismiss}>
          {t('start.dismiss')}
        </Button>
      }
    >
      <div className="start-meter" role="progressbar" aria-valuemin={0} aria-valuemax={steps.length} aria-valuenow={done}>
        <span className="start-meter-fill" style={{ width: `${(done / steps.length) * 100}%` }} />
      </div>
      <ol className="start-steps">
        {steps.map((step) => (
          <li key={step.key} className={step.done ? 'done' : ''}>
            <span className="start-check" aria-hidden="true">
              {step.done ? <Icon name="check" /> : null}
            </span>
            <span className="start-text">
              <span className="start-label">{t(step.label)}</span>
              <span className="start-hint">{t(step.hint)}</span>
            </span>
            {step.done ? (
              <span className="small muted">{t('start.done')}</span>
            ) : (
              <Link to={step.to} className="btn sm">
                {t('start.go')}
              </Link>
            )}
          </li>
        ))}
      </ol>
    </Card>
  )
}
