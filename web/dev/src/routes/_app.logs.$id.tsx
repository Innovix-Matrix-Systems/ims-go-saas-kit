import { useQuery } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { api } from '../api/client'
import type { DevLog } from '../api/types'
import {
  BackLink,
  Badge,
  Button,
  Card,
  CodeBlock,
  CopyButton,
  DetailStrip,
  EmptyState,
  ErrorBox,
  Grid,
  JsonView,
  LoadingBlock,
  PageHeader,
} from '../components/ui'
import { formatDateTime, levelTone, relativeTime } from '../format'

export const Route = createFileRoute('/_app/logs/$id')({
  component: LogDetailPage,
})

function LogDetailPage() {
  const { id } = Route.useParams()
  const log = useQuery({
    queryKey: ['log', id],
    queryFn: ({ signal }) => api<DevLog>(`/dev/api/logs/${id}`, { signal }),
  })

  if (log.isLoading) return <LoadingBlock label="Loading log…" />
  if (log.isError) {
    return (
      <ErrorBox message={log.error instanceof Error ? log.error.message : 'Unable to load log.'} onRetry={() => void log.refetch()} />
    )
  }
  if (!log.data) return <EmptyState title="Log not found" icon="logs" />

  const item = log.data

  return (
    <>
      <BackLink to="/logs">Logs</BackLink>
      <PageHeader
        title={item.message.length > 120 ? `${item.message.slice(0, 120)}…` : item.message}
        meta={`Log #${item.id} · ${relativeTime(item.created_at)}`}
        actions={
          <>
            {item.request_id ? (
              <Link to="/requests" search={{ request_id: item.request_id }} className="btn">
                Open request
              </Link>
            ) : null}
            <Button icon="refresh" onClick={() => void log.refetch()} disabled={log.isFetching}>
              Refresh
            </Button>
          </>
        }
      />

      <DetailStrip
        items={[
          { label: 'Level', value: <Badge tone={levelTone(item.level)}>{item.level.toLowerCase()}</Badge> },
          { label: 'Captured', value: formatDateTime(item.created_at) },
          {
            label: 'Request ID',
            wide: true,
            value: item.request_id ? (
              <>
                <Link to="/requests" search={{ request_id: item.request_id }} className="link mono truncate">
                  {item.request_id}
                </Link>
                <CopyButton value={item.request_id} />
              </>
            ) : (
              <span className="dim">not tied to a request</span>
            ),
          },
        ]}
      />

      <Grid min="lg" gap={4}>
        <Card title="Message">
          <CodeBlock code={item.message} label="Message" />
        </Card>
        <Card title="Attributes" description="Secrets are redacted before they are stored.">
          <JsonView value={item.attributes} label="Attributes" />
        </Card>
      </Grid>
    </>
  )
}
