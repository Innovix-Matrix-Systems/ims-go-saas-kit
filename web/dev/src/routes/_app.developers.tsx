import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { useMemo, useState, type FormEvent, type MouseEvent } from 'react'
import type { ColumnDef } from '@tanstack/react-table'
import { api } from '../api/client'
import type { DevDeveloper, DevDeveloperPage, DevDeveloperStats } from '../api/types'
import { DataTable } from '../components/DataTable'
import { useToast } from '../components/Toasts'
import {
  Button,
  Card,
  CardHeader,
  Badge,
  ConfirmDialog,
  EmptyState,
  ErrorBox,
  LoadingBlock,
  PageHeader,
  StatTile,
} from '../components/ui'
import { formatDateTime } from '../format'

export const Route = createFileRoute('/_app/developers')({
  component: DevelopersPage,
})

interface ConfirmState {
  title: string
  description: string
  confirmLabel: string
  danger?: boolean
  run: () => Promise<unknown>
}

interface CreateForm {
  name: string
  email: string
  password: string
  isActive: boolean
}

const emptyForm: CreateForm = { name: '', email: '', password: '', isActive: true }

function DevelopersPage() {
  const { push } = useToast()
  const queryClient = useQueryClient()
  const [searchInput, setSearchInput] = useState('')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)
  const [createOpen, setCreateOpen] = useState(false)
  const [form, setForm] = useState<CreateForm>(emptyForm)
  const [confirm, setConfirm] = useState<ConfirmState | null>(null)
  const [confirmBusy, setConfirmBusy] = useState(false)

  const stats = useQuery({
    queryKey: ['developer-stats'],
    queryFn: ({ signal }) => api<DevDeveloperStats>('/dev/api/developers/stats', { signal }),
    staleTime: 10_000,
  })

  const developers = useQuery({
    queryKey: ['developers', { search, page }],
    queryFn: ({ signal }) =>
      api<DevDeveloperPage>('/dev/api/developers', {
        query: { search: search || undefined, page, per_page: 25 },
        signal,
        unwrap: false,
      }),
    placeholderData: keepPreviousData,
  })

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['developers'] })
    void queryClient.invalidateQueries({ queryKey: ['developer-stats'] })
    void queryClient.invalidateQueries({ queryKey: ['actions'] })
    void queryClient.invalidateQueries({ queryKey: ['overview'] })
  }

  const ensureRole = useMutation({
    mutationFn: () => api<DevDeveloperStats>('/dev/api/developers/role', { method: 'POST' }),
    onSuccess: (result) => {
      push(
        result.role_exists
          ? `Developer role ready with ${result.role_permissions} permissions`
          : 'Developer role provisioned',
        'success',
      )
      invalidate()
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Unable to provision the role', 'error'),
  })

  const create = useMutation({
    mutationFn: (body: CreateForm) =>
      api<DevDeveloper>('/dev/api/developers', {
        method: 'POST',
        body: {
          name: body.name.trim(),
          email: body.email.trim(),
          password: body.password,
          is_active: body.isActive,
        },
      }),
    onSuccess: (developer) => {
      push(`${developer.name} can now access the developer dashboard`, 'success')
      setCreateOpen(false)
      setForm(emptyForm)
      invalidate()
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Unable to create the developer', 'error'),
  })

  const remove = useMutation({
    mutationFn: (userID: number) => api(`/dev/api/developers/${userID}`, { method: 'DELETE' }),
    onSuccess: () => {
      push('Developer access removed', 'success')
      invalidate()
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Unable to remove the developer', 'error'),
  })

  const roleExists = stats.data?.role_exists ?? false
  const rolePermissions = stats.data?.role_permissions ?? 0
  const totalPermissions = stats.data?.total_permissions ?? 0
  const roleOutOfSync = roleExists && rolePermissions < totalPermissions
  const statsReady = Boolean(stats.data) && !stats.isError
  const rows = developers.data?.data ?? []
  const meta = developers.data?.meta

  const submitSearch = (event: FormEvent) => {
    event.preventDefault()
    setSearch(searchInput.trim())
    setPage(1)
  }

  const submitCreate = (event: FormEvent) => {
    event.preventDefault()
    if (!roleExists || create.isPending) return
    create.mutate(form)
  }

  const askRemove = (event: MouseEvent, developer: DevDeveloper) => {
    event.stopPropagation()
    setConfirm({
      title: `Remove ${developer.name}?`,
      description:
        'The account loses dashboard access immediately. The internal user record remains, but the Developer role is unassigned.',
      confirmLabel: 'Remove access',
      danger: true,
      run: () => remove.mutateAsync(developer.user_id),
    })
  }

  const columns = useMemo<ColumnDef<DevDeveloper, any>[]>(
    () => [
      {
        id: 'developer',
        header: 'Developer',
        accessorFn: (row) => row.name,
        cell: ({ row }) => (
          <div>
            <div className="strong">
              {row.original.name}
              {row.original.is_self ? <span className="small dim"> (you)</span> : null}
            </div>
            <div className="small dim">{row.original.email}</div>
          </div>
        ),
      },
      {
        id: 'status',
        header: 'Status',
        accessorFn: (row) => (row.is_active ? 1 : 0),
        cell: ({ row }) => (
          <Badge tone={row.original.is_active ? 'ok' : 'danger'}>
            {row.original.is_active ? 'active' : 'inactive'}
          </Badge>
        ),
      },
      {
        id: 'role',
        header: 'Role',
        accessorFn: () => 'Developer',
        cell: () => <span className="small muted">Developer</span>,
      },
      {
        id: 'tokens',
        header: 'Tokens',
        accessorFn: (row) => row.active_tokens,
        cell: ({ row }) => <span className="num">{row.original.active_tokens}</span>,
      },
      {
        id: 'created',
        header: 'Created',
        accessorFn: (row) => row.created_at,
        cell: ({ row }) => (
          <span className="small dim nowrap" title={row.original.created_at}>
            {formatDateTime(row.original.created_at)}
          </span>
        ),
      },
      {
        id: 'actions',
        header: 'Actions',
        enableSorting: false,
        cell: ({ row }) => (
          <div className="flex justify-end">
            <Button
              size="sm"
              variant="ghost"
              icon="trash"
              title={row.original.is_self ? 'You cannot remove your own Developer role' : 'Remove access'}
              disabled={row.original.is_self}
              onClick={(event) => askRemove(event, row.original)}
            >
              Remove
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
        title="Developers"
        description="Dedicated developer accounts for testing API features — every account carries the Developer role with all permissions."
        actions={
          <Button variant="primary" icon="developers" onClick={() => setCreateOpen(true)}>
            New developer
          </Button>
        }
      />

      <div className="grid cols-4">
        <StatTile
          label="Developers"
          value={stats.data?.developers ?? 0}
          hint="internal accounts"
        />
        <StatTile
          label="Role status"
          value={roleExists ? 'Present' : 'Missing'}
          tone={roleExists ? 'ok' : 'danger'}
          hint={roleExists ? `${rolePermissions}/${totalPermissions} permissions` : 'create it below'}
        />
        <StatTile label="Total permissions" value={totalPermissions} hint="catalog entries" />
        <StatTile label="Role" value="Developer" hint="hidden system role" />
      </div>

      {stats.isError ? (
        <div className="mt-24">
          <ErrorBox
            message={stats.error instanceof Error ? stats.error.message : 'Unable to load developer settings.'}
          />
        </div>
      ) : statsReady && (!roleExists || roleOutOfSync) ? (
        <Card className="mt-24 role-warning">
          <div className="flex align-start g-4">
            <div className="fill">
              <div className="card-title">Developer role</div>
              <div className="card-sub mt-8">
                {roleExists
                  ? `The role has ${rolePermissions} of ${totalPermissions} catalog permissions. Sync it to mirror Super-Admin.`
                  : 'The "Developer" role does not exist yet. One click creates it and syncs it with all permissions, mirroring Super-Admin.'}
              </div>
            </div>
            <Button
              variant="primary"
              icon="check"
              disabled={ensureRole.isPending}
              onClick={() => ensureRole.mutate()}
            >
              {ensureRole.isPending
                ? 'Provisioning…'
                : roleExists
                  ? 'Sync permissions'
                  : 'Create Developer role'}
            </Button>
          </div>
        </Card>
      ) : null}

      <Card className="mt-24">
        <CardHeader
          title="Developer accounts"
          sub={`${meta?.total ?? rows.length} account(s)`}
          actions={
            <form className="flex g-2" onSubmit={submitSearch}>
              <input
                className="input w-md"
                placeholder="Search name or email…"
                value={searchInput}
                onChange={(event) => setSearchInput(event.target.value)}
                aria-label="Search developer accounts"
              />
              <Button type="submit" icon="search">
                Search
              </Button>
            </form>
          }
        />
        {developers.isLoading ? (
          <LoadingBlock label="Loading developer accounts…" />
        ) : developers.isError ? (
          <ErrorBox
            message={
              developers.error instanceof Error ? developers.error.message : 'Unable to load developer accounts.'
            }
          />
        ) : (
          <>
            <DataTable
              data={rows}
              columns={columns}
              empty={
                <EmptyState
                  title="No developer accounts"
                  description="Create one with the button above. Accounts get the Developer role (all permissions) and are meant for testing API features."
                  icon="developers"
                  action={
                    <Button variant="primary" icon="developers" onClick={() => setCreateOpen(true)}>
                      New developer
                    </Button>
                  }
                />
              }
            />
            <div className="flex mt-16">
              <Button
                icon="chevron"
                disabled={!meta || meta.current_page <= 1 || developers.isFetching}
                onClick={() => setPage((value) => Math.max(1, value - 1))}
              >
                Previous
              </Button>
              <Button
                icon="chevron"
                disabled={!meta || meta.current_page >= meta.last_page || developers.isFetching}
                onClick={() => setPage((value) => value + 1)}
              >
                Next
              </Button>
              <span className="small dim">
                {meta ? `page ${meta.current_page} of ${meta.last_page}` : ''}
              </span>
              {search ? (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    setSearchInput('')
                    setSearch('')
                    setPage(1)
                  }}
                >
                  Clear search
                </Button>
              ) : null}
            </div>
          </>
        )}
      </Card>

      {createOpen ? (
        <div className="overlay center" onClick={() => !create.isPending && setCreateOpen(false)}>
          <div
            className="modal lg"
            onClick={(event) => event.stopPropagation()}
          >
            <h2 className="modal-title">Create a developer</h2>
            <p className="modal-sub">
              The account receives the Developer role (all permissions) so it can hit any API endpoint.
            </p>
            {!roleExists ? (
              <div className="mb-16">
                <ErrorBox message="Create the Developer role first — the button is on the page behind this dialog." />
              </div>
            ) : null}
            <form onSubmit={submitCreate}>
              <label className="field">
                <span className="field-label">Name</span>
                <input
                  className="input"
                  value={form.name}
                  onChange={(event) => setForm((current) => ({ ...current, name: event.target.value }))}
                  placeholder="Dev McTester"
                  autoComplete="name"
                  required
                  minLength={2}
                  maxLength={255}
                />
              </label>
              <label className="field mt-16">
                <span className="field-label">Email</span>
                <input
                  className="input"
                  type="email"
                  value={form.email}
                  onChange={(event) => setForm((current) => ({ ...current, email: event.target.value }))}
                  placeholder="dev@mem.local"
                  autoComplete="email"
                  required
                  maxLength={255}
                />
              </label>
              <label className="field mt-16">
                <span className="field-label">Password</span>
                <input
                  className="input"
                  type="password"
                  value={form.password}
                  onChange={(event) => setForm((current) => ({ ...current, password: event.target.value }))}
                  placeholder="At least 8 characters"
                  autoComplete="new-password"
                  required
                  minLength={8}
                  maxLength={72}
                />
                <span className="small dim">Minimum 8 characters.</span>
              </label>
              <label className="checkbox mt-16">
                <input
                  type="checkbox"
                  checked={form.isActive}
                  onChange={(event) =>
                    setForm((current) => ({ ...current, isActive: event.target.checked }))
                  }
                />
                Active
              </label>
              {create.isError ? (
                <div className="mt-16">
                  <ErrorBox
                    message={create.error instanceof Error ? create.error.message : 'Unable to create the developer.'}
                  />
                </div>
              ) : null}
              <div className="modal-actions">
                <Button
                  type="button"
                  variant="ghost"
                  onClick={() => setCreateOpen(false)}
                  disabled={create.isPending}
                >
                  Cancel
                </Button>
                <Button type="submit" variant="primary" disabled={!roleExists || create.isPending}>
                  {create.isPending ? 'Creating…' : 'Create developer account'}
                </Button>
              </div>
            </form>
          </div>
        </div>
      ) : null}

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
