import { createFileRoute } from '@tanstack/react-router'
import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { api, type Schemas, type User } from '../api/client'
import { can, useSession } from '../auth/session'
import { initials, prettyJSON } from '../format'
import { Badge, Button, Card, EmptyState, PageHeader, Spinner } from '../components/ui'
import { intlLocale, useLocale, useT } from '../i18n'

export const Route = createFileRoute('/_app/org/activity')({
  component: ActivityPage,
})

type Entry = Schemas['ActivityEntry']
type Feed = Schemas['ActivityFeedEnvelope']

// What the log records about an organization (see the activity guide).
const subjects = ['user', 'role', 'branch', 'setting', 'subscription', 'payment', 'organization', 'webhook', 'file'] as const

function ActivityPage() {
  const t = useT()
  const session = useSession()
  const [subject, setSubject] = useState('')
  const [actor, setActor] = useState('')
  const feed = useInfiniteQuery({
    queryKey: ['activity', subject, actor],
    queryFn: ({ pageParam }) =>
      api<Feed>('/activity', { envelope: true, query: { per_page: 30, subject_type: subject, actor_id: actor, cursor: pageParam } }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.meta.next_cursor ?? undefined,
  })
  const members = useQuery({
    queryKey: ['members', 'everyone'],
    queryFn: () => api<User[]>('/user', { query: { per_page: 100, order_by: 'name', order_direction: 'asc' } }),
    enabled: can(session, 'user.view.all'),
  })
  const entries = feed.data?.pages.flatMap((page) => page.data) ?? []

  return (
    <>
      <PageHeader title={t('org.activity')} description={t('activity.subtitle')} />
      <div className="toolbar">
        <select className="input toolbar-select" value={subject} onChange={(e) => setSubject(e.target.value)}>
          <option value="">{t('activity.allSubjects')}</option>
          {subjects.map((type) => (
            <option key={type} value={type}>
              {type}
            </option>
          ))}
        </select>
        {members.data ? (
          <select className="input toolbar-select" value={actor} onChange={(e) => setActor(e.target.value)}>
            <option value="">{t('activity.allMembers')}</option>
            {members.data.map((user) => (
              <option key={user.id} value={user.id}>
                {user.name}
              </option>
            ))}
          </select>
        ) : null}
      </div>
      <Card flush>
        {feed.isLoading ? (
          <div className="p-4">
            <Spinner />
          </div>
        ) : entries.length === 0 ? (
          <EmptyState icon="activity" title={t('activity.empty')} />
        ) : (
          <ul className="activity-list">
            {entries.map((entry) => (
              <ActivityRow key={entry.id} entry={entry} />
            ))}
          </ul>
        )}
        {feed.hasNextPage ? (
          <div className="pager">
            <Button size="sm" disabled={feed.isFetchingNextPage} onClick={() => void feed.fetchNextPage()}>
              {feed.isFetchingNextPage ? <Spinner /> : null} {t('activity.more')}
            </Button>
          </div>
        ) : null}
      </Card>
    </>
  )
}

function ActivityRow({ entry }: { entry: Entry }) {
  const t = useT()
  const locale = useLocale()
  const [open, setOpen] = useState(false)
  const when = new Intl.DateTimeFormat(intlLocale(locale), { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(entry.created_at))
  const name = entry.actor?.name ?? t('activity.system')
  const hasChanges = Object.keys(entry.changes ?? {}).length > 0
  return (
    <li className="activity-row">
      <span className="avatar sm">{entry.actor ? initials(entry.actor.name) : '·'}</span>
      <div className="activity-body">
        <div className="activity-line">
          <span className="strong">{name}</span>
          <code className="activity-action">{entry.action}</code>
          {entry.subject_id ? <span className="small muted">{t('activity.subject', { type: entry.subject_type, id: entry.subject_id })}</span> : null}
          {entry.by_staff ? <Badge tone="warn">{t('activity.byStaff')}</Badge> : null}
        </div>
        <div className="small muted">
          {when}
          {entry.ip ? ` · ${t('activity.ip')} ${entry.ip}` : ''}
        </div>
        {hasChanges ? (
          <>
            <button type="button" className="link-button" aria-expanded={open} onClick={() => setOpen((value) => !value)}>
              {t('activity.changes')}
            </button>
            {open ? <pre className="activity-changes">{prettyJSON(entry.changes)}</pre> : null}
          </>
        ) : null}
      </div>
    </li>
  )
}
