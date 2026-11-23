import { Link, createFileRoute } from '@tanstack/react-router'
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { feedKey, fetchFeed, markAllRead, markRead, type FeedItem } from '../components/notifications'
import { CursorPagination, useCursorPages } from '../components/Pagination'
import { Badge, Button, Card, EmptyState, PageHeader } from '../components/ui'
import { intlLocale, useLocale, useT } from '../i18n'
import { ListSkeleton } from '../components/Skeleton'

export const Route = createFileRoute('/_app/notifications')({
  component: NotificationsPage,
})

function NotificationsPage() {
  const t = useT()
  const locale = useLocale()
  const client = useQueryClient()
  const [unreadOnly, setUnreadOnly] = useState(false)
  const pages = useCursorPages(20)
  // The feed is cursor-paged: each page asks for the rows after the last one seen.
  const feed = useQuery({
    queryKey: [...feedKey, 'page', unreadOnly, pages.cursor ?? '', pages.perPage],
    queryFn: () => fetchFeed({ cursor: pages.cursor, limit: pages.perPage, unreadOnly }),
    placeholderData: keepPreviousData,
  })
  const items = feed.data?.data ?? []
  const unread = feed.data?.meta.unread_count ?? 0
  const refresh = () => void client.invalidateQueries({ queryKey: feedKey })
  const when = (value: string) =>
    new Intl.DateTimeFormat(intlLocale(locale), { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))

  const read = async (item: FeedItem) => {
    if (!item.is_read) {
      await markRead(item.id)
      refresh()
    }
  }

  return (
    <>
      <PageHeader
        title={t('notifications.title')}
        description={unread > 0 ? t('notifications.unreadLabel', { count: unread }) : undefined}
        actions={
          <div className="flex g-2 align-center">
            <label className="check-row">
              <input
                type="checkbox"
                checked={unreadOnly}
                onChange={(e) => {
                  setUnreadOnly(e.target.checked)
                  pages.reset()
                }}
              />
              {t('notifications.unreadOnly')}
            </label>
            <Button size="sm" disabled={unread === 0} onClick={() => void markAllRead().then(refresh)}>
              {t('notifications.markAllRead')}
            </Button>
            <Link to="/settings/notifications" className="btn ghost sm">
              {t('notifications.preferences')}
            </Link>
          </div>
        }
      />
      <Card flush>
        {feed.isLoading ? (
          <ListSkeleton rows={6} />
        ) : items.length === 0 ? (
          <EmptyState icon="bell" title={t('notifications.empty')} />
        ) : (
          <ul className="bell-list page">
            {items.map((item) => (
              <li key={item.id}>
                <button type="button" className={`bell-item ${item.is_read ? '' : 'unread'} ${item.priority.toLowerCase()}`} onClick={() => void read(item)}>
                  <span className="bell-dot" aria-hidden="true" />
                  <span className="bell-text">
                    <span className="bell-title">
                      {item.title}{' '}
                      {item.priority !== 'INFO' ? (
                        <Badge tone={item.priority === 'CRITICAL' ? 'danger' : 'warn'}>{item.priority.toLowerCase()}</Badge>
                      ) : null}
                    </span>
                    {item.body ? <span className="bell-body">{item.body}</span> : null}
                    <span className="bell-time">{when(item.created_at)}</span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
        {feed.data && (items.length > 0 || pages.index > 0) ? (
          <CursorPagination
            index={pages.index}
            perPage={pages.perPage}
            shown={items.length}
            hasNext={Boolean(feed.data.meta.next_cursor)}
            onPrevious={pages.previous}
            onNext={() => pages.next(feed.data?.meta.next_cursor)}
            onPerPage={pages.setPerPage}
          />
        ) : null}
      </Card>
    </>
  )
}
