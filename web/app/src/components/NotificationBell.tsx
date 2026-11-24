import { Link, useNavigate } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useState } from 'react'
import { relativeTime } from '../format'
import { useT } from '../i18n'
import { useDismiss } from './inputs'
import { usePollInterval } from './realtime'
import { feedKey, fetchFeed, markAllRead, markRead, notificationLink, type FeedItem } from './notifications'
import { Icon } from './ui'

/** The top bar's bell: unread count, the latest notifications, mark as read. */
export function NotificationBell() {
  const t = useT()
  const client = useQueryClient()
  const [open, setOpen] = useState(false)
  const close = useCallback(() => setOpen(false), [])
  const ref = useDismiss(open, close)
  const feed = useQuery({
    queryKey: [...feedKey, 'bell'],
    queryFn: () => fetchFeed({ limit: 8 }),
    // Live updates refetch it as soon as something changes; the poll is the
    // fallback (every minute, every five while the stream is up).
    refetchInterval: usePollInterval(60_000, 5 * 60_000),
    refetchIntervalInBackground: false,
  })
  const unread = feed.data?.meta.unread_count ?? 0
  const refresh = () => void client.invalidateQueries({ queryKey: feedKey })

  const navigate = useNavigate()
  const read = async (item: FeedItem) => {
    const link = notificationLink(item)
    if (link) {
      close()
      void navigate({ to: link })
    }
    if (!item.is_read) {
      await markRead(item.id)
      refresh()
    }
  }

  return (
    <div className="appearance" ref={ref}>
      <button
        type="button"
        className="icon-btn bell-btn"
        aria-label={unread > 0 ? t('notifications.unreadLabel', { count: unread }) : t('notifications.title')}
        title={t('notifications.title')}
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
      >
        <Icon name="bell" />
        {unread > 0 ? <span className="bell-count">{unread > 99 ? '99+' : unread}</span> : null}
      </button>
      {open ? (
        <div className="ui-popover align-right bell-panel" role="dialog" aria-label={t('notifications.title')}>
          <div className="bell-head">
            <strong>{t('notifications.title')}</strong>
            {unread > 0 ? (
              <button type="button" className="link-btn small" onClick={() => void markAllRead().then(refresh)}>
                {t('notifications.markAllRead')}
              </button>
            ) : null}
          </div>
          {(feed.data?.data ?? []).length === 0 ? (
            <div className="bell-empty">{t('notifications.empty')}</div>
          ) : (
            <ul className="bell-list">
              {feed.data!.data.map((item) => (
                <li key={item.id}>
                  <button type="button" className={`bell-item ${item.is_read ? '' : 'unread'} ${item.priority.toLowerCase()}`} onClick={() => void read(item)}>
                    <span className="bell-dot" aria-hidden="true" />
                    <span className="bell-text">
                      <span className="bell-title">{item.title}</span>
                      {item.body ? <span className="bell-body">{item.body}</span> : null}
                      <span className="bell-time">{relativeTime(item.created_at)}</span>
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
          <Link to="/notifications" className="bell-all" onClick={close}>
            {t('notifications.viewAll')}
          </Link>
        </div>
      ) : null}
    </div>
  )
}
