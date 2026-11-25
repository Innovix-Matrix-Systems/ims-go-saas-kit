import { createFileRoute } from '@tanstack/react-router'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import { api, type Schemas, type User } from '../api/client'
import { can, useSession } from '../auth/session'
import { allTenantsQuery, useStartActing } from '../components/console'
import { useStartExport } from '../components/jobs'
import { Pagination } from '../components/Pagination'
import { useToast } from '../components/Toasts'
import { Avatar, Badge, Button, Card, ConfirmDialog, EmptyState, PageHeader } from '../components/ui'
import { useT } from '../i18n'
import { TableSkeleton } from '../components/Skeleton'

export const Route = createFileRoute('/_app/console/users')({
  component: UsersPage,
})

interface UserPage {
  data: User[]
  meta: Schemas['PaginationMeta']
}

function UsersPage() {
  const t = useT()
  const session = useSession()
  const { push } = useToast()
  const acting = useStartActing()
  const startExport = useStartExport()
  const [search, setSearch] = useState('')
  const [query, setQuery] = useState('')
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState(20)
  const [target, setTarget] = useState<User | null>(null)

  useEffect(() => {
    const id = window.setTimeout(() => {
      setQuery(search.trim())
      setPage(1)
    }, 300)
    return () => window.clearTimeout(id)
  }, [search])

  const users = useQuery({
    queryKey: ['console', 'users', query, page, perPage],
    queryFn: () => api<UserPage>('/user', { asStaff: true, envelope: true, query: { search: query, page, per_page: perPage, order_by: 'name', order_direction: 'asc' } }),
    placeholderData: keepPreviousData,
  })
  const tenants = useQuery(allTenantsQuery)
  const tenantNames = useMemo(() => new Map((tenants.data ?? []).map((tenant) => [tenant.id, tenant.name])), [tenants.data])
  const canLoginAs = can(session, 'user.impersonate')

  return (
    <>
      <PageHeader
        title={t('console.users')}
        description={t('console.usersSubtitle')}
        actions={
          can(session, 'user.internal.export') ? (
            <Button
              icon="download"
              onClick={() =>
                void startExport(t('jobs.kind.export.user'), () =>
                  api('/internal/user/export-background', {
                    method: 'POST',
                    asStaff: true,
                    body: { search: query || undefined, order_by: 'name', order_direction: 'asc' },
                  }),
                )
              }
            >
              {t('export.button')}
            </Button>
          ) : null
        }
      />
      <div className="toolbar">
        <input className="input toolbar-search" type="search" placeholder={t('members.search')} value={search} onChange={(e) => setSearch(e.target.value)} />
      </div>
      <Card flush>
        {users.isLoading ? (
          <TableSkeleton columns={5} />
        ) : (users.data?.data ?? []).length === 0 ? (
          <EmptyState icon="users" title={t('members.empty')} />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>{t('members.colName')}</th>
                  <th>{t('console.colOrg')}</th>
                  <th>{t('members.colRoles')}</th>
                  <th>{t('members.colStatus')}</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {users.data!.data.map((user) => (
                  <tr key={user.id}>
                    <td>
                      <div className="member-cell">
                        <Avatar name={user.name} photo={user.photo} size="sm" />
                        <span className="member-meta">
                          <span className="member-name">{user.name}</span>
                          <span className="small muted">{user.email}</span>
                        </span>
                      </div>
                    </td>
                    <td className="small">
                      {user.tenant_id == null ? <Badge tone="violet">{t('console.staffAccount')}</Badge> : (tenantNames.get(user.tenant_id) ?? `#${user.tenant_id}`)}
                    </td>
                    <td className="small">{(user.roles ?? []).map((role) => role.name).join(', ') || '—'}</td>
                    <td>
                      {user.invitation_status === 'pending' ? (
                        <Badge tone="warn">{t('members.invited')}</Badge>
                      ) : user.is_active ? (
                        <Badge tone="ok">{t('members.active')}</Badge>
                      ) : (
                        <Badge tone="neutral">{t('members.inactive')}</Badge>
                      )}
                    </td>
                    <td className="end">
                      {canLoginAs && user.tenant_id != null && user.is_active ? (
                        <Button size="sm" icon="user" onClick={() => setTarget(user)}>
                          {t('console.loginAs')}
                        </Button>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {users.data ? (
          <Pagination
            page={page}
            perPage={perPage}
            total={users.data.meta.total}
            onPage={setPage}
            onPerPage={(size) => {
              setPerPage(size)
              setPage(1)
            }}
          />
        ) : null}
      </Card>
      <ConfirmDialog
        open={target != null}
        title={t('console.loginAs')}
        description={t('console.loginAsConfirm', { name: target?.name ?? '' })}
        confirmLabel={t('console.loginAs')}
        danger
        onConfirm={() => {
          if (target) {
            void acting
              .loginAs(target, target.tenant_id != null ? tenantNames.get(target.tenant_id) : undefined)
              .catch((cause: unknown) => push(cause instanceof Error ? cause.message : String(cause), 'error'))
          }
          setTarget(null)
        }}
        onCancel={() => setTarget(null)}
      />
    </>
  )
}
