import { Link } from '@tanstack/react-router'
import type { Schemas } from '../api/client'
import { intlLocale, useLocale, useT, type MessageKey } from '../i18n'
import { featureName, formatMB } from './billing'
import { MeterList } from './charts'
import { Badge, Card } from './ui'

type Usage = Schemas['BillingUsage']

/** The organization's plan, its status and what it uses of each limit. */
export function PlanCard({ usage, status }: { usage?: Usage; status?: Schemas['SubscriptionStatusEnvelope']['data'] }) {
  const t = useT()
  const locale = useLocale()
  const pkg = status?.package
  const sub = status?.subscription
  const items = (usage?.limits ?? []).map((limit) => ({
    key: limit.feature_key,
    label: featureName(t, limit.feature_key),
    used: limit.used,
    limit: limit.limit,
    format: limit.feature_key === 'storage.mb' ? formatMB : undefined,
  }))
  return (
    <Card
      title={t('dashboard.plan')}
      description={pkg ? pkg.name : t('dashboard.noPlan')}
      actions={
        <Link to="/org/billing" className="link-btn">
          {t('dashboard.manage')}
        </Link>
      }
    >
      <div className="plan-summary">
        {sub ? (
          <Badge tone={status?.active ? 'ok' : 'warn'}>{t(`dashboard.subscription.${sub.status}` as MessageKey)}</Badge>
        ) : (
          <Badge tone="neutral">{t('dashboard.noPlan')}</Badge>
        )}
        {sub?.expires_at ? <span className="small muted">{t('dashboard.renews', { date: new Intl.DateTimeFormat(intlLocale(locale), { dateStyle: 'medium' }).format(new Date(sub.expires_at)) })}</span> : null}
      </div>
      {items.length > 0 ? <MeterList items={items} /> : <p className="small muted">{t('dashboard.noLimits')}</p>}
    </Card>
  )
}
