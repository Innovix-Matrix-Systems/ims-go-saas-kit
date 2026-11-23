import { useQuery } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { api } from '../api/client'
import type { DevException } from '../api/types'
import {
  BackLink,
  Button,
  Card,
  CodeBlock,
  CopyButton,
  DetailStrip,
  EmptyState,
  ErrorBox,
  LoadingBlock,
  PageHeader,
  Stack,
} from '../components/ui'
import { formatDateTime, relativeTime } from '../format'

export const Route = createFileRoute('/_app/exceptions/$id')({
  component: ExceptionDetailPage,
})

function ExceptionDetailPage() {
  const { id } = Route.useParams()
  const exception = useQuery({
    queryKey: ['exception', id],
    queryFn: ({ signal }) => api<DevException>(`/dev/api/exceptions/${id}`, { signal }),
  })

  if (exception.isLoading) return <LoadingBlock label="Loading exception…" />
  if (exception.isError) {
    return (
      <ErrorBox
        message={exception.error instanceof Error ? exception.error.message : 'Unable to load exception.'}
        onRetry={() => void exception.refetch()}
      />
    )
  }
  if (!exception.data) return <EmptyState title="Exception not found" icon="exceptions" />

  const item = exception.data

  return (
    <>
      <BackLink to="/exceptions">Exceptions</BackLink>
      <PageHeader
        title={item.class}
        meta={`Exception #${item.id} · ${relativeTime(item.created_at)}`}
        actions={
          <>
            {item.request_id ? (
              <Link to="/requests" search={{ request_id: item.request_id }} className="btn">
                Open request
              </Link>
            ) : null}
            <Button icon="refresh" onClick={() => void exception.refetch()} disabled={exception.isFetching}>
              Refresh
            </Button>
          </>
        }
      />

      <DetailStrip
        items={[
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

      <Stack gap={4}>
        <Card title="Message">
          <div className="exception-message">{item.message}</div>
        </Card>
        {item.stack ? (
          <Card title="Stack trace" description="Newest frame first.">
            <CodeBlock code={item.stack} label="Stack trace" maxHeight={560} />
          </Card>
        ) : null}
      </Stack>
    </>
  )
}
