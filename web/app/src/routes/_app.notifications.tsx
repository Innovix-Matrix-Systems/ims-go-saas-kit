import { Link, createFileRoute } from '@tanstack/react-router'
import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { feedKey, fetchFeed, markAllRead, markRead, type FeedItem } from '../components/notifications'
import { Badge, Button, Card, EmptyState, PageHeader, Spinner } from '../components/ui'
import { intlLocale, useLocale, useT } from '../i18n'

export const Route = createFileRoute('/_app/notifications')({
  component: NotificationsPage,
})

function NotificationsPage() {
  const t = useT()
  const locale = useLocale()
  const client = useQueryClient()
  const [unreadOnly, setUnreadOnly] = useState(false)
  const feed = useInfiniteQuery({
    queryKey: [...feedKey, 'page', unreadOnly],
    queryFn: ({ pageParam }) => fetchFeed({ cursor: pageParam, limit: 20, unreadOnly }),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.meta.next_cursor ?? null,
  })
  const items = feed.data?.pages.flatMap((page) => page.data) ?? []
  const unread = feed.data?.pages[0]?.meta.unread_count ?? 0
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
              <input type="checkbox" checked={unreadOnly} onChange={(e) => setUnreadOnly(e.target.checked)} />
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
          <div className="p-4">
            <Spinner />
          </div>
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
        {feed.hasNextPage ? (
          <div className="p-4">
            <Button size="sm" disabled={feed.isFetchingNextPage} onClick={() => void feed.fetchNextPage()}>
              {feed.isFetchingNextPage ? <Spinner /> : null} {t('notifications.loadMore')}
            </Button>
          </div>
        ) : null}
      </Card>
    </>
  )
}
