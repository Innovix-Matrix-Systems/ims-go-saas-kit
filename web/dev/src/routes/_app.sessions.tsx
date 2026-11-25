import { keepPreviousData, useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { useDeferredValue, useEffect, useMemo, useState, type MouseEvent } from 'react'
import type { ColumnDef } from '@tanstack/react-table'
import { api } from '../api/client'
import type {
  CursorPage,
  DevSessionPruneResult,
  DevSessionRevokeResult,
  DevSessionStats,
  DevSessionToken,
  DevSessionUser,
} from '../api/types'
import { DataTable } from '../components/DataTable'
import { useToast } from '../components/Toasts'
import {
  Button,
  Card,
  CardHeader,
  Badge,
  ConfirmDialog,
  Drawer,
  EmptyState,
  ErrorBox,
  Icon,
  LoadingBlock,
  PageHeader,
  RefreshToggle,
  StatTile,
} from '../components/ui'
import { formatDateTime, relativeTime } from '../format'
import { SessionChart } from '../components/PanelCharts'

export const Route = createFileRoute('/_app/sessions')({
  component: SessionsPage,
})

interface ConfirmState {
  title: string
  description: string
  confirmLabel: string
  danger?: boolean
  run: () => Promise<unknown>
}

function SessionsPage() {
  const { push } = useToast()
  const queryClient = useQueryClient()
  const [search, setSearch] = useState('')
  const [live, setLive] = useState(false)
  const [selectedUser, setSelectedUser] = useState<DevSessionUser | null>(null)
  const [idleDays, setIdleDays] = useState('30')
  const [preview, setPreview] = useState<DevSessionPruneResult | null>(null)
  const [confirm, setConfirm] = useState<ConfirmState | null>(null)
  const [confirmBusy, setConfirmBusy] = useState(false)
  const deferredSearch = useDeferredValue(search)

  const stats = useQuery({
    queryKey: ['sessions', 'stats'],
    queryFn: ({ signal }) => api<DevSessionStats>('/dev/api/sessions/stats', { signal }),
    refetchInterval: live ? 10_000 : false,
  })

  const users = useInfiniteQuery({
    queryKey: ['sessions', 'users', deferredSearch],
    queryFn: ({ pageParam, signal }) =>
      api<CursorPage<DevSessionUser>>('/dev/api/sessions/users', {
        query: { limit: 25, cursor: pageParam || undefined, search: deferredSearch || undefined },
        signal,
        unwrap: false,
      }),
    initialPageParam: '',
    getNextPageParam: (lastPage) => lastPage.meta?.next_cursor || undefined,
    placeholderData: keepPreviousData,
    refetchInterval: live ? 10_000 : false,
  })

  const tokens = useQuery({
    queryKey: ['sessions', 'tokens', selectedUser?.user_id],
    queryFn: ({ signal }) =>
      api<DevSessionToken[]>(`/dev/api/sessions/users/${selectedUser?.user_id}/tokens`, { signal }),
    enabled: selectedUser !== null,
  })

  const rows = useMemo(() => users.data?.pages.flatMap((page) => page.data) ?? [], [users.data])

  useEffect(() => {
    if (!selectedUser) return
    const fresh = rows.find((row) => row.user_id === selectedUser.user_id)
    if (fresh && fresh !== selectedUser) {
      setSelectedUser(fresh)
    }
  }, [rows, selectedUser])

  const selectedTokenCount = tokens.data
    ? tokens.data.filter((token) => !token.expired).length
    : selectedUser?.active_tokens ?? 0

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['sessions'] })
    void queryClient.invalidateQueries({ queryKey: ['actions'] })
    void queryClient.invalidateQueries({ queryKey: ['overview'] })
  }

  const revokeToken = useMutation({
    mutationFn: (tokenId: number) =>
      api<DevSessionRevokeResult>(`/dev/api/sessions/tokens/${tokenId}`, { method: 'DELETE' }),
    onSuccess: (result) => {
      push(`Revoked ${result.revoked} session token${result.revoked === 1 ? '' : 's'}`, 'success')
      invalidate()
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Unable to revoke token', 'error'),
  })

  const revokeUser = useMutation({
    mutationFn: (userId: number) =>
      api<DevSessionRevokeResult>(`/dev/api/sessions/users/${userId}`, { method: 'DELETE' }),
    onSuccess: (result) => {
      push(`Revoked ${result.revoked} token${result.revoked === 1 ? '' : 's'} for the user`, 'success')
      setSelectedUser(null)
      invalidate()
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Unable to revoke user sessions', 'error'),
  })

  const revokeAll = useMutation({
    mutationFn: () => api<DevSessionRevokeResult>('/dev/api/sessions', { method: 'DELETE' }),
    onSuccess: (result) => {
      push(`Force logout complete — ${result.revoked} token${result.revoked === 1 ? '' : 's'} revoked`, 'success')
      invalidate()
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Force logout failed', 'error'),
  })

  const prune = useMutation({
    mutationFn: ({ days, dryRun }: { days: number; dryRun: boolean }) =>
      api<DevSessionPruneResult>('/dev/api/sessions/prune', {
        method: 'POST',
        body: { idle_days: days, dry_run: dryRun },
      }),
    onSuccess: (result) => {
      if (result.dry_run) {
        setPreview(result)
        push(`${result.affected} token${result.affected === 1 ? '' : 's'} would be removed`, 'info')
      } else {
        setPreview(null)
        push(`Removed ${result.affected} token${result.affected === 1 ? '' : 's'}`, 'success')
        invalidate()
      }
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Session prune failed', 'error'),
  })

  const daysValue = Number.parseInt(idleDays, 10)

  const askForceLogout = () => {
    setConfirm({
      title: 'Force logout all users',
      description:
        "Every active token except this console's current token is revoked. Users must sign in again on all devices.",
      confirmLabel: 'Force logout',
      danger: true,
      run: () => revokeAll.mutateAsync(),
    })
  }

  const askUserLogout = (event: MouseEvent, user: DevSessionUser) => {
    event.stopPropagation()
    setConfirm({
      title: `Log out ${user.name}`,
      description: `Revoke all ${user.active_tokens} active token${user.active_tokens === 1 ? '' : 's'} for ${user.email}.`,
      confirmLabel: 'Log out user',
      danger: true,
      run: () => revokeUser.mutateAsync(user.user_id),
    })
  }

  const askTokenRevoke = (token: DevSessionToken) => {
    setConfirm({
      title: `Revoke ${token.device || token.name}`,
      description: token.current
        ? 'This is the token used by the current console session. Revoking it signs the console out.'
        : 'The device must sign in again to obtain a new token.',
      confirmLabel: 'Revoke token',
      danger: true,
      run: () => revokeToken.mutateAsync(token.id),
    })
  }

  const askPrune = () => {
    if (!preview || preview.affected === 0) return
    setConfirm({
      title: 'Clear inactive sessions',
      description: `Delete ${preview.affected} expired or idle token${preview.affected === 1 ? '' : 's'} older than ${preview.idle_days} days. Active tokens used within the threshold are untouched.`,
      confirmLabel: 'Clear tokens',
      danger: true,
      run: () => prune.mutateAsync({ days: preview.idle_days, dryRun: false }),
    })
  }

  const columns = useMemo<ColumnDef<DevSessionUser, any>[]>(
    () => [
      {
        id: 'user',
        header: 'User',
        accessorFn: (row) => row.name,
        cell: ({ row }) => (
          <div>
            <div className="strong">{row.original.name}</div>
            <div className="small dim">{row.original.email}</div>
          </div>
        ),
      },
      {
        id: 'tenant',
        header: 'Tenant',
        accessorFn: (row) => row.tenant_id ?? 0,
        cell: ({ row }) => <span className="small muted">{row.original.tenant_id ?? '—'}</span>,
      },
      {
        id: 'active',
        header: 'Active tokens',
        accessorFn: (row) => row.active_tokens,
        cell: ({ row }) => <span className="num">{row.original.active_tokens}</span>,
      },
      {
        id: 'total',
        header: 'Total',
        accessorFn: (row) => row.total_tokens,
        cell: ({ row }) => <span className="num small dim">{row.original.total_tokens}</span>,
      },
      {
        id: 'last_used',
        header: 'Last used',
        accessorFn: (row) => row.last_used_at ?? '',
        cell: ({ row }) => (
          <span className="small dim nowrap" title={row.original.last_used_at ?? undefined}>
            {row.original.last_used_at ? relativeTime(row.original.last_used_at) : 'Never'}
          </span>
        ),
      },
      {
        id: 'actions',
        header: '',
        enableSorting: false,
        cell: ({ row }) => (
          <div className="flex justify-end g-1">
            <Button size="sm" variant="ghost" title="Open devices" onClick={(event) => {
              event.stopPropagation()
              setSelectedUser(row.original)
            }}>
              <Icon name="eye" />
            </Button>
            <Button size="sm" variant="ghost" title="Log out user" onClick={(event) => askUserLogout(event, row.original)}>
              <Icon name="logout" />
            </Button>
          </div>
        ),
      },
    ],
    [],
  )

  return (
    <>
      <PageHeader
        title="Sessions"
        description="Device-level token management: force logout, idle-token pruning and per-user device revocation."
        actions={
          <>
            <RefreshToggle enabled={live} onChange={setLive} />
            <Button
              icon="refresh"
              onClick={() => {
                void queryClient.invalidateQueries({ queryKey: ['sessions'] })
              }}
            >
              Refresh
            </Button>
          </>
        }
      />

      <div className="grid cols-5">
        <StatTile label="Users" value={stats.data?.users ?? 0} hint="with active tokens" />
        <StatTile label="Active tokens" value={stats.data?.active_tokens ?? 0} tone="ok" hint="non-expired" />
        <StatTile label="Used (24h)" value={stats.data?.used_24h ?? 0} hint="last_used_at in 24h" />
        <StatTile
          label="Idle tokens"
          value={stats.data?.idle_tokens ?? 0}
          tone={(stats.data?.idle_tokens ?? 0) > 0 ? 'accent' : undefined}
          hint="not used in 24h"
        />
        <StatTile
          label="Inactive 30d+"
          value={stats.data?.inactive_30d ?? 0}
          tone={(stats.data?.inactive_30d ?? 0) > 0 ? 'danger' : undefined}
          hint="active but idle 30d+"
        />
      </div>

      {stats.data ? (
        <Card className="mt-24" title="Token activity" description="Active tokens by when they were last used">
          <SessionChart stats={stats.data} />
        </Card>
      ) : null}

      <div className="grid cols-2 mt-24">
        <Card>
          <CardHeader
            title="Force logout all"
            sub="Revoke every token except the console's current session."
          />
          <p className="small dim">
            Use this after a credential leak or when rotating devices. The audit log records how many
            tokens were revoked and the excluded token.
          </p>
          <div className="flex mt-16">
            <Button variant="danger" icon="logout" onClick={askForceLogout} disabled={revokeAll.isPending}>
              Force logout all
            </Button>
          </div>
        </Card>

        <Card>
          <CardHeader
            title="Clear inactive sessions"
            sub="Delete expired tokens and tokens idle beyond the threshold."
          />
          <div className="filters mb-8">
            <label className="field">
              <span className="field-label">Idle threshold (days)</span>
              <input
                className="input"
                inputMode="numeric"
                value={idleDays}
                onChange={(event) => {
                  setIdleDays(event.target.value.replace(/\D/g, '').slice(0, 4))
                  setPreview(null)
                }}
              />
            </label>
            <div className="spacer" />
            <Button
              icon="search"
              onClick={() => prune.mutate({ days: daysValue, dryRun: true })}
              disabled={!daysValue || daysValue < 1 || prune.isPending}
            >
              Preview
            </Button>
            <Button
              variant="danger"
              icon="trash"
              onClick={askPrune}
              disabled={!preview || preview.affected === 0 || prune.isPending}
            >
              Clear inactive
            </Button>
          </div>
          <div className="small dim">
            {preview
              ? preview.affected > 0
                ? `${preview.affected} token${preview.affected === 1 ? '' : 's'} would be removed (threshold ${preview.idle_days} days).`
                : `No tokens match the ${preview.idle_days}-day threshold.`
              : 'Preview first — active tokens used within the threshold are never touched.'}
          </div>
        </Card>
      </div>

      <Card className="mt-24">
        <CardHeader
          title="User directory"
          sub="Users owning at least one non-expired token."
          actions={<span className="small dim">{rows.length} loaded</span>}
        />
        <div className="filters">
          <label className="field grow">
            <span className="field-label">Search user</span>
            <input
              className="input"
              placeholder="Name or email"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
            />
          </label>
        </div>
        {users.isLoading ? (
          <LoadingBlock label="Loading sessions…" />
        ) : users.isError ? (
          <ErrorBox message={users.error instanceof Error ? users.error.message : 'Unable to load sessions.'} />
        ) : (
          <>
            <DataTable
              data={rows}
              columns={columns}
              onRowClick={(row) => setSelectedUser(row)}
              empty={
                <EmptyState
                  title="No active sessions"
                  description="Users appear here as soon as they sign in and receive a token."
                  icon="sessions"
                />
              }
            />
            <div className="flex mt-16">
              {users.hasNextPage ? (
                <Button
                  icon="chevron"
                  onClick={() => void users.fetchNextPage()}
                  disabled={users.isFetchingNextPage}
                >
                  {users.isFetchingNextPage ? 'Loading…' : 'Load more'}
                </Button>
              ) : (
                <span className="small dim">{rows.length === 0 ? 'No rows' : `${rows.length} users loaded`}</span>
              )}
              {users.isFetching && !users.isFetchingNextPage ? (
                <span className="small dim flex">
                  <span className="spinner" /> refreshing
                </span>
              ) : null}
            </div>
          </>
        )}
      </Card>

      <Drawer
        open={selectedUser !== null}
        title={selectedUser?.name ?? 'Devices'}
        subtitle={selectedUser ? `${selectedUser.email} · ${selectedTokenCount} active token${selectedTokenCount === 1 ? '' : 's'}` : undefined}
        onClose={() => setSelectedUser(null)}
        actions={
          selectedUser ? (
            <Button
              size="sm"
              variant="ghost"
              icon="refresh"
              onClick={() => {
                void Promise.all([tokens.refetch(), users.refetch(), stats.refetch()])
              }}
              disabled={tokens.isFetching}
            >
              Refresh
            </Button>
          ) : undefined
        }
      >
        {tokens.isLoading ? (
          <LoadingBlock label="Loading devices…" />
        ) : tokens.isError ? (
          <ErrorBox message={tokens.error instanceof Error ? tokens.error.message : 'Unable to load devices.'} />
        ) : (tokens.data ?? []).length === 0 ? (
          <EmptyState title="No tokens" description="This user has no session tokens." icon="sessions" />
        ) : (
          <div className="stack">
            {(tokens.data ?? []).map((token) => (
              <div key={token.id} className="card p-14">
                <div className="flex align-start">
                  <div className="min-0">
                    <div className="strong">{token.device || token.name}</div>
                    <div className="small dim mono">{token.name}</div>
                  </div>
                  <div className="spacer" />
                  <div className="flex g-2">
                    {token.current ? <Badge tone="ok">current</Badge> : null}
                    {token.expired ? <Badge tone="danger">expired</Badge> : null}
                    <Button
                      size="sm"
                      variant="ghost"
                      icon="trash"
                      title="Revoke token"
                      onClick={() => askTokenRevoke(token)}
                    >
                      Revoke
                    </Button>
                  </div>
                </div>
                <dl className="kv mt-16">
                  <dt>Created</dt>
                  <dd>{formatDateTime(token.created_at)}</dd>
                  <dt>Last used</dt>
                  <dd>{token.last_used_at ? `${formatDateTime(token.last_used_at)} (${relativeTime(token.last_used_at)})` : 'Never'}</dd>
                  <dt>Expires</dt>
                  <dd>{token.expires_at ? formatDateTime(token.expires_at) : 'No expiry'}</dd>
                  <dt>Token ID</dt>
                  <dd>#{token.id}</dd>
                </dl>
              </div>
            ))}
          </div>
        )}
      </Drawer>

      <ConfirmDialog
        open={confirm !== null}
        title={confirm?.title ?? ''}
        description={confirm?.description}
        confirmLabel={confirm?.confirmLabel}
        danger={confirm?.danger}
        busy={confirmBusy}
        onCancel={() => setConfirm(null)}
        onConfirm={() => {
          if (!confirm) return
          setConfirmBusy(true)
          void confirm
            .run()
            .then(() => setConfirm(null))
            .finally(() => setConfirmBusy(false))
        }}
      />
    </>
  )
}
