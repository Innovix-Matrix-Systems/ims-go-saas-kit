import { createFileRoute } from '@tanstack/react-router'
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState, type FormEvent } from 'react'
import { api, type Schemas } from '../api/client'
import { can, useSession } from '../auth/session'
import { allTenantsQuery } from '../components/console'
import { Field, useApiErrors } from '../components/form'
import { DatePicker, TimePicker } from '../components/inputs'
import { Pagination } from '../components/Pagination'
import { TableSkeleton } from '../components/Skeleton'
import { AnnouncementView, type AnnouncementKind } from '../components/SystemBanners'
import { useToast } from '../components/Toasts'
import { Badge, Button, Card, ConfirmDialog, Drawer, EmptyState, Icon, PageHeader, Spinner, StatTile, type BadgeTone } from '../components/ui'
import { OrgMark } from '../components/WorkspaceMenu'
import { intlLocale, useLocale, useT, type MessageKey } from '../i18n'

export const Route = createFileRoute('/_app/console/announcements')({
  component: AnnouncementsPage,
})

type Listed = Schemas['AnnouncementListItem']
type Detail = Schemas['AnnouncementDetail']
type State = Listed['state']
interface Page {
  data: Listed[]
  meta: Schemas['PaginationMeta']
}

const kinds: AnnouncementKind[] = ['info', 'promotion', 'warning', 'critical']
const kindTones: Record<AnnouncementKind, BadgeTone> = { info: 'info', promotion: 'violet', warning: 'warn', critical: 'danger' }
const stateTones: Record<State, BadgeTone> = { live: 'ok', scheduled: 'info', ended: 'neutral', draft: 'neutral', archived: 'neutral' }
const channels = ['database', 'mail', 'push', 'sms'] as const
type StatusFilter = '' | 'published' | 'draft' | 'archived'

function useWhen() {
  const locale = intlLocale(useLocale())
  return (value?: string | null) => (value ? new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) : '—')
}

const orgCount = (t: ReturnType<typeof useT>, count: number) => (count === 1 ? t('announcements.orgOne') : t('announcements.orgCount', { count }))

function rate(part: number, whole: number) {
  return whole > 0 ? `${Math.round((part / whole) * 100)}%` : '—'
}

/** Staff announcements: one row each, whoever they reach, with how they did. */
function AnnouncementsPage() {
  const t = useT()
  const session = useSession()
  const when = useWhen()
  const [status, setStatus] = useState<StatusFilter>('')
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState(20)
  const [editing, setEditing] = useState<Detail | 'new' | null>(null)
  const [opened, setOpened] = useState<number | null>(null)
  const manage = can(session, 'announcement.manage')
  const list = useQuery({
    queryKey: ['console', 'announcements', status, page, perPage],
    queryFn: () => api<Page>('/internal/announcements', { asStaff: true, envelope: true, query: { status: status || undefined, page, per_page: perPage } }),
    placeholderData: keepPreviousData,
  })
  const rows = list.data?.data ?? []

  return (
    <>
      <PageHeader
        title={t('console.announcements')}
        description={t('announcements.subtitle')}
        actions={
          manage ? (
            <Button variant="primary" icon="plus" onClick={() => setEditing('new')}>
              {t('console.newAnnouncement')}
            </Button>
          ) : null
        }
      />
      <div className="toolbar">
        <div className="segmented">
          {(['', 'published', 'draft', 'archived'] as const).map((value) => (
            <button
              key={value}
              type="button"
              className={status === value ? 'active' : ''}
              onClick={() => {
                setStatus(value)
                setPage(1)
              }}
            >
              {t(value ? (`announcements.filter.${value}` as MessageKey) : 'announcements.filter.all')}
            </button>
          ))}
        </div>
      </div>
      <Card flush>
        {list.isLoading ? (
          <TableSkeleton columns={5} avatar={false} />
        ) : rows.length === 0 ? (
          <EmptyState icon="bell" title={t('console.noAnnouncements')} description={t('announcements.emptyHint')} />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>{t('console.colTitle')}</th>
                  <th>{t('announcements.colAudience')}</th>
                  <th>{t('announcements.colState')}</th>
                  <th>{t('announcements.colWindow')}</th>
                  <th className="right">{t('announcements.views')}</th>
                  <th className="right">{t('announcements.clicks')}</th>
                  <th className="right">{t('announcements.dismissals')}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((item) => (
                  <tr key={item.id} className="clickable" onClick={() => setOpened(item.id)}>
                    <td>
                      <span className="member-meta">
                        <span className="member-name">
                          {item.title} <Badge tone={kindTones[item.kind]}>{t(`announcements.kind.${item.kind}` as MessageKey)}</Badge>
                        </span>
                        {item.body ? <span className="small muted truncate maxw-sm">{item.body}</span> : null}
                      </span>
                    </td>
                    <td className="small">{item.audience === 'all' ? t('console.allOrganizations') : orgCount(t, item.tenant_count)}</td>
                    <td>
                      <Badge tone={stateTones[item.state]}>{t(`announcements.state.${item.state}` as MessageKey)}</Badge>
                    </td>
                    <td className="small muted nowrap">
                      {when(item.starts_at)}
                      <br />
                      {item.ends_at ? `→ ${when(item.ends_at)}` : t('announcements.noEnd')}
                    </td>
                    <td className="right num">{item.totals.views}</td>
                    <td className="right num">
                      {item.totals.clicks}
                      {item.cta_url ? <span className="small muted"> · {rate(item.totals.clicks, item.totals.views)}</span> : null}
                    </td>
                    <td className="right num">{item.totals.dismissals}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {list.data ? (
          <Pagination
            page={page}
            perPage={perPage}
            total={list.data.meta.total}
            onPage={setPage}
            onPerPage={(size) => {
              setPerPage(size)
              setPage(1)
            }}
          />
        ) : null}
      </Card>
      {opened != null ? (
        <AnnouncementDrawer
          id={opened}
          manage={manage}
          onEdit={(detail) => {
            setOpened(null)
            setEditing(detail)
          }}
          onClose={() => setOpened(null)}
        />
      ) : null}
      {editing ? <AnnouncementEditor current={editing === 'new' ? null : editing} onClose={() => setEditing(null)} /> : null}
    </>
  )
}

/** One announcement: a preview, how it did, and where it went. */
function AnnouncementDrawer({ id, manage, onEdit, onClose }: { id: number; manage: boolean; onEdit: (detail: Detail) => void; onClose: () => void }) {
  const t = useT()
  const when = useWhen()
  const client = useQueryClient()
  const { push } = useToast()
  const [confirm, setConfirm] = useState(false)
  const [busy, setBusy] = useState(false)
  const detail = useQuery({ queryKey: ['console', 'announcement', id], queryFn: () => api<Detail>(`/internal/announcements/${id}`, { asStaff: true }) })
  const data = detail.data

  const refresh = async () => {
    await client.invalidateQueries({ queryKey: ['console', 'announcements'] })
    await client.invalidateQueries({ queryKey: ['console', 'announcement', id] })
  }
  const setStatus = async (status: 'published' | 'archived') => {
    if (!data) return
    setBusy(true)
    try {
      await api(`/internal/announcements/${id}`, { method: 'PATCH', asStaff: true, body: { ...requestOf(data), status } })
      await refresh()
      push(t('form.saved'), 'success')
    } catch (cause) {
      push(cause instanceof Error ? cause.message : String(cause), 'error')
    } finally {
      setBusy(false)
    }
  }
  const remove = async () => {
    setBusy(true)
    try {
      await api(`/internal/announcements/${id}`, { method: 'DELETE', asStaff: true })
      await client.invalidateQueries({ queryKey: ['console', 'announcements'] })
      push(t('announcements.deleted'), 'success')
      onClose()
    } catch (cause) {
      push(cause instanceof Error ? cause.message : String(cause), 'error')
    } finally {
      setBusy(false)
      setConfirm(false)
    }
  }

  return (
    <Drawer
      open
      title={data?.title ?? '…'}
      subtitle={data ? `${t(`announcements.kind.${data.kind}` as MessageKey)} · ${t(`announcements.state.${data.state}` as MessageKey)}` : undefined}
      size="lg"
      onClose={onClose}
      actions={
        data && manage ? (
          <>
            <Button size="sm" icon="edit" onClick={() => onEdit(data)}>
              {t('announcements.edit')}
            </Button>
            {data.status === 'draft' ? (
              <Button size="sm" variant="primary" icon="bell" disabled={busy} onClick={() => void setStatus('published')}>
                {t('announcements.publish')}
              </Button>
            ) : data.status === 'archived' ? (
              <Button size="sm" disabled={busy} onClick={() => void setStatus('published')}>
                {t('announcements.publish')}
              </Button>
            ) : (
              <Button size="sm" disabled={busy} onClick={() => void setStatus('archived')}>
                {t('announcements.archive')}
              </Button>
            )}
            <Button size="sm" variant="danger" icon="trash" disabled={busy} onClick={() => setConfirm(true)}>
              {t('files.delete')}
            </Button>
          </>
        ) : null
      }
    >
      {!data ? (
        <Spinner />
      ) : (
        <div className="stack g-4">
          <div className="announcement-preview">
            <AnnouncementView announcement={data} />
          </div>
          <div className="dash-kpis">
            <StatTile label={t('announcements.views')} value={data.totals.views} hint={t('announcements.uniquePeople')} />
            <StatTile label={t('announcements.clicks')} value={data.totals.clicks} hint={data.cta_url ? t('announcements.clickRate', { rate: rate(data.totals.clicks, data.totals.views) }) : t('announcements.noButton')} />
            <StatTile label={t('announcements.dismissals')} value={data.totals.dismissals} hint={rate(data.totals.dismissals, data.totals.views)} />
          </div>
          <dl className="details">
            <dt>{t('announcements.colAudience')}</dt>
            <dd>{data.audience === 'all' ? t('console.allOrganizations') : data.tenants.map((tenant) => tenant.name).join(', ')}</dd>
            <dt>{t('announcements.starts')}</dt>
            <dd>{when(data.starts_at)}</dd>
            <dt>{t('announcements.ends')}</dt>
            <dd>{data.ends_at ? when(data.ends_at) : t('announcements.noEnd')}</dd>
            <dt>{t('announcements.channels')}</dt>
            <dd>
              {data.channels.length === 0
                ? t('announcements.bannerOnly')
                : `${data.channels.map((channel) => t(`announcements.channel.${channel}` as MessageKey)).join(', ')} · ${
                    data.delivered_at ? t('announcements.deliveredAt', { when: when(data.delivered_at) }) : t('announcements.notDelivered')
                  }`}
            </dd>
            <dt>{t('announcements.dismissible')}</dt>
            <dd>{data.dismissible ? t('announcements.yes') : t('announcements.no')}</dd>
            {data.cta_url ? (
              <>
                <dt>{t('announcements.button')}</dt>
                <dd>
                  {data.cta_label} ·{' '}
                  <a href={data.cta_url} target="_blank" rel="noopener noreferrer">
                    {data.cta_url}
                  </a>
                </dd>
              </>
            ) : null}
          </dl>
          <ByOrganization id={data.id} />
        </div>
      )}
      <ConfirmDialog
        open={confirm}
        title={t('files.delete')}
        description={t('announcements.deleteConfirm')}
        confirmLabel={t('files.delete')}
        danger
        busy={busy}
        onConfirm={() => void remove()}
        onCancel={() => setConfirm(false)}
      />
    </Drawer>
  )
}

interface TenantTotalsPage {
  data: Schemas['AnnouncementTenantTotals'][]
  meta: Schemas['PaginationMeta']
}

/** How an announcement did in each organization, a page at a time, by name. */
function ByOrganization({ id }: { id: number }) {
  const t = useT()
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState(10)
  const rows = useQuery({
    queryKey: ['console', 'announcement', id, 'organizations', search.trim(), page, perPage],
    queryFn: () =>
      api<TenantTotalsPage>(`/internal/announcements/${id}/organizations`, {
        asStaff: true,
        envelope: true,
        query: { search: search.trim() || undefined, page, per_page: perPage },
      }),
    placeholderData: keepPreviousData,
  })
  const items = rows.data?.data ?? []
  return (
    <Card
      title={t('announcements.byOrganization')}
      actions={
        <input
          className="input sm by-org-search"
          type="search"
          placeholder={t('announcements.searchOrgs')}
          aria-label={t('announcements.searchOrgs')}
          value={search}
          onChange={(e) => {
            setSearch(e.target.value)
            setPage(1)
          }}
        />
      }
      flush
    >
      {rows.isLoading ? (
        <TableSkeleton columns={4} avatar={false} />
      ) : items.length === 0 ? (
        <EmptyState icon="tenants" title={search.trim() ? t('announcements.noMatch') : t('announcements.noReach')} />
      ) : (
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <th>{t('dashboard.colOrganization')}</th>
                <th className="right">{t('announcements.views')}</th>
                <th className="right">{t('announcements.clicks')}</th>
                <th className="right">{t('announcements.dismissals')}</th>
              </tr>
            </thead>
            <tbody>
              {items.map((row) => (
                <tr key={row.id}>
                  <td>
                    <span className="org-cell">
                      <OrgMark name={row.name} />
                      {row.name}
                    </span>
                  </td>
                  <td className="right num">{row.views}</td>
                  <td className="right num">{row.clicks}</td>
                  <td className="right num">{row.dismissals}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {rows.data && rows.data.meta.total > 0 ? (
        <Pagination
          page={page}
          perPage={perPage}
          total={rows.data.meta.total}
          onPage={setPage}
          onPerPage={(size) => {
            setPerPage(size)
            setPage(1)
          }}
        />
      ) : null}
    </Card>
  )
}

/** The request body that keeps an announcement as it is (for status changes). */
function requestOf(data: Detail) {
  return {
    title: data.title,
    body: data.body,
    kind: data.kind,
    cta_label: data.cta_label,
    cta_url: data.cta_url,
    tenant_ids: data.audience === 'all' ? [] : data.tenants.map((tenant) => tenant.id),
    starts_at: data.starts_at,
    ends_at: data.ends_at,
    dismissible: data.dismissible,
    status: data.status,
    channels: data.channels,
  }
}

/** "2026-11-24" and "14:30" from a timestamp, in local time. */
function splitLocal(value?: string | null): [string, string] {
  if (!value) return ['', '']
  const date = new Date(value)
  const pad = (n: number) => String(n).padStart(2, '0')
  return [`${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`, `${pad(date.getHours())}:${pad(date.getMinutes())}`]
}

function joinLocal(day: string, time: string): string | null {
  if (!day) return null
  return new Date(`${day}T${time || '00:00'}:00`).toISOString()
}

/** Writing or changing an announcement, with a live preview of the banner. */
function AnnouncementEditor({ current, onClose }: { current: Detail | null; onClose: () => void }) {
  const t = useT()
  const client = useQueryClient()
  const { push } = useToast()
  const errors = useApiErrors()
  const tenants = useQuery(allTenantsQuery)
  const [title, setTitle] = useState(current?.title ?? '')
  const [body, setBody] = useState(current?.body ?? '')
  const [kind, setKind] = useState<AnnouncementKind>(current?.kind ?? 'info')
  const [ctaLabel, setCtaLabel] = useState(current?.cta_label ?? '')
  const [ctaUrl, setCtaUrl] = useState(current?.cta_url ?? '')
  const [everyone, setEveryone] = useState(current ? current.audience === 'all' : true)
  const [chosen, setChosen] = useState<number[]>(current?.tenants.map((tenant) => tenant.id) ?? [])
  const [search, setSearch] = useState('')
  const [[startDay, startTime], setStart] = useState(() => splitLocal(current?.starts_at))
  const [[endDay, endTime], setEnd] = useState(() => splitLocal(current?.ends_at))
  const [dismissible, setDismissible] = useState(current?.dismissible ?? true)
  const [picked, setPicked] = useState<string[]>(current?.channels ?? [])
  const [busy, setBusy] = useState(false)
  const delivered = !!current?.delivered_at
  const visible = useMemo(
    () => (tenants.data ?? []).filter((tenant) => tenant.is_active && tenant.name.toLowerCase().includes(search.trim().toLowerCase())),
    [tenants.data, search],
  )

  const save = async (status: 'published' | 'draft', event?: FormEvent) => {
    event?.preventDefault()
    setBusy(true)
    errors.clear()
    try {
      const body_ = {
        title: title.trim(),
        body: body.trim() || null,
        kind,
        cta_label: ctaLabel.trim() || null,
        cta_url: ctaUrl.trim() || null,
        tenant_ids: everyone ? [] : chosen,
        starts_at: joinLocal(startDay, startTime),
        ends_at: joinLocal(endDay, endTime),
        dismissible,
        status,
        channels: picked,
      }
      if (current) {
        await api(`/internal/announcements/${current.id}`, { method: 'PATCH', asStaff: true, body: body_ })
      } else {
        await api('/internal/announcements', { method: 'POST', asStaff: true, idempotent: true, body: body_ })
      }
      await client.invalidateQueries({ queryKey: ['console', 'announcements'] })
      if (current) await client.invalidateQueries({ queryKey: ['console', 'announcement', current.id] })
      push(status === 'draft' ? t('announcements.savedDraft') : t('announcements.published'), 'success')
      onClose()
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Drawer open title={current ? t('announcements.edit') : t('console.newAnnouncement')} size="lg" onClose={onClose}>
      <form onSubmit={(event) => void save('published', event)} className="stack g-4">
        <div className="announcement-preview">
          <span className="small muted">{t('announcements.preview')}</span>
          <AnnouncementView announcement={{ title, body: body || null, kind, cta_label: ctaLabel || null, cta_url: ctaUrl || null, dismissible }} />
        </div>

        <div className="field">
          <span className="field-label">{t('announcements.kind')}</span>
          <div className="segmented" role="group" aria-label={t('announcements.kind')}>
            {kinds.map((value) => (
              <button key={value} type="button" className={kind === value ? 'active' : ''} aria-pressed={kind === value} onClick={() => setKind(value)}>
                {t(`announcements.kind.${value}` as MessageKey)}
              </button>
            ))}
          </div>
        </div>
        <Field label={t('console.announceTitle')} error={errors.field('title')}>
          <input className="input" required maxLength={255} value={title} onChange={(e) => setTitle(e.target.value)} />
        </Field>
        <Field label={`${t('console.announceBody')} (${t('form.optional')})`} error={errors.field('body')}>
          <textarea className="input" rows={3} maxLength={5000} value={body} onChange={(e) => setBody(e.target.value)} />
        </Field>
        <div className="form-grid-2">
          <Field label={`${t('announcements.buttonLabel')} (${t('form.optional')})`} error={errors.field('cta_label')}>
            <input className="input" maxLength={60} placeholder={t('announcements.buttonLabelHint')} value={ctaLabel} onChange={(e) => setCtaLabel(e.target.value)} />
          </Field>
          <Field label={`${t('announcements.buttonLink')} (${t('form.optional')})`} error={errors.field('cta_url')}>
            <input className="input" type="url" placeholder="https://" value={ctaUrl} onChange={(e) => setCtaUrl(e.target.value)} />
          </Field>
        </div>

        <div className="stack-tight">
          <span className="field-label">{t('console.sendTo')}</span>
          <label className="check-row">
            <input type="radio" name="audience" checked={everyone} onChange={() => setEveryone(true)} />
            {t('console.allOrganizations')}
          </label>
          <label className="check-row">
            <input type="radio" name="audience" checked={!everyone} onChange={() => setEveryone(false)} />
            {t('console.chosenOrganizations')}
            {!everyone ? <span className="small muted">· {orgCount(t, chosen.length)}</span> : null}
          </label>
          {!everyone ? (
            <div className="audience-picker">
              <input className="input" placeholder={t('announcements.searchOrgs')} value={search} onChange={(e) => setSearch(e.target.value)} />
              <div className="audience-list">
                {visible.map((tenant) => (
                  <label key={tenant.id} className="check-row">
                    <input
                      type="checkbox"
                      checked={chosen.includes(tenant.id)}
                      onChange={(e) => setChosen((ids) => (e.target.checked ? [...ids, tenant.id] : ids.filter((id) => id !== tenant.id)))}
                    />
                    <OrgMark name={tenant.name} logo={tenant.branding?.logo_url} />
                    {tenant.name}
                  </label>
                ))}
              </div>
            </div>
          ) : null}
          {errors.field('tenant_ids') ? <span className="field-error">{errors.field('tenant_ids')}</span> : null}
        </div>

        <div className="form-grid-2">
          <div className="field">
            <span className="field-label">{t('announcements.starts')}</span>
            <div className="flex g-2">
              <DatePicker value={startDay} onChange={(day) => setStart([day, startTime])} placeholder={t('announcements.now')} ariaLabel={t('announcements.starts')} />
              <TimePicker value={startTime} onChange={(time) => setStart([startDay, time])} ariaLabel={t('announcements.starts')} />
            </div>
          </div>
          <div className="field">
            <span className="field-label">{t('announcements.ends')}</span>
            <div className="flex g-2">
              <DatePicker value={endDay} min={startDay || undefined} onChange={(day) => setEnd([day, endTime])} placeholder={t('announcements.noEnd')} ariaLabel={t('announcements.ends')} />
              <TimePicker value={endTime} onChange={(time) => setEnd([endDay, time])} ariaLabel={t('announcements.ends')} />
            </div>
            {errors.field('ends_at') ? <span className="field-error">{errors.field('ends_at')}</span> : null}
          </div>
        </div>

        <label className="check-row">
          <input type="checkbox" checked={dismissible} onChange={(e) => setDismissible(e.target.checked)} />
          {t('announcements.dismissibleLabel')}
        </label>

        <div className="stack-tight">
          <span className="field-label">{t('announcements.alsoSend')}</span>
          <div className="flex g-4 wrap">
            {channels.map((channel) => (
              <label key={channel} className="check-row">
                <input
                  type="checkbox"
                  disabled={delivered}
                  checked={picked.includes(channel)}
                  onChange={(e) => setPicked((list) => (e.target.checked ? [...list, channel] : list.filter((item) => item !== channel)))}
                />
                {t(`announcements.channel.${channel}` as MessageKey)}
              </label>
            ))}
          </div>
          <span className="small muted">{delivered ? t('announcements.alreadyDelivered') : t('announcements.alsoSendHint')}</span>
        </div>

        {errors.box}
        <div className="form-actions">
          <Button variant="ghost" type="button" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button type="button" disabled={busy || !title.trim()} onClick={() => void save('draft')}>
            {t('announcements.saveDraft')}
          </Button>
          <Button variant="primary" type="submit" disabled={busy || !title.trim() || (!everyone && chosen.length === 0)}>
            {busy ? <Spinner /> : <Icon name="bell" />} {current?.status === 'published' ? t('form.save') : t('announcements.publish')}
          </Button>
        </div>
      </form>
    </Drawer>
  )
}
