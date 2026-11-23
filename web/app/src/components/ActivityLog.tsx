import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useDeferredValue, useEffect, useMemo, useState } from 'react'
import { api, saveBlob, type Schemas } from '../api/client'
import { formatDateTime, localDayEnd, localDayStart, relativeTimeIn } from '../format'
import { intlLocale, useLocale, useT } from '../i18n'
import { allTenantsQuery } from './console'
import { DatePicker, Select } from './inputs'
import { CursorPagination, useCursorPages } from './Pagination'
import { Badge, Button, Card, Drawer, EmptyState, ErrorBox, Icon, JsonView, LoadingBlock, PageHeader, StatTile } from './ui'

/** The filters, kept in the URL so a filtered view can be shared. */
export interface ActivitySearch {
  q?: string
  action?: string
  who?: number
  area?: string
  from?: string
  to?: string
  /** Platform log only: '' every organization and the system log, 'system', or a tenant id. */
  org?: string
}

export function validateActivitySearch(search: Record<string, unknown>): ActivitySearch {
  const text = (value: unknown) => (typeof value === 'string' && value ? value : undefined)
  return {
    q: text(search.q),
    action: text(search.action),
    who: typeof search.who === 'number' ? search.who : undefined,
    area: text(search.area),
    from: text(search.from),
    to: text(search.to),
    org: typeof search.org === 'number' ? String(search.org) : text(search.org),
  }
}

type Entry = Schemas['ActivityEntry']
type Feed = Schemas['ActivityFeedEnvelope']
type Facets = Schemas['ActivityFacets']

/** "role.permissions_assigned" → "Role permissions assigned". */
function actionLabel(action: string): string {
  const text = action.replace(/[._-]+/g, ' ').trim()
  return text.charAt(0).toUpperCase() + text.slice(1)
}

const subjectOf = (entry: Entry) => (entry.subject_id ? `${entry.subject_type} #${entry.subject_id}` : entry.subject_type)

/**
 * The activity log, laid out like the developer dashboard's audit log. An
 * organization reads its own log; `platform` (staff) reads every
 * organization's log and the system log, with an organization filter and
 * who acted as whom.
 */
export function ActivityLog({
  platform,
  search,
  onSearch,
}: {
  platform?: boolean
  search: ActivitySearch
  onSearch: (patch: Partial<ActivitySearch> | null) => void
}) {
  const t = useT()
  const locale = intlLocale(useLocale())
  const [text, setText] = useState(search.q ?? '')
  const deferredText = useDeferredValue(text.trim())
  const [openIndex, setOpenIndex] = useState<number | null>(null)
  const pages = useCursorPages(50)

  const setFilter = (patch: Partial<ActivitySearch> | null) => {
    setOpenIndex(null)
    pages.reset()
    onSearch(patch)
  }
  useEffect(() => {
    if ((search.q ?? '') !== deferredText) setFilter({ q: deferredText || undefined })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [deferredText])
  useEffect(() => setText(search.q ?? ''), [search.q])

  const filters = { ...search, q: deferredText || undefined }
  // Staff read as themselves (never through an organization they impersonate).
  const as = platform ? { asStaff: true } : {}
  const tenantFilter = platform ? filters.org : undefined
  const feed = useQuery({
    queryKey: ['activity', platform ? 'platform' : 'org', filters, pages.cursor ?? '', pages.perPage],
    queryFn: ({ signal }) =>
      api<Feed>('/activity', {
        ...as,
        envelope: true,
        signal,
        query: {
          per_page: pages.perPage,
          cursor: pages.cursor,
          tenant_id: tenantFilter,
          search: filters.q,
          action: filters.action,
          actor_id: filters.who,
          subject_type: filters.area,
          from: localDayStart(filters.from),
          to: localDayEnd(filters.to),
        },
      }),
    placeholderData: keepPreviousData,
  })
  const facets = useQuery({
    queryKey: ['activity', 'facets', platform ? 'platform' : 'org', tenantFilter ?? ''],
    queryFn: () => api<Facets>('/activity/facets', { ...as, query: { tenant_id: tenantFilter } }),
    staleTime: 30_000,
  })
  const tenants = useQuery({ ...allTenantsQuery, enabled: Boolean(platform) })
  const tenantNames = useMemo(() => new Map((tenants.data ?? []).map((tenant) => [tenant.id, tenant.name])), [tenants.data])
  const organizationOf = (entry: Entry) =>
    entry.tenant_id == null ? t('activity.system') : (tenantNames.get(entry.tenant_id) ?? `#${entry.tenant_id}`)

  const rows = feed.data?.data ?? []
  const total = feed.data?.meta.total
  const filtered = Boolean(filters.q || filters.action || filters.who || filters.area || filters.from || filters.to || filters.org)
  const selected = openIndex !== null ? (rows[openIndex] ?? null) : null
  const allEntries = (facets.data?.actions ?? []).reduce((sum, item) => sum + item.total, 0)

  const exportCSV = () => {
    const header = ['when', ...(platform ? ['organization'] : []), 'who', 'action', 'subject', 'ip', 'by_staff', 'details']
    const escape = (value: unknown) => `"${String(value ?? '').replace(/"/g, '""')}"`
    const lines = rows.map((row) =>
      [
        row.created_at,
        ...(platform ? [organizationOf(row)] : []),
        row.actor?.name ?? '',
        row.action,
        subjectOf(row),
        row.ip,
        row.by_staff,
        JSON.stringify(row.changes),
      ]
        .map(escape)
        .join(','),
    )
    saveBlob(new Blob([[header.join(','), ...lines].join('\n')], { type: 'text/csv' }), `activity-${new Date().toISOString().slice(0, 10)}.csv`)
  }

  return (
    <>
      <PageHeader
        title={platform ? t('console.auditLog') : t('org.activity')}
        description={platform ? t('console.auditLogSubtitle') : t('activity.subtitle')}
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
          {platform ? (
            <div className="field">
              <span className="field-label">{t('console.colOrg')}</span>
              <Select
                value={filters.org ?? ''}
                ariaLabel={t('console.colOrg')}
                onChange={(value) => setFilter({ org: value || undefined })}
                options={[
                  { value: '', label: t('console.allActivity') },
                  { value: 'system', label: t('console.systemActivity') },
                  ...(tenants.data ?? []).map((tenant) => ({ value: String(tenant.id), label: tenant.name })),
                ]}
              />
            </div>
          ) : null}
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
                  setFilter(null)
                }}
              >
                {t('activity.clear')}
              </button>
            ) : null}
            {total != null ? <span className="small dim">{t('activity.matching', { count: total })}</span> : null}
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
                  {platform ? <th>{t('console.colOrg')}</th> : null}
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
                    {platform ? <td className="small">{row.tenant_id == null ? <Badge tone="violet">{t('activity.system')}</Badge> : organizationOf(row)}</td> : null}
                    <td className="small">
                      <span className="audit-action">
                        {row.actor?.name ?? t('activity.system')}
                        {row.by_staff ? <Badge tone="warn">{row.impersonator?.name ? t('console.actedBy', { name: row.impersonator.name }) : t('activity.byStaff')}</Badge> : null}
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
        {feed.data && (rows.length > 0 || pages.index > 0) ? (
          <CursorPagination
            index={pages.index}
            perPage={pages.perPage}
            shown={rows.length}
            total={total}
            hasNext={Boolean(feed.data.meta.next_cursor)}
            onPrevious={() => {
              setOpenIndex(null)
              pages.previous()
            }}
            onNext={() => {
              setOpenIndex(null)
              pages.next(feed.data?.meta.next_cursor)
            }}
            onPerPage={pages.setPerPage}
          />
        ) : null}
      </Card>

      <ActivityDrawer
        entry={selected}
        organization={platform && selected ? organizationOf(selected) : undefined}
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
  organization,
  hasPrev,
  hasNext,
  onPrev,
  onNext,
  onClose,
  onFilter,
}: {
  entry: Entry | null
  organization?: string
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
          {organization ? (
            <>
              <dt>{t('console.colOrg')}</dt>
              <dd>{organization}</dd>
            </>
          ) : null}
          <dt>{t('activity.colWho')}</dt>
          <dd>
            <span className="audit-action">
              {entry.actor?.name ?? t('activity.system')}
              {entry.by_staff ? <Badge tone="warn">{t('activity.byStaff')}</Badge> : null}
            </span>
          </dd>
          {entry.impersonator ? (
            <>
              <dt>{t('console.actingStaff')}</dt>
              <dd>{entry.impersonator.name || `#${entry.impersonator.id}`}</dd>
            </>
          ) : null}
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
