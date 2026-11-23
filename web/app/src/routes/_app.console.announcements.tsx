import { createFileRoute } from '@tanstack/react-router'
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState, type FormEvent } from 'react'
import { api, type Schemas } from '../api/client'
import { can, useSession } from '../auth/session'
import { allTenantsQuery } from '../components/console'
import { Field, useApiErrors } from '../components/form'
import { Select } from '../components/inputs'
import { Pagination } from '../components/Pagination'
import { useToast } from '../components/Toasts'
import { Badge, Button, Card, EmptyState, Icon, Modal, PageHeader, Spinner, type BadgeTone } from '../components/ui'
import { intlLocale, useLocale, useT, type MessageKey } from '../i18n'
import { TableSkeleton } from '../components/Skeleton'

export const Route = createFileRoute('/_app/console/announcements')({
  component: AnnouncementsPage,
})

type Notification = Schemas['Notification']
interface NotificationPage {
  data: Notification[]
  meta: Schemas['PaginationMeta']
}
type Priority = 'INFO' | 'WARNING' | 'CRITICAL'
const priorityTones: Record<Priority, BadgeTone> = { INFO: 'info', WARNING: 'warn', CRITICAL: 'danger' }

function AnnouncementsPage() {
  const t = useT()
  const session = useSession()
  const locale = useLocale()
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState(20)
  const [writing, setWriting] = useState(false)
  const tenants = useQuery(allTenantsQuery)
  const names = useMemo(() => new Map((tenants.data ?? []).map((tenant) => [tenant.id, tenant.name])), [tenants.data])
  const sent = useQuery({
    queryKey: ['console', 'announcements', page, perPage],
    queryFn: () => api<NotificationPage>('/internal/notification', { asStaff: true, envelope: true, query: { page, per_page: perPage, event_type: 'platform.announcement' } }),
    placeholderData: keepPreviousData,
  })
  const when = (value: string) => new Intl.DateTimeFormat(intlLocale(locale), { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
  const rows = sent.data?.data ?? []

  return (
    <>
      <PageHeader
        title={t('console.announcements')}
        description={t('console.announcementsSubtitle')}
        actions={
          can(session, 'notification.create') ? (
            <Button variant="primary" icon="plus" onClick={() => setWriting(true)}>
              {t('console.newAnnouncement')}
            </Button>
          ) : null
        }
      />
      <Card title={t('console.sentHistory')} flush>
        {sent.isLoading ? (
          <TableSkeleton columns={4} avatar={false} />
        ) : rows.length === 0 ? (
          <EmptyState icon="bell" title={t('console.noAnnouncements')} />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>{t('console.colTitle')}</th>
                  <th>{t('console.colOrg')}</th>
                  <th>{t('console.priority')}</th>
                  <th>{t('console.colSent')}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((item) => (
                  <tr key={item.id}>
                    <td>
                      <span className="member-meta">
                        <span className="member-name">{item.title}</span>
                        {item.body ? <span className="small muted truncate maxw-sm">{item.body}</span> : null}
                      </span>
                    </td>
                    <td className="small">{item.tenant_id ? (names.get(item.tenant_id) ?? `#${item.tenant_id}`) : '—'}</td>
                    <td>
                      <Badge tone={priorityTones[item.priority as Priority] ?? 'neutral'}>{t(`console.priority.${item.priority}` as MessageKey)}</Badge>
                    </td>
                    <td className="small muted">{when(item.created_at)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {sent.data ? (
          <Pagination
            page={page}
            perPage={perPage}
            total={sent.data.meta.total}
            onPage={setPage}
            onPerPage={(size) => {
              setPerPage(size)
              setPage(1)
            }}
          />
        ) : null}
      </Card>
      {writing ? <AnnouncementForm onClose={() => setWriting(false)} /> : null}
    </>
  )
}

function AnnouncementForm({ onClose }: { onClose: () => void }) {
  const t = useT()
  const client = useQueryClient()
  const { push } = useToast()
  const errors = useApiErrors()
  const tenants = useQuery(allTenantsQuery)
  const [title, setTitle] = useState('')
  const [body, setBody] = useState('')
  const [priority, setPriority] = useState<Priority>('INFO')
  const [everyone, setEveryone] = useState(true)
  const [chosen, setChosen] = useState<number[]>([])
  const [email, setEmail] = useState(false)
  const [busy, setBusy] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    errors.clear()
    try {
      const result = await api<{ organizations: number }>('/internal/notification/broadcast', {
        method: 'POST',
        asStaff: true,
        idempotent: true,
        body: {
          priority,
          title: title.trim(),
          body: body.trim() || null,
          channels: email ? ['mail'] : [],
          tenant_ids: everyone ? [] : chosen,
        },
      })
      await client.invalidateQueries({ queryKey: ['console', 'announcements'] })
      push(t('console.sent', { count: result.organizations }), 'success')
      onClose()
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open title={t('console.newAnnouncement')} onClose={onClose}>
      <form onSubmit={submit} className="stack">
        <Field label={t('console.announceTitle')} error={errors.field('title')}>
          <input className="input" required maxLength={255} value={title} onChange={(e) => setTitle(e.target.value)} />
        </Field>
        <Field label={`${t('console.announceBody')} (${t('form.optional')})`} error={errors.field('body')}>
          <textarea className="input" rows={4} value={body} onChange={(e) => setBody(e.target.value)} />
        </Field>
        <Field label={t('console.priority')} error={errors.field('priority')}>
          <Select
            value={priority}
            ariaLabel={t('console.priority')}
            onChange={setPriority}
            options={(['INFO', 'WARNING', 'CRITICAL'] as const).map((value) => ({ value, label: t(`console.priority.${value}`) }))}
          />
        </Field>
        <div className="stack-tight">
          <span className="field-label">{t('console.sendTo')}</span>
          <label className="check-row">
            <input type="radio" name="audience" checked={everyone} onChange={() => setEveryone(true)} />
            {t('console.allOrganizations')}
          </label>
          <label className="check-row">
            <input type="radio" name="audience" checked={!everyone} onChange={() => setEveryone(false)} />
            {t('console.chosenOrganizations')}
          </label>
          {!everyone ? (
            <div className="role-picker">
              {(tenants.data ?? [])
                .filter((tenant) => tenant.is_active)
                .map((tenant) => (
                  <label key={tenant.id} className="check-row">
                    <input
                      type="checkbox"
                      checked={chosen.includes(tenant.id)}
                      onChange={(e) => setChosen((current) => (e.target.checked ? [...current, tenant.id] : current.filter((id) => id !== tenant.id)))}
                    />
                    {tenant.name}
                  </label>
                ))}
            </div>
          ) : null}
          {errors.field('tenant_ids') ? <span className="field-error">{errors.field('tenant_ids')}</span> : null}
        </div>
        <label className="check-row">
          <input type="checkbox" checked={email} onChange={(e) => setEmail(e.target.checked)} />
          {t('console.alsoEmail')}
        </label>
        {errors.box}
        <div className="modal-actions">
          <Button variant="ghost" type="button" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" type="submit" disabled={busy || !title.trim() || (!everyone && chosen.length === 0)}>
            {busy ? <Spinner /> : <Icon name="bell" />} {t('console.send')}
          </Button>
        </div>
      </form>
    </Modal>
  )
}
