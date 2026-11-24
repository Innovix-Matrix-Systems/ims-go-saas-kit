import { Link } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type ReactNode } from 'react'
import { can, useSession } from '../auth/session'
import { intlLocale, useLocale, useT } from '../i18n'
import { subscriptionStatusQuery } from './billing'
import { feedKey, fetchFeed, markRead } from './notifications'
import { organizationQuery } from './organization'
import { Icon, type IconName } from './ui'
import { inOrganization } from './WorkspaceMenu'

// Banners across the top of the app for what must not be missed: the
// organization's plan running out, and staff announcements marked warning
// or critical. Each can be put away; a plan warning comes back the next day.

/** How soon a plan's end is worth a warning, in days. */
const WARN_DAYS = 7

type Tone = 'info' | 'warn' | 'danger'

function Banner({
  tone,
  icon,
  title,
  body,
  action,
  onDismiss,
  dismissLabel,
}: {
  tone: Tone
  icon: IconName
  title: string
  body?: string | null
  action?: ReactNode
  onDismiss?: () => void
  dismissLabel: string
}) {
  return (
    <div className={`system-banner ${tone}`} role={tone === 'danger' ? 'alert' : 'status'}>
      <Icon name={icon} />
      <span className="system-banner-text">
        <strong>{title}</strong>
        {body ? <span className="system-banner-body">{body}</span> : null}
      </span>
      {action}
      {onDismiss ? (
        <button type="button" className="system-banner-close" onClick={onDismiss} aria-label={dismissLabel} title={dismissLabel}>
          <Icon name="x" />
        </button>
      ) : null}
    </div>
  )
}

const today = () => new Date().toISOString().slice(0, 10)

function useDismissedToday(key: string | null) {
  const read = () => {
    if (!key) return false
    try {
      return localStorage.getItem(key) === today()
    } catch {
      return false
    }
  }
  const [dismissed, setDismissed] = useState(read)
  const dismiss = () => {
    if (!key) return
    try {
      localStorage.setItem(key, today())
    } catch {
      // Storage is off: hidden until the page reloads.
    }
    setDismissed(true)
  }
  return [dismissed || read(), dismiss] as const
}

/**
 * The organization's plan when it needs attention: ending within a week,
 * cancelled, overdue, ended, waiting for a payment, or missing. Shown to
 * those who can see billing, with the way to fix it.
 */
export function PlanBanner() {
  const t = useT()
  const locale = intlLocale(useLocale())
  const session = useSession()
  const allowed = inOrganization(session) && can(session, 'billing.view')
  const status = useQuery({ ...subscriptionStatusQuery, enabled: allowed })
  const organization = useQuery({ ...organizationQuery, enabled: allowed })
  const data = status.data
  const sub = data?.subscription
  const plan = data?.package?.name ?? ''

  let notice: { kind: string; tone: Tone; title: string; body?: string } | null = null
  if (allowed && data) {
    const expires = sub?.expires_at ? new Date(sub.expires_at) : null
    const daysLeft = expires ? Math.ceil((expires.getTime() - Date.now()) / 86_400_000) : null
    const date = expires ? new Intl.DateTimeFormat(locale, { dateStyle: 'medium' }).format(expires) : ''
    const when = daysLeft === null ? '' : daysLeft <= 0 ? t('planBanner.today') : daysLeft === 1 ? t('planBanner.tomorrow') : t('planBanner.inDays', { days: daysLeft })
    if (!sub) {
      notice = { kind: 'none', tone: 'warn', title: t('planBanner.none'), body: t('planBanner.noneBody') }
    } else if (sub.status === 'expired' || (!data.active && sub.status !== 'pending' && sub.status !== 'past_due')) {
      notice = { kind: 'ended', tone: 'danger', title: t('planBanner.ended', { plan }), body: t('planBanner.endedBody') }
    } else if (sub.status === 'past_due') {
      notice = { kind: 'past_due', tone: 'danger', title: t('planBanner.pastDue', { plan }), body: t('planBanner.pastDueBody') }
    } else if (sub.status === 'pending') {
      notice = { kind: 'pending', tone: 'info', title: t('planBanner.pending', { plan }), body: t('planBanner.pendingBody') }
    } else if (sub.status === 'cancelled' && expires) {
      notice = { kind: 'cancelled', tone: 'warn', title: t('planBanner.cancelled', { plan, date }), body: t('planBanner.renewBody') }
    } else if (daysLeft !== null && daysLeft <= WARN_DAYS) {
      notice =
        sub.status === 'trial'
          ? { kind: 'trial', tone: 'warn', title: t('planBanner.trialEnds', { when }), body: t('planBanner.trialBody') }
          : { kind: 'ending', tone: 'warn', title: t('planBanner.ends', { plan, when }), body: t('planBanner.renewBody') }
    }
  }

  const key = notice && organization.data ? `ims.app.banner.plan.${organization.data.id}.${notice.kind}` : null
  const [dismissed, dismiss] = useDismissedToday(key)
  if (!notice || dismissed) return null

  return (
    <Banner
      tone={notice.tone}
      icon={notice.tone === 'info' ? 'clock' : 'alert'}
      title={notice.title}
      body={notice.body}
      action={
        <Link to="/org/billing" className="btn sm">
          {notice.kind === 'pending' ? t('planBanner.viewBilling') : t('planBanner.choose')}
        </Link>
      }
      onDismiss={dismiss}
      dismissLabel={t('planBanner.hide')}
    />
  )
}

/**
 * The newest unread staff announcement marked warning or critical, until it
 * is read here or in the notifications. Informational ones stay in the bell.
 */
export function AnnouncementBanner() {
  const t = useT()
  const session = useSession()
  const client = useQueryClient()
  const feed = useQuery({
    queryKey: [...feedKey, 'banner'],
    queryFn: () => fetchFeed({ unreadOnly: true, limit: 20 }),
    enabled: !!session,
    refetchInterval: 60_000,
    refetchIntervalInBackground: false,
  })
  const announcements = (feed.data?.data ?? []).filter((item) => item.event_type === 'platform.announcement' && item.priority !== 'INFO' && !item.is_read)
  const [busy, setBusy] = useState(false)
  if (announcements.length === 0) return null
  const current = announcements[0]

  const dismiss = async () => {
    setBusy(true)
    try {
      await markRead(current.id)
      await client.invalidateQueries({ queryKey: feedKey })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Banner
      tone={current.priority === 'CRITICAL' ? 'danger' : 'warn'}
      icon="bell"
      title={current.title}
      body={current.body}
      action={
        announcements.length > 1 ? (
          <Link to="/notifications" className="btn sm ghost">
            {t('announcementBanner.more', { count: announcements.length - 1 })}
          </Link>
        ) : undefined
      }
      onDismiss={busy ? undefined : () => void dismiss()}
      dismissLabel={t('announcementBanner.dismiss')}
    />
  )
}
