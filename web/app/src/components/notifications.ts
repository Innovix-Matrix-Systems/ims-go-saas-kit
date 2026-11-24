import { api, type Schemas } from '../api/client'
import { sessionQuery, type Session } from '../auth/session'
import { queryClient } from '../query'

/**
 * Where the signed-in account's own notifications live. Staff without an
 * organization use the internal routes: on the organization routes a staff
 * account reads every organization at once. Impersonating or logging in as
 * someone uses the organization routes, which then answer for them.
 */
export function notificationPath(path: string): string {
  const session = queryClient.getQueryData<Session>(sessionQuery.queryKey)
  return session?.isPlatformUser && !session.acting ? `/internal${path}` : path
}

export type FeedItem = Schemas['FeedItem']
export type FeedPage = Schemas['NotificationFeedEnvelope']

/** One page of the caller's notifications (newest first). */
export function fetchFeed(options: { cursor?: string | null; limit?: number; unreadOnly?: boolean } = {}): Promise<FeedPage> {
  return api<FeedPage>(notificationPath('/notification/feed'), {
    envelope: true,
    query: { limit: options.limit ?? 20, cursor: options.cursor ?? undefined, unread_only: options.unreadOnly ? true : undefined },
  })
}

export function markRead(id: number): Promise<unknown> {
  return api(notificationPath(`/notification/${id}/read`), { method: 'POST' })
}

export function markAllRead(): Promise<unknown> {
  return api(notificationPath('/notification/read-all'), { method: 'POST' })
}

/**
 * Where a notification leads, if it says (data.url, an in-app path such as
 * "/org/billing"). Anything that is not a plain in-app path is ignored.
 */
export function notificationLink(item: FeedItem): string | null {
  const data = item.data
  if (!data || typeof data !== 'object') return null
  const url = (data as { url?: unknown }).url
  return typeof url === 'string' && url.startsWith('/') && !url.startsWith('//') ? url : null
}

/** Query key prefix shared by the bell and the page, so marking read refreshes both. */
export const feedKey = ['notifications'] as const
