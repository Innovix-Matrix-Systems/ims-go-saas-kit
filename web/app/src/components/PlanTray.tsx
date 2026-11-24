import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useCallback, useState } from 'react'
import { api, type Schemas } from '../api/client'
import { can, useSession } from '../auth/session'
import { intlLocale, useLocale, useT, type MessageKey } from '../i18n'
import { featureName, formatMB, subscriptionStatusQuery } from './billing'
import { MeterList } from './MeterList'
import { useDismiss } from './inputs'
import { Badge, Icon } from './ui'
import { inOrganization } from './WorkspaceMenu'

/** What the organization uses of its plan, shared with the billing page and dashboards. */
export const usageQuery = { queryKey: ['billing', 'usage'], queryFn: () => api<Schemas['BillingUsage']>('/billing/usage'), staleTime: 60_000 }

/**
 * The top bar's plan: a peek at the organization's plan and what it uses of
 * each limit, a dot when something is nearly full, and the way to billing.
 */
export function PlanTray() {
  const t = useT()
  const locale = intlLocale(useLocale())
  const session = useSession()
  const [open, setOpen] = useState(false)
  const close = useCallback(() => setOpen(false), [])
  const ref = useDismiss(open, close)
  const allowed = inOrganization(session) && can(session, 'billing.view')
  const status = useQuery({ ...subscriptionStatusQuery, enabled: allowed })
  const usage = useQuery({ ...usageQuery, enabled: allowed })
  if (!allowed) return null

  const limits = usage.data?.limits ?? []
  const nearlyFull = limits.some((limit) => limit.limit > 0 && limit.used / limit.limit >= 0.8)
  const pkg = status.data?.package
  const sub = status.data?.subscription

  return (
    <div className="appearance" ref={ref}>
      <button
        type="button"
        className="icon-btn jobs-trigger"
        title={t('planTray.title')}
        aria-label={nearlyFull ? `${t('planTray.title')} (${t('planTray.nearlyFull')})` : t('planTray.title')}
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
      >
        <Icon name="package" />
        {nearlyFull ? <span className="jobs-dot warn" aria-hidden="true" /> : null}
      </button>
      {open ? (
        <div className="ui-popover align-right plan-tray" role="dialog" aria-label={t('planTray.title')}>
          <div className="jobs-menu-head">
            <span className="workspace-text">
              <span className="workspace-name">{pkg?.name ?? t('dashboard.noPlan')}</span>
              <span className="workspace-sub">
                {sub?.expires_at
                  ? t('dashboard.renews', { date: new Intl.DateTimeFormat(locale, { dateStyle: 'medium' }).format(new Date(sub.expires_at)) })
                  : t('planTray.title')}
              </span>
            </span>
            {sub ? <Badge tone={status.data?.active ? 'ok' : 'warn'}>{t(`dashboard.subscription.${sub.status}` as MessageKey)}</Badge> : null}
          </div>
          <div className="plan-tray-body">
            {limits.length > 0 ? (
              <MeterList
                items={limits.map((limit) => ({
                  key: limit.feature_key,
                  label: featureName(t, limit.feature_key),
                  used: limit.used,
                  limit: limit.limit,
                  format: limit.feature_key === 'storage.mb' ? formatMB : undefined,
                }))}
              />
            ) : (
              <p className="small muted">{usage.isLoading ? '…' : t('dashboard.noLimits')}</p>
            )}
          </div>
          <Link to="/org/billing" className="plan-tray-foot" onClick={close}>
            {t('planTray.manage')}
            <Icon name="chevron" />
          </Link>
        </div>
      ) : null}
    </div>
  )
}
