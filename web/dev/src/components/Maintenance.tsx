import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api } from '../api/client'
import type { DevMaintenance } from '../api/types'
import { relativeTime } from '../format'
import { useToast } from './Toasts'
import { Button, Card, CardHeader, Badge, ConfirmDialog, CopyButton, Modal, Spinner } from './ui'

const DEFAULT_MESSAGE = 'The service is down for maintenance. Please try again shortly.'

/** The maintenance state, shared by the top-bar pill and the System card. */
export function useMaintenance() {
  return useQuery({
    queryKey: ['maintenance'],
    queryFn: ({ signal }) => api<DevMaintenance>('/dev/api/maintenance', { signal }),
    refetchInterval: 15_000,
    staleTime: 5_000,
    retry: false,
  })
}

/** Red top-bar pill on every page while the API is down. */
export function MaintenancePill() {
  const { data } = useMaintenance()
  if (!data?.down) return null
  const since = data.since ? `since ${relativeTime(data.since)}` : ''
  return (
    <span className="env-pill maintenance-pill" title={`${data.message}${since ? ` (${since})` : ''}`}>
      <span className="dot red" /> Maintenance
    </span>
  )
}

/** Inspector → System: the maintenance state and its toggle (the routes need dev.manage). */
export function MaintenanceCard() {
  const state = useMaintenance()
  const queryClient = useQueryClient()
  const { push } = useToast()
  const [dialog, setDialog] = useState<'down' | 'up' | null>(null)
  const [message, setMessage] = useState(DEFAULT_MESSAGE)
  const [retry, setRetry] = useState('60')
  const [withSecret, setWithSecret] = useState(false)
  const [secret, setSecret] = useState<string | null>(null)

  const toggle = useMutation({
    mutationFn: (direction: 'down' | 'up') =>
      api<DevMaintenance>(`/dev/api/maintenance/${direction}`, {
        method: 'POST',
        body: direction === 'down' ? { message, retry_after: Number(retry) || 60, with_secret: withSecret } : {},
      }),
    onSuccess: (next, direction) => {
      queryClient.setQueryData(['maintenance'], { ...next, secret: undefined })
      void queryClient.invalidateQueries({ queryKey: ['health'] })
      setDialog(null)
      if (direction === 'down') {
        setSecret(next.secret ?? null)
        push('The API is now in maintenance mode', 'info')
      } else {
        setSecret(null)
        push('The API is live again', 'success')
      }
    },
    onError: (error) => push(error instanceof Error ? error.message : 'The maintenance mode could not be changed', 'error'),
  })

  const data = state.data
  const down = Boolean(data?.down)

  return (
    <Card className={`maintenance-card ${down ? 'down' : ''}`}>
      <CardHeader
        title="Maintenance mode"
        sub={
          down
            ? 'API clients get 503 MAINTENANCE_MODE. This dashboard, /up, /api/health and /docs keep working.'
            : 'Take the API down for an upgrade or migration; the same as `ims down`.'
        }
        actions={
          <div className="flex">
            {state.isLoading ? <Spinner /> : <Badge tone={down ? 'danger' : 'ok'}>{down ? 'down' : 'live'}</Badge>}
            {data ? (
              down ? (
                <Button variant="primary" icon="refresh" onClick={() => setDialog('up')}>
                  Bring back up
                </Button>
              ) : (
                <Button variant="danger" onClick={() => setDialog('down')}>
                  Take down…
                </Button>
              )
            ) : null}
          </div>
        }
      />
      {state.isError ? <div className="small dim">Maintenance state unavailable.</div> : null}
      {down && data ? (
        <dl className="runtime-pairs maintenance-pairs">
          <div><dt>Since</dt><dd title={data.since ?? ''}>{data.since ? relativeTime(data.since) : '—'}</dd></div>
          <div><dt>Retry-After</dt><dd>{data.retry_after}s</dd></div>
          <div><dt>Bypass</dt><dd>{data.has_secret ? 'secret set' : 'none'}</dd></div>
          <div><dt>By</dt><dd>{data.by_user_id ? `user #${data.by_user_id}` : 'CLI'}</dd></div>
          <div className="maintenance-message"><dt>Message</dt><dd>{data.message}</dd></div>
        </dl>
      ) : null}
      {secret ? (
        <div className="maintenance-secret">
          <div className="small">
            Bypass secret (shown once): send <code>X-Maintenance-Bypass: {secret}</code>
          </div>
          <CopyButton value={secret} label="Copy secret" />
        </div>
      ) : null}

      <Modal
        open={dialog === 'down'}
        title="Take the API down?"
        description="Every API process answers 503 within MAINTENANCE_CHECK_INTERVAL (5s by default). Queue workers keep running."
        onClose={() => setDialog(null)}
        actions={
          <>
            <Button variant="ghost" onClick={() => setDialog(null)} disabled={toggle.isPending}>
              Cancel
            </Button>
            <Button variant="danger" onClick={() => toggle.mutate('down')} disabled={toggle.isPending}>
              {toggle.isPending ? <Spinner /> : null}
              Take down
            </Button>
          </>
        }
      >
        <label className="field">
          <span className="field-label">Message for API clients</span>
          <textarea className="textarea" rows={2} maxLength={500} value={message} onChange={(event) => setMessage(event.target.value)} />
        </label>
        <label className="field mt-16">
          <span className="field-label">Retry after (seconds)</span>
          <input className="input" type="number" min={1} max={86400} value={retry} onChange={(event) => setRetry(event.target.value)} />
        </label>
        <label className="checkbox mt-16">
          <input type="checkbox" checked={withSecret} onChange={(event) => setWithSecret(event.target.checked)} />
          Generate a bypass secret (X-Maintenance-Bypass header)
        </label>
      </Modal>

      <ConfirmDialog
        open={dialog === 'up'}
        title="Bring the API back up?"
        description="API requests answer normally again on every process within the check interval."
        confirmLabel="Bring back up"
        busy={toggle.isPending}
        onConfirm={() => toggle.mutate('up')}
        onCancel={() => setDialog(null)}
      />
    </Card>
  )
}
