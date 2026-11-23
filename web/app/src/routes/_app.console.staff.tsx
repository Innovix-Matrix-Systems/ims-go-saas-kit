import { createFileRoute } from '@tanstack/react-router'
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { api, type Schemas, type User } from '../api/client'
import { can, useSession } from '../auth/session'
import { Field, useApiErrors } from '../components/form'
import { Select } from '../components/inputs'
import { Pagination } from '../components/Pagination'
import { useToast } from '../components/Toasts'
import { Avatar, Badge, Button, Card, EmptyState, Icon, Modal, PageHeader, Spinner } from '../components/ui'
import { useT } from '../i18n'
import { TableSkeleton } from '../components/Skeleton'

export const Route = createFileRoute('/_app/console/staff')({
  component: StaffPage,
})

interface UserPage {
  data: User[]
  meta: Schemas['PaginationMeta']
}

function StaffPage() {
  const t = useT()
  const session = useSession()
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState(20)
  const [inviting, setInviting] = useState(false)
  const staff = useQuery({
    queryKey: ['console', 'staff', page, perPage],
    queryFn: () => api<UserPage>('/user', { asStaff: true, envelope: true, query: { platform: true, page, per_page: perPage, order_by: 'name', order_direction: 'asc' } }),
    placeholderData: keepPreviousData,
  })

  return (
    <>
      <PageHeader
        title={t('console.staff')}
        description={t('console.staffSubtitle')}
        actions={
          can(session, 'user.internal.create') ? (
            <Button variant="primary" icon="plus" onClick={() => setInviting(true)}>
              {t('console.newStaff')}
            </Button>
          ) : null
        }
      />
      <Card flush>
        {staff.isLoading ? (
          <TableSkeleton columns={3} />
        ) : (staff.data?.data ?? []).length === 0 ? (
          <EmptyState icon="shield" title={t('members.empty')} />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>{t('members.colName')}</th>
                  <th>{t('members.colRoles')}</th>
                  <th>{t('members.colStatus')}</th>
                </tr>
              </thead>
              <tbody>
                {staff.data!.data.map((user) => (
                  <tr key={user.id}>
                    <td>
                      <div className="member-cell">
                        <Avatar name={user.name} photo={user.photo} size="sm" />
                        <span className="member-meta">
                          <span className="member-name">
                            {user.name} {user.id === session?.user.id ? <Badge tone="neutral">{t('members.you')}</Badge> : null}
                          </span>
                          <span className="small muted">{user.email}</span>
                        </span>
                      </div>
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
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {staff.data ? (
          <Pagination
            page={page}
            perPage={perPage}
            total={staff.data.meta.total}
            onPage={setPage}
            onPerPage={(size) => {
              setPerPage(size)
              setPage(1)
            }}
          />
        ) : null}
      </Card>
      {inviting ? <InviteStaff onClose={() => setInviting(false)} /> : null}
    </>
  )
}

function InviteStaff({ onClose }: { onClose: () => void }) {
  const t = useT()
  const client = useQueryClient()
  const { push } = useToast()
  const errors = useApiErrors()
  // Platform roles are the system roles (Super-Admin, and any staff roles made for the platform).
  const roles = useQuery({
    queryKey: ['console', 'roles'],
    queryFn: () => api<Schemas['Role'][]>('/role', { asStaff: true }),
    select: (items) => items.filter((role) => role.is_system),
  })
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [roleId, setRoleId] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    errors.clear()
    try {
      await api('/internal/user', {
        method: 'POST',
        asStaff: true,
        idempotent: true,
        body: { name: name.trim(), email: email.trim(), send_invitation: true, tenant_id: null, roles: roleId ? [Number(roleId)] : [] },
      })
      await client.invalidateQueries({ queryKey: ['console', 'staff'] })
      push(t('console.staffInvited', { email: email.trim() }), 'success')
      onClose()
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Modal open title={t('console.newStaff')} description={t('members.inviteHint')} onClose={onClose}>
      <form onSubmit={submit} className="stack">
        <Field label={t('members.name')} error={errors.field('name')}>
          <input className="input" autoComplete="off" required value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label={t('auth.email')} error={errors.field('email')}>
          <input className="input" type="email" autoComplete="off" required value={email} onChange={(e) => setEmail(e.target.value)} />
        </Field>
        <Field label={t('console.role')} error={errors.field('roles')}>
          <Select
            value={roleId}
            placeholder={t('console.role')}
            ariaLabel={t('console.role')}
            onChange={setRoleId}
            options={(roles.data ?? []).map((role) => ({ value: String(role.id), label: role.name, hint: role.description ?? undefined }))}
          />
        </Field>
        {errors.box}
        <div className="modal-actions">
          <Button variant="ghost" type="button" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" type="submit" disabled={busy || name.trim().length < 2 || !email.trim() || !roleId}>
            {busy ? <Spinner /> : <Icon name="mail" />} {t('members.sendInvite')}
          </Button>
        </div>
      </form>
    </Modal>
  )
}
