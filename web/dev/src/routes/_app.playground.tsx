import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'
import { api, fetchMe, getCachedMe } from '../api/client'
import type { PlaygroundInfo, PlaygroundQueueResult } from '../api/types'
import { Select } from '../components/inputs'
import { useToast } from '../components/Toasts'
import {
  Button,
  Card,
  CardHeader,
  Badge,
  EmptyState,
  ErrorBox,
  Icon,
  LoadingBlock,
  PageHeader,
} from '../components/ui'

export const Route = createFileRoute('/_app/playground')({
  component: PlaygroundPage,
})

type PlaygroundJobKind = 'dev.test' | 'user.export' | 'mail.send'

interface QueueJobRequest {
  kind: PlaygroundJobKind
  queue?: string
  count?: number
  sleep_ms?: number
  fail?: boolean
  tag?: string
  to?: string
  subject?: string
  text?: string
}

const presets: { label: string; description: string; request: QueueJobRequest }[] = [
  {
    label: '1 default job',
    description: 'Fast job on the default queue.',
    request: { kind: 'dev.test', queue: 'default', count: 1, sleep_ms: 0, fail: false, tag: 'playground' },
  },
  {
    label: '5 default jobs',
    description: 'Fill the queue list with a small batch.',
    request: { kind: 'dev.test', queue: 'default', count: 5, sleep_ms: 0, fail: false, tag: 'playground-batch' },
  },
  {
    label: '1 heavy job',
    description: 'Job on the heavy queue.',
    request: { kind: 'dev.test', queue: 'heavy', count: 1, sleep_ms: 0, fail: false, tag: 'playground-heavy' },
  },
  {
    label: 'Slow job (2s)',
    description: 'Watch the running state in Queue.',
    request: { kind: 'dev.test', queue: 'default', count: 1, sleep_ms: 2000, fail: false, tag: 'playground-slow' },
  },
  {
    label: 'Failing job',
    description: 'Exercises the failed/discarded path.',
    request: { kind: 'dev.test', queue: 'default', count: 1, sleep_ms: 0, fail: true, tag: 'playground-fail' },
  },
]

function PlaygroundPage() {
  const { push } = useToast()
  const queryClient = useQueryClient()
  const [simulating, setSimulating] = useState(false)
  const [queue, setQueue] = useState('default')
  const [count, setCount] = useState('1')
  const [sleepMS, setSleepMS] = useState('0')
  const [fail, setFail] = useState(false)
  const [tag, setTag] = useState('playground')
  const [mailTo, setMailTo] = useState('playground@example.test')
  const [mailSubject, setMailSubject] = useState('Playground test email')
  const [mailText, setMailText] = useState('Sent from the developer Playground.')
  const [lastResult, setLastResult] = useState<PlaygroundQueueResult | null>(null)

  const me = useQuery({
    queryKey: ['me'],
    queryFn: fetchMe,
    staleTime: 60_000,
    retry: false,
    placeholderData: getCachedMe() ?? undefined,
  })

  const local = me.data?.playground === true

  const info = useQuery({
    queryKey: ['playground'],
    queryFn: ({ signal }) => api<PlaygroundInfo>('/dev/api/playground', { signal }),
    enabled: local,
    retry: false,
  })

  const simulate = async (logs: number, level: 'warn' | 'error', exception: boolean) => {
    setSimulating(true)
    try {
      const result = await api<{ request_id: string; logs: number; exception: boolean }>(
        '/dev/api/simulate-events',
        { method: 'POST', body: { logs, level, exception } },
      )
      push(
        `Generated ${result.logs} ${level} log${result.logs === 1 ? '' : 's'}${result.exception ? ' + exception' : ''}`,
        'success',
      )
      for (const key of ['logs', 'exceptions', 'actions', 'overview']) {
        void queryClient.invalidateQueries({ queryKey: [key] })
      }
    } catch (error) {
      push(error instanceof Error ? error.message : 'Simulation failed', 'error')
    } finally {
      setSimulating(false)
    }
  }

  const enqueue = useMutation({
    mutationFn: (request: QueueJobRequest) =>
      api<PlaygroundQueueResult>('/dev/api/playground/jobs', { method: 'POST', body: request }),
    onSuccess: (result) => {
      setLastResult(result)
      push(
        `Enqueued ${result.enqueued} ${result.kind} job${result.enqueued === 1 ? '' : 's'} on ${result.queue} (${result.driver})`,
        'success',
      )
      for (const key of ['queue', 'commands', 'actions', 'overview']) {
        void queryClient.invalidateQueries({ queryKey: [key] })
      }
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Unable to enqueue test jobs', 'error'),
  })

  if (me.isLoading && !me.data) {
    return <LoadingBlock label="Loading playground…" />
  }

  if (!local) {
    return (
      <>
        <PageHeader
          title="Playground"
          description="Local-only sandbox for synthetic telemetry and queue jobs."
        />
        <Card>
          <EmptyState
            title="Playground is local only"
            description={`This environment (${me.data?.email ?? 'unknown'}) is not local/development, so the playground endpoints are disabled.`}
            icon="playground"
            action={
              <Link to="/" className="btn ghost">
                Back to overview
              </Link>
            }
          />
        </Card>
      </>
    )
  }

  const customCount = Number.parseInt(count, 10)
  const customSleep = Number.parseInt(sleepMS, 10)
  const customValid = customCount >= 1 && customCount <= 10 && customSleep >= 0 && customSleep <= 10000

  return (
    <>
      <PageHeader
        title="Playground"
        description="Local-only sandbox for sample telemetry and queue jobs. Every action is recorded in the audit log."
        actions={
          <Link to="/queue" className="btn ghost">
            <Icon name="queue" />
            Open Queue
          </Link>
        }
      />

      <div className="grid cols-2">
        <Card>
          <CardHeader
            title="Test telemetry"
            sub="Generate synthetic entries to verify the Logs and Exceptions screens."
            actions={<Icon name="terminal" className="icon-inline" />}
          />
          <div className="flex wrap">
            <Button icon="logs" disabled={simulating} onClick={() => void simulate(3, 'warn', false)}>
              3 warning logs
            </Button>
            <Button icon="logs" disabled={simulating} onClick={() => void simulate(3, 'error', false)}>
              3 error logs
            </Button>
            <Button
              variant="danger"
              icon="alert"
              disabled={simulating}
              onClick={() => void simulate(1, 'error', true)}
            >
              Log + exception
            </Button>
          </div>
          <div className="small dim mt-8">
            Entries are correlated with the request id of this call and appear immediately in Logs,
            Exceptions, and the Commands audit log.
          </div>
        </Card>

        <Card>
          <CardHeader
            title="Queue test jobs"
            sub="Enqueue synthetic jobs, then inspect, retry or cancel them in Queue."
            actions={
              info.data ? (
                <div className="flex g-2">
                  <Badge tone={info.data.queue_driver === 'river' ? 'ok' : 'warn'}>
                    {info.data.queue_driver}
                  </Badge>
                  <Badge>{info.data.job_kind}</Badge>
                </div>
              ) : null
            }
          />
          {info.isError ? (
            <ErrorBox
              message={info.error instanceof Error ? info.error.message : 'Unable to load playground info.'}
            />
          ) : null}
          <div className="stack-tight">
            {presets.map((preset) => (
              <div key={preset.label} className="flex g-3">
                <Button
                  size="sm"
                  disabled={enqueue.isPending}
                  onClick={() => enqueue.mutate(preset.request)}
                >
                  {preset.label}
                </Button>
                <span className="small dim">{preset.description}</span>
              </div>
            ))}
          </div>
          {info.data?.queue_driver === 'sync' ? (
            <div className="small dim mt-8">
              QUEUE_DRIVER=sync executes jobs inline, so they complete before the response and do not
              appear in Queue. Use the river driver to inspect them.
            </div>
          ) : null}
        </Card>
      </div>

      <Card className="mt-24">
        <CardHeader
          title="Real jobs"
          sub="Exercise actual registered job kinds, not just the synthetic dev.test job."
          actions={
            <div className="flex g-2">
              {(info.data?.jobs ?? ['dev.test', 'user.export', 'mail.send']).map((job) => (
                <Badge key={job} tone="neutral">
                  {job}
                </Badge>
              ))}
            </div>
          }
        />
        <div className="grid cols-2">
          <div className="stack-tight">
            <div className="card-title">User export</div>
            <p className="small dim">
              Creates a real data-processing job and queues <span className="mono">user.export</span> on the
              heavy queue. A running worker writes an xlsx artifact. The developer session is internal, so
              the export covers all tenants.
            </p>
            <div className="flex">
              <Button
                icon="files"
                disabled={enqueue.isPending}
                onClick={() => enqueue.mutate({ kind: 'user.export' })}
              >
                Queue user export
              </Button>
              {lastResult?.kind === 'user.export' ? (
                <span className="small dim">
                  processing #{lastResult.processing_job_id} · {lastResult.job_id}
                </span>
              ) : null}
            </div>
          </div>

          <div className="stack-tight">
            <div className="card-title">Test email</div>
            <p className="small dim">
              Queues <span className="mono">mail.send</span> through the configured driver (the log driver
              by default outside production).
            </p>
            <div className="filters mb-0">
              <label className="field grow">
                <span className="field-label">To</span>
                <input className="input" value={mailTo} onChange={(event) => setMailTo(event.target.value)} />
              </label>
              <label className="field grow">
                <span className="field-label">Subject</span>
                <input
                  className="input"
                  value={mailSubject}
                  onChange={(event) => setMailSubject(event.target.value)}
                />
              </label>
            </div>
            <label className="field">
              <span className="field-label">Text</span>
              <textarea
                className="textarea"
                rows={2}
                value={mailText}
                onChange={(event) => setMailText(event.target.value)}
              />
            </label>
            <div className="flex">
              <Button
                icon="logs"
                disabled={enqueue.isPending || !mailTo.trim() || !mailSubject.trim()}
                onClick={() =>
                  enqueue.mutate({
                    kind: 'mail.send',
                    to: mailTo.trim(),
                    subject: mailSubject.trim(),
                    text: mailText,
                  })
                }
              >
                Queue email
              </Button>
              {lastResult?.kind === 'mail.send' ? (
                <span className="small dim">
                  {lastResult.enqueued} mail job{lastResult.enqueued === 1 ? '' : 's'} on {lastResult.queue}
                </span>
              ) : null}
            </div>
          </div>
        </div>
      </Card>

      <Card className="mt-24">
        <CardHeader
          title="Custom test job"
          sub={`Creates a ${info.data?.job_kind ?? 'dev.test'} job on the selected queue.`}
          actions={<Icon name="playground" className="icon-inline" />}
        />
        <div className="filters mb-0">
          <label className="field">
            <span className="field-label">Queue</span>
            <Select
              value={queue}
              onChange={setQueue}
              options={(info.data?.queues ?? ['default', 'critical', 'heavy']).map((option) => ({ value: option, label: option }))}
            />
          </label>
          <label className="field">
            <span className="field-label">Count (1–10)</span>
            <input
              className="input"
              inputMode="numeric"
              value={count}
              onChange={(event) => setCount(event.target.value.replace(/\D/g, '').slice(0, 2))}
            />
          </label>
          <label className="field">
            <span className="field-label">Sleep (ms)</span>
            <input
              className="input"
              inputMode="numeric"
              value={sleepMS}
              onChange={(event) => setSleepMS(event.target.value.replace(/\D/g, '').slice(0, 5))}
            />
          </label>
          <label className="field">
            <span className="field-label">Tag</span>
            <input className="input" value={tag} maxLength={60} onChange={(event) => setTag(event.target.value)} />
          </label>
          <label className="checkbox field-pad">
            <input type="checkbox" checked={fail} onChange={(event) => setFail(event.target.checked)} />
            Fail on purpose
          </label>
          <span className="spacer" />
          <Button
            variant="primary"
            icon="play"
            disabled={!customValid || enqueue.isPending}
            onClick={() =>
              enqueue.mutate({
                kind: 'dev.test',
                queue,
                count: customCount,
                sleep_ms: customSleep,
                fail,
                tag: tag.trim() || 'playground',
              })
            }
          >
            {enqueue.isPending ? 'Enqueuing…' : `Enqueue ${customCount || 1}`}
          </Button>
        </div>
        <div className="small dim mt-8">
          Sleep is bounded to 10s so the request stays responsive. Jobs are dispatched through the
          normal queue engine and appear in the queue admin with kind {info.data?.job_kind ?? 'dev.test'}.
        </div>
      </Card>
    </>
  )
}
