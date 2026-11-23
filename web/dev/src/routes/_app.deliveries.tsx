import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useEffect, useMemo, useState } from 'react'
import { api } from '../api/client'
import type { CursorPage, DevDelivery, DevDeliveryContent, DevDeliveryTemplates } from '../api/types'
import { Select } from '../components/inputs'
import {
  Badge,
  Button,
  Card,
  Cluster,
  CopyButton,
  Drawer,
  EmptyState,
  ErrorBox,
  LoadingBlock,
  PageHeader,
  RefreshToggle,
  Split,
  Stack,
  Tabs,
  type BadgeTone,
} from '../components/ui'
import { formatDateTime, relativeTime } from '../format'

/* Deliveries (phase 26.2): the delivery log — every message sent by mail, SMS
 * or push, one row per channel, never a body — and template previews rendered
 * with each template's sample data. */

type DeliveryTab = 'log' | 'templates'
type Channel = 'mail' | 'sms' | 'push'

interface DeliverySearch {
  tab?: DeliveryTab
  channel?: Channel
  status?: DevDelivery['status']
  template?: string
}

const channels: Channel[] = ['mail', 'sms', 'push']
const statuses: DevDelivery['status'][] = ['queued', 'sent', 'failed', 'skipped']

export const Route = createFileRoute('/_app/deliveries')({
  validateSearch: (search: Record<string, unknown>): DeliverySearch => ({
    tab: search.tab === 'templates' ? 'templates' : undefined,
    channel: channels.includes(search.channel as Channel) ? (search.channel as Channel) : undefined,
    status: statuses.includes(search.status as DevDelivery['status']) ? (search.status as DevDelivery['status']) : undefined,
    template: typeof search.template === 'string' && search.template ? search.template : undefined,
  }),
  component: DeliveriesPage,
})

function statusTone(status: DevDelivery['status']): BadgeTone {
  switch (status) {
    case 'sent':
      return 'ok'
    case 'failed':
      return 'danger'
    case 'queued':
      return 'info'
    default:
      return 'neutral'
  }
}

function DeliveriesPage() {
  const search = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })
  const tab = search.tab ?? 'log'
  const templates = useQuery({
    queryKey: ['deliveries', 'templates'],
    queryFn: ({ signal }) => api<DevDeliveryTemplates>('/dev/api/deliveries/templates', { signal }),
  })

  return (
    <>
      <PageHeader
        title="Deliveries"
        description="Every message sent by mail, SMS or push, and a preview of each template. Bodies are never stored."
        meta={
          (templates.data?.channels ?? []).length > 0 ? (
            <Cluster gap={1}>
              {(templates.data?.channels ?? []).map((channel) => (
                <Badge key={channel.channel} title={`${channel.channel} goes through the ${channel.driver} driver`}>
                  {channel.channel} · {channel.driver}
                </Badge>
              ))}
            </Cluster>
          ) : undefined
        }
      />
      <Tabs
        tabs={[
          { id: 'log', label: 'Log' },
          { id: 'templates', label: 'Templates' },
        ]}
        active={tab}
        onChange={(next) => void navigate({ search: next === 'log' ? {} : { tab: next }, replace: true })}
      />
      {tab === 'log' ? <DeliveryLog search={search} templates={templates.data} /> : <TemplatePreview templates={templates.data} initial={search.template} />}
    </>
  )
}

function DeliveryLog({ search, templates }: { search: DeliverySearch; templates?: DevDeliveryTemplates }) {
  const navigate = useNavigate({ from: Route.fullPath })
  const [live, setLive] = useState(false)
  const [selected, setSelected] = useState<DevDelivery | null>(null)
  const setFilter = (patch: Partial<DeliverySearch>) =>
    void navigate({ search: (previous) => ({ ...previous, ...patch, tab: undefined }), replace: true })

  const query = useInfiniteQuery({
    queryKey: ['deliveries', 'log', search.channel, search.status, search.template],
    queryFn: ({ pageParam, signal }) =>
      api<CursorPage<DevDelivery>>('/dev/api/deliveries', {
        query: {
          limit: 25,
          cursor: pageParam || undefined,
          channel: search.channel,
          status: search.status,
          template: search.template,
        },
        signal,
        unwrap: false,
      }),
    initialPageParam: '',
    getNextPageParam: (lastPage) => lastPage.meta?.next_cursor || undefined,
    refetchInterval: live ? 5000 : false,
  })
  const rows = useMemo(() => query.data?.pages.flatMap((page) => page.data) ?? [], [query.data])
  const filtered = Boolean(search.channel || search.status || search.template)

  return (
    <>
      <div className="filters">
        <label className="field">
          <span className="field-label">Channel</span>
          <Select
            value={search.channel ?? ''}
            onChange={(value) => setFilter({ channel: (value || undefined) as Channel | undefined })}
            options={[{ value: '', label: 'Any channel' }, ...channels.map((channel) => ({ value: channel, label: channel }))]}
          />
        </label>
        <label className="field">
          <span className="field-label">Status</span>
          <Select
            value={search.status ?? ''}
            onChange={(value) => setFilter({ status: (value || undefined) as DevDelivery['status'] | undefined })}
            options={[{ value: '', label: 'Any status' }, ...statuses.map((status) => ({ value: status, label: status }))]}
          />
        </label>
        <label className="field">
          <span className="field-label">Template</span>
          <Select
            value={search.template ?? ''}
            onChange={(value) => setFilter({ template: value || undefined })}
            options={[
              { value: '', label: 'Any template' },
              ...(templates?.templates ?? []).map((template) => ({ value: template.name, label: template.name })),
              { value: 'raw', label: 'raw (no template)' },
            ]}
          />
        </label>
        {filtered ? (
          <Button variant="ghost" icon="x" onClick={() => void navigate({ search: {}, replace: true })}>
            Clear
          </Button>
        ) : null}
        <div className="spacer" />
        <RefreshToggle enabled={live} onChange={setLive} />
      </div>

      <Card flush>
        {query.isLoading ? (
          <LoadingBlock label="Loading deliveries…" />
        ) : query.isError ? (
          <ErrorBox
            message={query.error instanceof Error ? query.error.message : 'Unable to load deliveries.'}
            onRetry={() => void query.refetch()}
          />
        ) : rows.length === 0 ? (
          <EmptyState
            title={filtered ? 'Nothing matches' : 'Nothing sent yet'}
            description={
              filtered
                ? 'Clear a filter to see more.'
                : 'One-time codes, password resets and every other message appear here, one row per channel. Send one with the delivery:test command.'
            }
            icon="mail"
          />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>When</th>
                  <th>Channel</th>
                  <th>Template</th>
                  <th>To</th>
                  <th>Status</th>
                  <th className="right">Attempts</th>
                  <th>Error</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <tr key={row.id} className={`clickable ${selected?.id === row.id ? 'selected' : ''}`} onClick={() => setSelected(row)}>
                    <td className="small dim nowrap" title={formatDateTime(row.created_at)}>
                      {relativeTime(row.created_at)}
                    </td>
                    <td className="small">{row.channel}</td>
                    <td className="mono small">{row.template}</td>
                    <td className="mono small dim">{row.address || '—'}</td>
                    <td>
                      <Badge tone={statusTone(row.status)}>{row.status}</Badge>
                    </td>
                    <td className="small dim right num">{row.attempts}</td>
                    <td className="small dim truncate delivery-error" title={row.error ?? undefined}>
                      {row.error ?? '—'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
      {query.hasNextPage ? (
        <Cluster gap={3} className="mt-16">
          <Button icon="chevron" onClick={() => void query.fetchNextPage()} disabled={query.isFetchingNextPage}>
            {query.isFetchingNextPage ? 'Loading…' : 'Load more'}
          </Button>
        </Cluster>
      ) : null}

      <Drawer
        open={selected !== null}
        size="md"
        title={selected ? `${selected.channel} · ${selected.template}` : 'Delivery'}
        subtitle={selected ? `Delivery #${selected.id} · ${relativeTime(selected.created_at)}` : undefined}
        onClose={() => setSelected(null)}
      >
        {selected ? (
          <Stack gap={4}>
            {selected.error ? <ErrorBox message={selected.error} /> : null}
            <dl className="kv">
              <dt>Status</dt>
              <dd>
                <Badge tone={statusTone(selected.status)}>{selected.status}</Badge>
              </dd>
              <dt>To</dt>
              <dd>{selected.address || '—'}</dd>
              <dt>Attempts</dt>
              <dd>{selected.attempts}</dd>
              <dt>Queued</dt>
              <dd>{formatDateTime(selected.created_at)}</dd>
              <dt>Sent</dt>
              <dd>{selected.sent_at ? formatDateTime(selected.sent_at) : '—'}</dd>
              <dt>User / tenant</dt>
              <dd>
                {selected.user_id ?? '—'} / {selected.tenant_id ?? '—'}
              </dd>
              <dt>Provider ID</dt>
              <dd>
                {selected.provider_message_id ? (
                  <Cluster gap={1}>
                    {selected.provider_message_id}
                    <CopyButton value={selected.provider_message_id} />
                  </Cluster>
                ) : (
                  '—'
                )}
              </dd>
            </dl>
            <p className="small dim">Addresses are masked and bodies are never stored; preview the template to see the content.</p>
            {selected.template !== 'raw' ? (
              <Button icon="eye" onClick={() => void navigate({ search: { tab: 'templates', template: selected.template }, replace: true })}>
                Preview {selected.template}
              </Button>
            ) : null}
          </Stack>
        ) : null}
      </Drawer>
    </>
  )
}

function TemplatePreview({ templates, initial }: { templates?: DevDeliveryTemplates; initial?: string }) {
  const list = templates?.templates ?? []
  const [name, setName] = useState(initial ?? '')
  const [channel, setChannel] = useState<Channel>('mail')
  const [locale, setLocale] = useState('en')
  const selected = list.find((template) => template.name === name) ?? list[0]

  useEffect(() => {
    if (selected && !selected.channels.includes(channel)) setChannel(selected.channels[0] as Channel)
  }, [selected, channel])

  const preview = useQuery({
    queryKey: ['deliveries', 'preview', selected?.name, channel, locale],
    enabled: Boolean(selected),
    queryFn: ({ signal }) =>
      api<DevDeliveryContent>(`/dev/api/deliveries/templates/${encodeURIComponent(selected!.name)}/preview`, {
        query: { channel, locale },
        signal,
      }),
  })

  if (!templates) return <LoadingBlock label="Loading templates…" />
  if (list.length === 0) {
    return (
      <Card flush>
        <EmptyState title="No templates" description="Register templates on the delivery service to preview them here." icon="mail" />
      </Card>
    )
  }

  return (
    <Split
      asideWidth="sm"
      aside={
        <Card title="Templates" description={`${list.length} registered`} flush>
          <div className="delivery-template-list">
            {list.map((template) => (
              <button
                type="button"
                key={template.name}
                className={`delivery-template ${template.name === selected?.name ? 'active' : ''}`}
                onClick={() => setName(template.name)}
              >
                <span className="mono">{template.name}</span>
                <span className="small dim">{template.channels.join(' · ')}</span>
              </button>
            ))}
          </div>
        </Card>
      }
    >
      <Card
        title={<span className="mono">{selected?.name}</span>}
        description="Rendered with the template's sample data. Nothing is sent."
        flush
      >
        <div className="filters delivery-preview-filters">
          <label className="field">
            <span className="field-label">Channel</span>
            <Select
              value={channel}
              onChange={(value) => setChannel(value as Channel)}
              options={(selected?.channels ?? []).map((value) => ({ value, label: value }))}
            />
          </label>
          <label className="field">
            <span className="field-label">Locale</span>
            <Select
              value={locale}
              onChange={setLocale}
              options={[
                { value: 'en', label: 'English (en)' },
                { value: 'bn', label: 'বাংলা (bn)' },
              ]}
            />
          </label>
        </div>
        {preview.isLoading ? (
          <LoadingBlock label="Rendering…" />
        ) : preview.isError ? (
          <ErrorBox message={preview.error instanceof Error ? preview.error.message : 'Unable to render the template.'} />
        ) : preview.data ? (
          <div className="delivery-rendered">
            {preview.data.subject ? (
              <div className="delivery-subject">
                <span className="field-label">Subject</span> {preview.data.subject}
              </div>
            ) : null}
            {preview.data.title ? (
              <div className="delivery-subject">
                <span className="field-label">Title</span> {preview.data.title}
              </div>
            ) : null}
            {preview.data.html ? (
              <iframe title="HTML preview" className="delivery-frame" sandbox="" srcDoc={preview.data.html} />
            ) : null}
            <span className="field-label">{channel === 'mail' ? 'Plain text' : 'Text'}</span>
            <pre className="delivery-text">{preview.data.text}</pre>
          </div>
        ) : null}
      </Card>
    </Split>
  )
}
