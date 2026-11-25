import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useState, type ReactNode } from 'react'
import { api, type Schemas } from '../api/client'
import { can, useSession } from '../auth/session'
import { intlLocale, useLocale, useT } from '../i18n'
import { subscriptionStatusQuery } from './billing'
import { organizationQuery } from './organization'
import { Icon, type IconName } from './ui'
import { usePollInterval } from './realtime'
import { inOrganization } from './WorkspaceMenu'

// Banners across the top of the app for what must not be missed: staff
// announcements, and the organization's plan running out.

/** How soon a plan's end is worth a warning, in days. */
const WARN_DAYS = 7

type Tone = 'info' | 'promo' | 'warn' | 'danger'

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
  dismissLabel?: string
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

/* ── Announcements ─────────────────────────────────────── */

export type LiveAnnouncement = Schemas['LiveAnnouncement']
export type AnnouncementKind = LiveAnnouncement['kind']

const kindTones: Record<AnnouncementKind, Tone> = { info: 'info', promotion: 'promo', warning: 'warn', critical: 'danger' }
const kindIcons: Record<AnnouncementKind, IconName> = { info: 'bell', promotion: 'package', warning: 'alert', critical: 'alert' }

/**
 * An announcement as members see it. The console's editor shows the same
 * component as a preview (without the callbacks).
 */
export function AnnouncementView({
  announcement,
  onDismiss,
  onAction,
}: {
  announcement: Pick<LiveAnnouncement, 'title' | 'body' | 'kind' | 'cta_label' | 'cta_url' | 'dismissible'>
  onDismiss?: () => void
  onAction?: () => void
}) {
  const t = useT()
  const { title, body, kind, cta_label: label, cta_url: url, dismissible } = announcement
  return (
    <Banner
      tone={kindTones[kind]}
      icon={kindIcons[kind]}
      title={title || t('announcements.untitled')}
      body={body}
      action={
        label && url ? (
          <a className={`btn sm ${kind === 'promotion' ? 'primary' : ''}`} href={url} target="_blank" rel="noopener noreferrer" onClick={onAction}>
            {label}
            <Icon name="external" />
          </a>
        ) : undefined
      }
      onDismiss={dismissible ? (onDismiss ?? (() => undefined)) : undefined}
      dismissLabel={t('announcementBanner.dismiss')}
    />
  )
}

const DISMISSED_KEY = 'ims.app.announcements.dismissed'
const VIEWED_KEY = 'ims.app.announcements.viewed'

function readIds(store: Storage | undefined, key: string): number[] {
  try {
    const parsed = JSON.parse(store?.getItem(key) ?? '[]')
    return Array.isArray(parsed) ? parsed.filter((id) => typeof id === 'number') : []
  } catch {
    return []
  }
}

function writeIds(store: Storage | undefined, key: string, ids: number[]) {
  try {
    // Only the latest hundred: old announcements end anyway.
    store?.setItem(key, JSON.stringify(ids.slice(-100)))
  } catch {
    // Storage is off: the server still knows what was dismissed.
  }
}

const local = () => (typeof window === 'undefined' ? undefined : window.localStorage)
const session = () => (typeof window === 'undefined' ? undefined : window.sessionStorage)

/** Tells the API what the member did; a failure is not worth bothering them. */
function track(id: number, type: 'view' | 'click' | 'dismiss') {
  void api(`/announcements/${id}/events`, { method: 'POST', body: { type } }).catch(() => undefined)
}

/**
 * The most urgent live announcement for the member's organization, as a
 * banner. Closing it is remembered on this device and on the server (so it
 * stays closed elsewhere); the first view, a click on its button and the
 * dismissal are counted.
 */
export function AnnouncementBanner() {
  const user = useSession()
  const inside = inOrganization(user)
  const live = useQuery({
    queryKey: ['announcements', 'live'],
    queryFn: () => api<LiveAnnouncement[]>('/announcements/live'),
    enabled: inside,
    // Live updates refetch it when an announcement starts, changes or ends;
    // the poll is the fallback.
    refetchInterval: usePollInterval(5 * 60_000, 15 * 60_000),
    refetchIntervalInBackground: false,
  })
  const [dismissed, setDismissed] = useState(() => readIds(local(), DISMISSED_KEY))
  const current = (live.data ?? []).find((item) => !dismissed.includes(item.id))

  useEffect(() => {
    if (!current) return
    const viewed = readIds(session(), VIEWED_KEY)
    if (viewed.includes(current.id)) return
    writeIds(session(), VIEWED_KEY, [...viewed, current.id])
    track(current.id, 'view')
  }, [current?.id])

  if (!inside || !current) return null

  const dismiss = () => {
    const next = [...dismissed, current.id]
    writeIds(local(), DISMISSED_KEY, next)
    setDismissed(next)
    track(current.id, 'dismiss')
  }
  return <AnnouncementView announcement={current} onDismiss={dismiss} onAction={() => track(current.id, 'click')} />
}

/* ── Plan ──────────────────────────────────────────────── */

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
  const user = useSession()
  const allowed = inOrganization(user) && can(user, 'billing.view')
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
    const when = daysLeft === null ? '' : daysLeft <= 0 ? t('planBanner.today') : daysLeft === 1 ? t('planBanner.tomorrow') : t('planBanner.inDays', { count: daysLeft })
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
