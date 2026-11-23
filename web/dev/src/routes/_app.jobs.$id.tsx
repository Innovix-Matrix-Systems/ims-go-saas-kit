import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { api } from '../api/client'
import type { DevJob } from '../api/types'
import { JobFacts, JobState, jobActionCopy, jobCan, type JobAction } from '../components/Jobs'
import { useToast } from '../components/Toasts'
import { BackLink, Button, ConfirmDialog, DetailStrip, EmptyState, ErrorBox, LoadingBlock, PageHeader } from '../components/ui'
import { relativeTime } from '../format'

export const Route = createFileRoute('/_app/jobs/$id')({
  component: JobDetailPage,
})

function JobDetailPage() {
  const { id } = Route.useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { push } = useToast()
  const [confirm, setConfirm] = useState<JobAction | null>(null)

  const job = useQuery({
    queryKey: ['job', id],
    queryFn: ({ signal }) => api<DevJob>(`/dev/api/queue/jobs/${id}`, { signal }),
    // A job that can still change is followed; a finished one is not.
    refetchInterval: (query) => (['completed', 'discarded', 'cancelled'].includes(query.state.data?.state ?? '') ? false : 5000),
    retry: false,
  })

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['jobs'] })
    void queryClient.invalidateQueries({ queryKey: ['job', id] })
    void queryClient.invalidateQueries({ queryKey: ['queues'] })
    void queryClient.invalidateQueries({ queryKey: ['actions'] })
    void queryClient.invalidateQueries({ queryKey: ['overview'] })
  }

  const mutation = useMutation({
    mutationFn: (action: JobAction) => {
      if (action === 'delete') return api(`/dev/api/queue/jobs/${id}`, { method: 'DELETE' })
      return api(`/dev/api/queue/jobs/${id}/${action}`, { method: 'POST' })
    },
    onSuccess: (_result, action) => {
      push(`Job #${id} ${{ retry: 'queued again', cancel: 'cancelled', delete: 'deleted' }[action]}`, 'success')
      if (action === 'delete') void navigate({ to: '/queue', search: {} })
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Job action failed', 'error'),
    onSettled: invalidate,
  })

  if (job.isLoading) return <LoadingBlock label="Loading job…" />
  if (job.isError) {
    return <ErrorBox message={job.error instanceof Error ? job.error.message : 'Unable to load job.'} onRetry={() => void job.refetch()} />
  }
  if (!job.data) return <EmptyState title="Job not found" icon="queue" />

  const item = job.data
  const copy = confirm ? jobActionCopy(item, confirm) : null

  return (
    <>
      <BackLink to="/queue" search={{}}>
        Queue
      </BackLink>
      <PageHeader
        title={item.kind}
        meta={`Job #${item.id} · created ${relativeTime(item.created_at)}`}
        actions={
          <>
            <Button icon="refresh" onClick={() => void job.refetch()} disabled={job.isFetching}>
              Refresh
            </Button>
            {jobCan('retry', item.state) ? (
              <Button icon="refresh" onClick={() => setConfirm('retry')}>
                Retry
              </Button>
            ) : null}
            {jobCan('cancel', item.state) ? (
              <Button variant="danger" icon="pause" onClick={() => setConfirm('cancel')}>
                Cancel
              </Button>
            ) : null}
            {jobCan('delete', item.state) ? (
              <Button variant="danger" icon="trash" onClick={() => setConfirm('delete')}>
                Delete
              </Button>
            ) : null}
          </>
        }
      />

      <DetailStrip
        items={[
          { label: 'State', value: <JobState state={item.state} /> },
          { label: 'Queue', value: item.queue },
          { label: 'Attempt', value: `${item.attempt} of ${item.max_attempts}` },
          { label: 'Priority', value: item.priority },
        ]}
      />

      <JobFacts job={item} />

      <ConfirmDialog
        open={copy !== null}
        title={copy?.title ?? ''}
        description={copy?.description}
        confirmLabel={copy?.title}
        danger={copy?.danger}
        busy={mutation.isPending}
        onCancel={() => setConfirm(null)}
        onConfirm={() => {
          if (!confirm) return
          mutation.mutate(confirm, { onSettled: () => setConfirm(null) })
        }}
      />
    </>
  )
}
