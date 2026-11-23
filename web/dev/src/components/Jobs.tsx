import type { DevJob } from '../api/types'
import { formatDateTime, formatDuration, stateTone } from '../format'
import { Badge, Card, Cluster, CodeBlock, JsonView, LiveDot, Stack } from './ui'

// River jobs as the Queue drawer and the job page show them.

export const jobStates = ['available', 'running', 'scheduled', 'retryable', 'pending', 'discarded', 'completed', 'cancelled'] as const
export type JobStateName = (typeof jobStates)[number]

export const jobStateLabels: Record<JobStateName, string> = {
  available: 'Available',
  running: 'Running',
  scheduled: 'Scheduled',
  retryable: 'Retrying',
  pending: 'Pending',
  discarded: 'Failed',
  completed: 'Completed',
  cancelled: 'Cancelled',
}

export type JobAction = 'retry' | 'cancel' | 'delete'

/** Which actions River accepts for a job in this state. */
export function jobCan(action: JobAction, state: string): boolean {
  if (action === 'cancel') return ['available', 'scheduled', 'retryable', 'pending', 'running'].includes(state)
  return state !== 'running'
}

/** The confirmation copy for a job action. */
export function jobActionCopy(job: DevJob, action: JobAction): { title: string; description: string; danger: boolean } {
  const name = `Job #${job.id} (${job.kind})`
  switch (action) {
    case 'retry':
      return { title: 'Retry job', description: `${name} becomes available to run now.`, danger: false }
    case 'cancel':
      return { title: 'Cancel job', description: `${name} will not run again.`, danger: true }
    case 'delete':
      return { title: 'Delete job', description: `${name} is removed for good.`, danger: true }
  }
}

/** A job's state; a running job gets the live dot. */
export function JobState({ state }: { state: string }) {
  return (
    <Cluster gap={1}>
      {state === 'running' ? <LiveDot /> : null}
      <Badge tone={stateTone(state)}>{jobStateLabels[state as JobStateName]?.toLowerCase() ?? state}</Badge>
    </Cluster>
  )
}

/** Timing, args and every failed attempt. */
export function JobFacts({ job }: { job: DevJob }) {
  return (
    <Stack gap={4}>
      <dl className="kv">
        <dt>State</dt>
        <dd>
          <JobState state={job.state} />
        </dd>
        <dt>Queue</dt>
        <dd>{job.queue}</dd>
        <dt>River kind</dt>
        <dd>{job.river_kind ?? '—'}</dd>
        <dt>Priority</dt>
        <dd>{job.priority}</dd>
        <dt>Attempt</dt>
        <dd>
          {job.attempt} / {job.max_attempts}
        </dd>
        <dt>Created</dt>
        <dd>{formatDateTime(job.created_at)}</dd>
        <dt>Scheduled</dt>
        <dd>{formatDateTime(job.scheduled_at)}</dd>
        <dt>Attempted</dt>
        <dd>{job.attempted_at ? formatDateTime(job.attempted_at) : '—'}</dd>
        <dt>Finished</dt>
        <dd>{job.finalized_at ? formatDateTime(job.finalized_at) : '—'}</dd>
        <dt>Took</dt>
        <dd>{jobDuration(job)}</dd>
        <dt>Tags</dt>
        <dd>{(job.tags ?? []).length > 0 ? (job.tags ?? []).join(', ') : '—'}</dd>
      </dl>
      <Card title="Args" description="Values under known secret keys are redacted.">
        <JsonView value={job.args} label="Args" />
      </Card>
      <Card title={`Failed attempts (${job.errors?.length ?? 0})`}>
        {(job.errors?.length ?? 0) === 0 ? (
          <div className="muted small">No attempt has failed.</div>
        ) : (
          <Stack gap={3}>
            {job.errors?.map((error, index) => (
              <div key={`${index}-${error.at}`}>
                <Cluster gap={2} justify="between" className="mb-8">
                  <Badge tone="danger">attempt {error.attempt ?? index + 1}</Badge>
                  <span className="small dim">{error.at ? formatDateTime(error.at) : ''}</span>
                </Cluster>
                <CodeBlock code={error.error ?? ''} label="Error" />
              </div>
            ))}
          </Stack>
        )}
      </Card>
    </Stack>
  )
}

function jobDuration(job: DevJob): string {
  if (!job.attempted_at || !job.finalized_at) return '—'
  return formatDuration(new Date(job.finalized_at).getTime() - new Date(job.attempted_at).getTime())
}
