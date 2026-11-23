import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { api, type Schemas } from '../api/client'
import { prettyJSON } from '../format'
import { Field, useApiErrors } from '../components/form'
import { pageSlice, Pagination } from '../components/Pagination'
import { useToast } from '../components/Toasts'
import { Badge, Button, Card, ConfirmDialog, CopyButton, Drawer, EmptyState, Icon, Modal, PageHeader, Spinner } from '../components/ui'
import { intlLocale, useLocale, useT, type MessageKey } from '../i18n'
import { FormSkeleton, ListSkeleton } from '../components/Skeleton'

export const Route = createFileRoute('/_app/org/webhooks')({
  component: WebhooksPage,
})

type Endpoint = Schemas['WebhookEndpoint']
type EndpointWithSecret = Schemas['WebhookEndpointWithSecret']
type Delivery = Schemas['WebhookDelivery']
type WebhookEvent = Schemas['WebhookEvent']

const statusLabels: Record<Delivery['status'], MessageKey> = {
  pending: 'webhooks.status.pending',
  succeeded: 'webhooks.status.succeeded',
  abandoned: 'webhooks.status.abandoned',
}
const statusTones = { pending: 'warn', succeeded: 'ok', abandoned: 'danger' } as const

const eventsQuery = { queryKey: ['webhook-events'], queryFn: () => api<WebhookEvent[]>('/webhook/events'), staleTime: 300_000 }

function WebhooksPage() {
  const t = useT()
  const [creating, setCreating] = useState(false)
  const [selected, setSelected] = useState<number | null>(null)
  const [secret, setSecret] = useState<string | null>(null)
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState(20)
  const endpoints = useQuery({ queryKey: ['webhooks'], queryFn: () => api<Endpoint[]>('/webhook') })

  return (
    <>
      <PageHeader
        title={t('org.webhooks')}
        description={t('webhooks.subtitle')}
        actions={
          <Button variant="primary" icon="plus" onClick={() => setCreating(true)}>
            {t('webhooks.create')}
          </Button>
        }
      />
      <Card flush>
        {endpoints.isLoading ? (
          <ListSkeleton rows={3} />
        ) : (endpoints.data ?? []).length === 0 ? (
          <EmptyState icon="actions" title={t('webhooks.empty')} />
        ) : (
          <ul className="role-list">
            {pageSlice(endpoints.data!, page, perPage).map((endpoint) => (
              <li key={endpoint.id}>
                <button type="button" className="role-row" onClick={() => setSelected(endpoint.id)}>
                  <span className="role-meta">
                    <span className="member-name">
                      <span className="mono webhook-url">{endpoint.url}</span>
                      <EndpointState endpoint={endpoint} />
                    </span>
                    <span className="small muted">
                      {endpoint.description ? `${endpoint.description} · ` : ''}
                      {endpoint.events.includes('*') ? t('webhooks.allEvents') : endpoint.events.join(', ')}
                    </span>
                  </span>
                  <Icon name="chevron" />
                </button>
              </li>
            ))}
          </ul>
        )}
        {(endpoints.data ?? []).length > 0 ? (
          <Pagination
            page={page}
            perPage={perPage}
            total={endpoints.data!.length}
            onPage={setPage}
            onPerPage={(size) => {
              setPerPage(size)
              setPage(1)
            }}
          />
        ) : null}
      </Card>
      {creating ? (
        <EndpointForm
          onClose={() => setCreating(false)}
          onCreated={(created) => {
            setCreating(false)
            setSecret(created.secret)
            setSelected(created.id)
          }}
        />
      ) : null}
      {selected != null ? <EndpointDrawer id={selected} onSecret={setSecret} onClose={() => setSelected(null)} /> : null}
      <SecretDialog secret={secret} onClose={() => setSecret(null)} />
    </>
  )
}

function EndpointState({ endpoint }: { endpoint: Endpoint }) {
  const t = useT()
  if (!endpoint.is_active) return <Badge tone="neutral">{t('webhooks.disabled')}</Badge>
  if (endpoint.consecutive_failures > 0) return <Badge tone="warn">{t('webhooks.failures', { count: endpoint.consecutive_failures })}</Badge>
  return <Badge tone="ok">{t('webhooks.active')}</Badge>
}

function SecretDialog({ secret, onClose }: { secret: string | null; onClose: () => void }) {
  const t = useT()
  return (
    <Modal open={secret != null} title={t('webhooks.secretTitle')} description={t('webhooks.secretHint')} onClose={onClose}>
      <div className="stack">
        <div className="secret-box">
          <code className="mono">{secret}</code>
          <CopyButton value={secret ?? ''} />
        </div>
        <div className="modal-actions">
          <Button variant="primary" onClick={onClose}>
            {t('common.close')}
          </Button>
        </div>
      </div>
    </Modal>
  )
}

/** Picks event types, or `*` for every event. */
function EventPicker({ value, onChange }: { value: string[]; onChange: (events: string[]) => void }) {
  const t = useT()
  const events = useQuery(eventsQuery)
  const all = value.includes('*')
  return (
    <div className="stack-tight">
      <label className="check-row">
        <input type="checkbox" checked={all} onChange={(e) => onChange(e.target.checked ? ['*'] : [])} />
        {t('webhooks.allEvents')}
      </label>
      {!all ? (
        <div className="event-picker">
          {(events.data ?? []).map((event) => (
            <label key={event.type} className="check-row permission-row">
              <input
                type="checkbox"
                checked={value.includes(event.type)}
                onChange={(e) => onChange(e.target.checked ? [...value, event.type] : value.filter((item) => item !== event.type))}
              />
              <span className="permission-meta">
                <span className="mono">{event.type}</span>
                <span className="small muted">{event.description}</span>
              </span>
            </label>
          ))}
        </div>
      ) : null}
    </div>
  )
}

function EndpointForm({ endpoint, onClose, onCreated }: { endpoint?: Endpoint; onClose: () => void; onCreated?: (created: EndpointWithSecret) => void }) {
  const t = useT()
  const client = useQueryClient()
  const { push } = useToast()
  const errors = useApiErrors()
  const [url, setUrl] = useState(endpoint?.url ?? 'https://')
  const [description, setDescription] = useState(endpoint?.description ?? '')
  const [events, setEvents] = useState<string[]>(endpoint?.events ?? ['*'])
  const [busy, setBusy] = useState(false)

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    errors.clear()
    const body = { url: url.trim(), description: description.trim(), events }
    try {
      if (endpoint) {
        await api(`/webhook/${endpoint.id}`, { method: 'PATCH', body })
        push(t('form.saved'), 'success')
      } else {
        const created = await api<EndpointWithSecret>('/webhook', { method: 'POST', idempotent: true, body })
        onCreated?.(created)
      }
      await client.invalidateQueries({ queryKey: ['webhooks'] })
      await client.invalidateQueries({ queryKey: ['webhook', endpoint?.id] })
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  const form = (
    <form onSubmit={submit} className="stack">
      <Field label={t('webhooks.url')} hint={t('webhooks.urlHint')} error={errors.field('url')}>
        <input className="input mono" type="url" name="url" autoComplete="off" required value={url} onChange={(e) => setUrl(e.target.value)} />
      </Field>
      <Field label={`${t('webhooks.description')} (${t('form.optional')})`} error={errors.field('description')}>
        <input className="input" name="description" autoComplete="off" value={description} onChange={(e) => setDescription(e.target.value)} />
      </Field>
      <Field label={t('webhooks.events')} error={errors.field('events')}>
        <EventPicker value={events} onChange={setEvents} />
      </Field>
      {errors.box}
      <div className={endpoint ? '' : 'modal-actions'}>
        {endpoint ? null : (
          <Button variant="ghost" type="button" onClick={onClose}>
            {t('common.cancel')}
          </Button>
        )}
        <Button variant="primary" type="submit" disabled={busy || events.length === 0 || url.trim().length < 9}>
          {busy ? <Spinner /> : <Icon name="check" />} {endpoint ? t('webhooks.save') : t('webhooks.create')}
        </Button>
      </div>
    </form>
  )
  if (endpoint) return form
  return (
    <Modal open title={t('webhooks.createTitle')} onClose={onClose}>
      {form}
    </Modal>
  )
}

function EndpointDrawer({ id, onSecret, onClose }: { id: number; onSecret: (secret: string) => void; onClose: () => void }) {
  const t = useT()
  const client = useQueryClient()
  const { push } = useToast()
  const [busy, setBusy] = useState(false)
  const [confirm, setConfirm] = useState<'rotate' | 'delete' | null>(null)
  const endpoint = useQuery({ queryKey: ['webhook', id], queryFn: () => api<Endpoint>(`/webhook/${id}`) })
  const deliveries = useQuery({ queryKey: ['webhook-deliveries', id], queryFn: () => api<Delivery[]>(`/webhook/${id}/deliveries`, { query: { limit: 25 } }) })

  const refresh = async () => {
    await client.invalidateQueries({ queryKey: ['webhooks'] })
    await client.invalidateQueries({ queryKey: ['webhook', id] })
    await client.invalidateQueries({ queryKey: ['webhook-deliveries', id] })
  }

  const run = async (action: () => Promise<void>) => {
    setBusy(true)
    try {
      await action()
      await refresh()
    } catch (cause) {
      push(cause instanceof Error ? cause.message : String(cause), 'error')
    } finally {
      setBusy(false)
      setConfirm(null)
    }
  }

  const answered = (delivery: Delivery) => (delivery.response_status ? `HTTP ${delivery.response_status}` : (delivery.error ?? '—'))

  const data = endpoint.data
  return (
    <Drawer open title={data?.url ?? '…'} subtitle={data?.description || undefined} onClose={onClose} size="lg">
      {!data ? (
        <FormSkeleton fields={3} />
      ) : (
        <div className="stack g-4">
          <div className="flex wrap">
            <EndpointState endpoint={data} />
            {data.disabled_reason ? <span className="small muted">{data.disabled_reason}</span> : null}
          </div>
          <div className="flex wrap">
            <Button
              icon="actions"
              disabled={busy}
              onClick={() =>
                void run(async () => {
                  const delivery = await api<Delivery>(`/webhook/${id}/test`, { method: 'POST' })
                  push(t('webhooks.testSent', { status: answered(delivery) }), delivery.status === 'succeeded' ? 'success' : 'error')
                })
              }
            >
              {t('webhooks.test')}
            </Button>
            <Button
              icon={data.is_active ? 'pause' : 'play'}
              disabled={busy}
              onClick={() =>
                void run(async () => {
                  await api(`/webhook/${id}`, { method: 'PATCH', body: { is_active: !data.is_active } })
                  push(t('form.saved'), 'success')
                })
              }
            >
              {data.is_active ? t('webhooks.disable') : t('webhooks.enable')}
            </Button>
            <Button icon="key" disabled={busy} onClick={() => setConfirm('rotate')}>
              {t('webhooks.rotate')}
            </Button>
            <span className="spacer" />
            <Button variant="danger" icon="trash" disabled={busy} onClick={() => setConfirm('delete')}>
              {t('webhooks.delete')}
            </Button>
          </div>
          <Card title={t('webhooks.endpoint')}>
            <EndpointForm key={data.updated_at} endpoint={data} onClose={onClose} />
          </Card>
          <Card title={t('webhooks.deliveries')} flush>
            {!deliveries.data ? (
              <ListSkeleton rows={4} />
            ) : deliveries.data.length === 0 ? (
              <EmptyState icon="queue" title={t('webhooks.noDeliveries')} />
            ) : (
              <ul className="activity-list">
                {deliveries.data.map((delivery) => (
                  <DeliveryRow
                    key={delivery.id}
                    delivery={delivery}
                    answered={answered(delivery)}
                    busy={busy}
                    onRedeliver={() =>
                      void run(async () => {
                        const again = await api<Delivery>(`/webhook/deliveries/${delivery.id}/redeliver`, { method: 'POST' })
                        push(t('webhooks.redelivered', { status: answered(again) }), again.status === 'succeeded' ? 'success' : 'error')
                      })
                    }
                  />
                ))}
              </ul>
            )}
          </Card>
        </div>
      )}
      <ConfirmDialog
        open={confirm === 'rotate'}
        title={t('webhooks.rotate')}
        description={t('webhooks.rotateConfirm')}
        confirmLabel={t('webhooks.rotate')}
        busy={busy}
        onConfirm={() =>
          void run(async () => {
            const rotated = await api<EndpointWithSecret>(`/webhook/${id}/rotate-secret`, { method: 'POST' })
            onSecret(rotated.secret)
          })
        }
        onCancel={() => setConfirm(null)}
      />
      <ConfirmDialog
        open={confirm === 'delete'}
        title={t('webhooks.delete')}
        description={t('webhooks.deleteConfirm')}
        confirmLabel={t('webhooks.delete')}
        danger
        busy={busy}
        onConfirm={() =>
          void run(async () => {
            await api(`/webhook/${id}`, { method: 'DELETE' })
            push(t('members.done'), 'success')
            onClose()
          })
        }
        onCancel={() => setConfirm(null)}
      />
    </Drawer>
  )
}

function DeliveryRow({ delivery, answered, busy, onRedeliver }: { delivery: Delivery; answered: string; busy: boolean; onRedeliver: () => void }) {
  const t = useT()
  const locale = useLocale()
  const [open, setOpen] = useState(false)
  const when = new Intl.DateTimeFormat(intlLocale(locale), { dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(delivery.created_at))
  return (
    <li className="activity-row">
      <div className="activity-body">
        <div className="activity-line">
          <Badge tone={statusTones[delivery.status]}>{t(statusLabels[delivery.status])}</Badge>
          <code className="activity-action">{delivery.event_type}</code>
          <span className="small muted">{answered}</span>
          <span className="spacer" />
          <Button size="sm" variant="ghost" icon="refresh" disabled={busy} onClick={onRedeliver}>
            {t('webhooks.redeliver')}
          </Button>
        </div>
        <div className="small muted">
          {when} · {t('webhooks.attempts', { count: delivery.attempts })}
          {delivery.duration_ms != null ? ` · ${delivery.duration_ms} ms` : ''}
        </div>
        <button type="button" className="link-button" aria-expanded={open} onClick={() => setOpen((value) => !value)}>
          {t('activity.changes')}
        </button>
        {open ? (
          <div className="stack-tight">
            <div className="small strong">{t('webhooks.payload')}</div>
            <pre className="activity-changes">{prettyJSON(delivery.payload)}</pre>
            {delivery.response_body ? (
              <>
                <div className="small strong">{t('webhooks.response')}</div>
                <pre className="activity-changes">{delivery.response_body}</pre>
              </>
            ) : null}
          </div>
        ) : null}
      </div>
    </li>
  )
}
