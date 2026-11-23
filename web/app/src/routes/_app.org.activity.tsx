import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { keepPreviousData, useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { useDeferredValue, useEffect, useMemo, useState } from 'react'
import { api, saveBlob, type Schemas } from '../api/client'
import { formatDateTime, localDayEnd, localDayStart, relativeTimeIn } from '../format'
import { DatePicker, Select } from '../components/inputs'
import { Badge, Button, Card, Drawer, EmptyState, ErrorBox, Icon, JsonView, LoadingBlock, PageHeader, StatTile } from '../components/ui'
import { intlLocale, useLocale, useT } from '../i18n'

interface ActivitySearch {
  q?: string
  action?: string
  who?: number
  area?: string
  from?: string
  to?: string
}

export const Route = createFileRoute('/_app/org/activity')({
  validateSearch: (search: Record<string, unknown>): ActivitySearch => ({
    q: typeof search.q === 'string' && search.q ? search.q : undefined,
    action: typeof search.action === 'string' && search.action ? search.action : undefined,
    who: typeof search.who === 'number' ? search.who : undefined,
    area: typeof search.area === 'string' && search.area ? search.area : undefined,
    from: typeof search.from === 'string' && search.from ? search.from : undefined,
    to: typeof search.to === 'string' && search.to ? search.to : undefined,
  }),
  component: ActivityPage,
})

type Entry = Schemas['ActivityEntry']
type Feed = Schemas['ActivityFeedEnvelope']
type Facets = Schemas['ActivityFacets']

/** "role.permissions_assigned" → "Role permissions assigned". */
function actionLabel(action: string): string {
  const text = action.replace(/[._-]+/g, ' ').trim()
  return text.charAt(0).toUpperCase() + text.slice(1)
}

const subjectOf = (entry: Entry) => (entry.subject_id ? `${entry.subject_type} #${entry.subject_id}` : entry.subject_type)

function ActivityPage() {
  const t = useT()
  const locale = intlLocale(useLocale())
  const search = Route.useSearch()
  const navigate = useNavigate({ from: '/org/activity' })
  const [text, setText] = useState(search.q ?? '')
  const deferredText = useDeferredValue(text.trim())
  const [openIndex, setOpenIndex] = useState<number | null>(null)

  const setFilter = (patch: Partial<ActivitySearch>) => {
    setOpenIndex(null)
    void navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true })
  }
  useEffect(() => {
    if ((search.q ?? '') !== deferredText) setFilter({ q: deferredText || undefined })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [deferredText])
  useEffect(() => setText(search.q ?? ''), [search.q])

  const filters = { ...search, q: deferredText || undefined }
  const feed = useInfiniteQuery({
    queryKey: ['activity', filters],
    queryFn: ({ pageParam, signal }) =>
      api<Feed>('/activity', {
        envelope: true,
        signal,
        query: {
          per_page: 50,
          cursor: pageParam,
          search: filters.q,
          action: filters.action,
          actor_id: filters.who,
          subject_type: filters.area,
          from: localDayStart(filters.from),
          to: localDayEnd(filters.to),
        },
      }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.meta.next_cursor ?? undefined,
    placeholderData: keepPreviousData,
  })
  const facets = useQuery({ queryKey: ['activity', 'facets'], queryFn: () => api<Facets>('/activity/facets'), staleTime: 30_000 })

  const rows = useMemo(() => feed.data?.pages.flatMap((page) => page.data) ?? [], [feed.data])
  const total = feed.data?.pages[0]?.meta.total ?? rows.length
  const filtered = Boolean(filters.q || filters.action || filters.who || filters.area || filters.from || filters.to)
  const selected = openIndex !== null ? (rows[openIndex] ?? null) : null
  const allEntries = (facets.data?.actions ?? []).reduce((sum, item) => sum + item.total, 0)

  const exportCSV = () => {
    const header = ['when', 'who', 'action', 'subject', 'ip', 'by_staff', 'details']
    const escape = (value: unknown) => `"${String(value ?? '').replace(/"/g, '""')}"`
    const lines = rows.map((row) =>
      [row.created_at, row.actor?.name ?? '', row.action, subjectOf(row), row.ip, row.by_staff, JSON.stringify(row.changes)].map(escape).join(','),
    )
    saveBlob(new Blob([[header.join(','), ...lines].join('\n')], { type: 'text/csv' }), `activity-${new Date().toISOString().slice(0, 10)}.csv`)
  }

  return (
    <>
      <PageHeader
        title={t('org.activity')}
        description={t('activity.subtitle')}
        actions={
          <>
            <Button icon="download" onClick={exportCSV} disabled={rows.length === 0}>
              {t('activity.export')}
            </Button>
            <Button
              icon="refresh"
              onClick={() => {
                void feed.refetch()
                void facets.refetch()
              }}
              disabled={feed.isFetching}
            >
              {t('activity.refresh')}
            </Button>
          </>
        }
      />

      <div className="audit-stats">
        <StatTile label={t('activity.lastDay')} value={facets.data?.last_day ?? 0} hint={t('activity.lastDayHint')} />
        <StatTile label={t('activity.entries')} value={allEntries} hint={t('activity.entriesHint')} />
        <StatTile label={t('activity.people')} value={facets.data?.actors.length ?? 0} hint={t('activity.peopleHint')} />
      </div>

      <Card className="mt-16">
        <div className="audit-filters">
          <label className="field">
            <span className="field-label">{t('activity.search')}</span>
            <span className="search-box">
              <Icon name="search" />
              <input className="input" placeholder={t('activity.searchPlaceholder')} value={text} onChange={(event) => setText(event.target.value)} />
            </span>
          </label>
          <div className="field">
            <span className="field-label">{t('activity.action')}</span>
            <Select
              value={filters.action ?? ''}
              ariaLabel={t('activity.action')}
              onChange={(value) => setFilter({ action: value || undefined })}
              options={[
                { value: '', label: t('activity.anyAction') },
                ...(facets.data?.actions ?? []).map((item) => ({
                  value: item.value,
                  label: actionLabel(item.value),
                  text: `${actionLabel(item.value)} ${item.value}`,
                  hint: String(item.total),
                })),
              ]}
            />
          </div>
          <div className="field">
            <span className="field-label">{t('activity.who')}</span>
            <Select
              value={filters.who ? String(filters.who) : ''}
              ariaLabel={t('activity.who')}
              onChange={(value) => setFilter({ who: value ? Number(value) : undefined })}
              options={[
                { value: '', label: t('activity.anyone') },
                ...(facets.data?.actors ?? []).map((actor) => ({ value: String(actor.id), label: actor.name || `#${actor.id}`, hint: String(actor.total) })),
              ]}
            />
          </div>
          <div className="field">
            <span className="field-label">{t('activity.area')}</span>
            <Select
              value={filters.area ?? ''}
              ariaLabel={t('activity.area')}
              onChange={(value) => setFilter({ area: value || undefined })}
              options={[
                { value: '', label: t('activity.anyArea') },
                ...(facets.data?.subjects ?? []).map((item) => ({ value: item.value, label: actionLabel(item.value), hint: String(item.total) })),
              ]}
            />
          </div>
          <div className="field">
            <span className="field-label">{t('activity.from')}</span>
            <DatePicker value={filters.from ?? ''} max={filters.to} placeholder={t('activity.notSet')} onChange={(value) => setFilter({ from: value || undefined })} />
          </div>
          <div className="field">
            <span className="field-label">{t('activity.to')}</span>
            <DatePicker value={filters.to ?? ''} min={filters.from} placeholder={t('activity.notSet')} onChange={(value) => setFilter({ to: value || undefined })} />
          </div>
          <div className="audit-count">
            {filtered ? (
              <button
                type="button"
                className="link-btn"
                onClick={() => {
                  setText('')
                  void navigate({ search: {}, replace: true })
                }}
              >
                {t('activity.clear')}
              </button>
            ) : null}
            <span className="small dim">{t('activity.matching', { count: total })}</span>
          </div>
        </div>
      </Card>

      <Card className="mt-16" flush>
        {feed.isLoading ? (
          <LoadingBlock label={t('activity.loading')} />
        ) : feed.isError ? (
          <ErrorBox message={feed.error instanceof Error ? feed.error.message : String(feed.error)} />
        ) : rows.length === 0 ? (
          <EmptyState icon="activity" title={filtered ? t('activity.noMatch') : t('activity.empty')} description={filtered ? t('activity.noMatchHint') : t('activity.emptyHint')} />
        ) : (
          <div className="table-wrap">
            <table className="table audit-table">
              <thead>
                <tr>
                  <th className="w-sm">{t('activity.colWhen')}</th>
                  <th>{t('activity.colWho')}</th>
                  <th>{t('activity.colAction')}</th>
                  <th>{t('activity.colSubject')}</th>
                  <th className="w-sm">{t('activity.colIp')}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row, index) => (
                  <tr key={row.id} className={`clickable ${openIndex === index ? 'selected' : ''}`} onClick={() => setOpenIndex(index)}>
                    <td className="small dim nowrap" title={formatDateTime(row.created_at)}>
                      {relativeTimeIn(locale, row.created_at)}
                    </td>
                    <td className="small">
                      <span className="audit-action">
                        {row.actor?.name ?? t('activity.system')}
                        {row.by_staff ? <Badge tone="warn">{t('activity.byStaff')}</Badge> : null}
                      </span>
                    </td>
                    <td>
                      <span className="audit-action">
                        <span className="status-dot ok" />
                        <b>{actionLabel(row.action)}</b>
                        <span className="tag">{row.subject_type}</span>
                      </span>
                    </td>
                    <td className="mono small dim truncate maxw-sm">{subjectOf(row)}</td>
                    <td className="mono small dim">{row.ip ?? '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {rows.length > 0 ? (
          <div className="pagination">
            {feed.hasNextPage ? (
              <Button icon="chevron" onClick={() => void feed.fetchNextPage()} disabled={feed.isFetchingNextPage}>
                {feed.isFetchingNextPage ? t('activity.loading') : t('activity.more')}
              </Button>
            ) : null}
            <span className="spacer" />
            <span className="small muted">{t('activity.showingAll', { shown: rows.length, total })}</span>
          </div>
        ) : null}
      </Card>

      <ActivityDrawer
        entry={selected}
        hasPrev={openIndex !== null && openIndex > 0}
        hasNext={openIndex !== null && openIndex < rows.length - 1}
        onPrev={() => setOpenIndex((index) => (index !== null && index > 0 ? index - 1 : index))}
        onNext={() => setOpenIndex((index) => (index !== null && index < rows.length - 1 ? index + 1 : index))}
        onClose={() => setOpenIndex(null)}
        onFilter={setFilter}
      />
    </>
  )
}

function ActivityDrawer({
  entry,
  hasPrev,
  hasNext,
  onPrev,
  onNext,
  onClose,
  onFilter,
}: {
  entry: Entry | null
  hasPrev: boolean
  hasNext: boolean
  onPrev: () => void
  onNext: () => void
  onClose: () => void
  onFilter: (patch: Partial<ActivitySearch>) => void
}) {
  const t = useT()
  const locale = intlLocale(useLocale())
  useEffect(() => {
    if (!entry) return
    const onKey = (event: KeyboardEvent) => {
      if (event.target instanceof HTMLInputElement) return
      if (event.key === 'ArrowUp' || event.key === 'k') onPrev()
      if (event.key === 'ArrowDown' || event.key === 'j') onNext()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [entry, onPrev, onNext])

  if (!entry) return null
  const when = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(entry.created_at))
  return (
    <Drawer
      open
      title={
        <span className="audit-action">
          <span className="status-dot ok" />
          {actionLabel(entry.action)}
        </span>
      }
      subtitle={`${when} · ${relativeTimeIn(locale, entry.created_at)}`}
      onClose={onClose}
      actions={
        <span className="flex g-1">
          <button type="button" className="icon-btn flip" title={t('activity.newer')} disabled={!hasPrev} onClick={onPrev}>
            <Icon name="chevron" />
          </button>
          <button type="button" className="icon-btn" title={t('activity.older')} disabled={!hasNext} onClick={onNext}>
            <Icon name="chevron" />
          </button>
        </span>
      }
    >
      <div className="stack">
        <dl className="kv audit-kv">
          <dt>{t('activity.colWho')}</dt>
          <dd>
            <span className="audit-action">
              {entry.actor?.name ?? t('activity.system')}
              {entry.by_staff ? <Badge tone="warn">{t('activity.byStaff')}</Badge> : null}
            </span>
          </dd>
          <dt>{t('activity.when')}</dt>
          <dd>{when}</dd>
          <dt>{t('activity.actionKey')}</dt>
          <dd className="mono">{entry.action}</dd>
          <dt>{t('activity.colSubject')}</dt>
          <dd>{subjectOf(entry)}</dd>
          <dt>{t('activity.ip')}</dt>
          <dd>{entry.ip ?? '—'}</dd>
          {entry.request_id ? (
            <>
              <dt>{t('activity.requestId')}</dt>
              <dd className="mono small">{entry.request_id}</dd>
            </>
          ) : null}
        </dl>
        <div>
          <JsonView value={entry.changes} label={t('activity.changes')} />
          <div className="small dim mt-8">{t('activity.secrets')}</div>
        </div>
        <div className="audit-quick">
          {entry.actor ? (
            <Button size="sm" icon="user" onClick={() => onFilter({ who: entry.actor!.id })}>
              {t('activity.byPerson')}
            </Button>
          ) : null}
          <Button size="sm" icon="activity" onClick={() => onFilter({ action: entry.action })}>
            {t('activity.onlyAction', { action: actionLabel(entry.action) })}
          </Button>
          <Button size="sm" icon="list" onClick={() => onFilter({ area: entry.subject_type })}>
            {t('activity.onlySubject', { type: entry.subject_type })}
          </Button>
          {entry.ip ? (
            <Button size="sm" icon="search" onClick={() => onFilter({ q: entry.ip ?? undefined })}>
              {t('activity.fromIp')}
            </Button>
          ) : null}
        </div>
      </div>
    </Drawer>
  )
}
