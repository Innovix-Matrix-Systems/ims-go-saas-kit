import { api, type Schemas } from '../api/client'

export type FeedItem = Schemas['FeedItem']
export type FeedPage = Schemas['NotificationFeedEnvelope']

/** One page of the caller's notifications (newest first). */
export function fetchFeed(options: { cursor?: string | null; limit?: number; unreadOnly?: boolean } = {}): Promise<FeedPage> {
  return api<FeedPage>('/notification/feed', {
    envelope: true,
    query: { limit: options.limit ?? 20, cursor: options.cursor ?? undefined, unread_only: options.unreadOnly ? true : undefined },
  })
}

export function markRead(id: number): Promise<unknown> {
  return api(`/notification/${id}/read`, { method: 'POST' })
}

export function markAllRead(): Promise<unknown> {
  return api('/notification/read-all', { method: 'POST' })
}

/** Query key prefix shared by the bell and the page, so marking read refreshes both. */
export const feedKey = ['notifications'] as const
