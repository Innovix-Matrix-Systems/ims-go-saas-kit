import { createFileRoute } from '@tanstack/react-router'
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState, type FormEvent } from 'react'
import { api, type Schemas, type User } from '../api/client'
import { branchesQuery } from '../components/BranchSwitcher'
import { can, useSession } from '../auth/session'
import { initials } from '../format'
import { Field, useApiErrors } from '../components/form'
import { Select } from '../components/inputs'
import { Pagination } from '../components/Pagination'
import { useToast } from '../components/Toasts'
import { Badge, Button, Card, ConfirmDialog, Drawer, EmptyState, Icon, Modal, PageHeader, Spinner } from '../components/ui'
import { intlLocale, useLocale, useT } from '../i18n'

export const Route = createFileRoute('/_app/org/members')({
  component: MembersPage,
})

type Role = Schemas['Role']
interface Page {
  data: User[]
  meta: Schemas['PaginationMeta']
}
interface Organization {
  owner: { id: number } | null
}
type Filter = '' | 'pending' | 'accepted'

function MembersPage() {
  const t = useT()
  const session = useSession()
  const [search, setSearch] = useState('')
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<Filter>('')
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState(20)
  const [inviting, setInviting] = useState(false)
  const [selected, setSelected] = useState<User | null>(null)

  // Search as the user types, without a request per key.
  useEffect(() => {
    const id = window.setTimeout(() => {
      setQuery(search.trim())
      setPage(1)
    }, 300)
    return () => window.clearTimeout(id)
  }, [search])

  const members = useQuery({
    queryKey: ['members', query, filter, page, perPage],
    queryFn: () =>
      api<Page>('/user', { envelope: true, query: { search: query, invitation: filter || undefined, page, per_page: perPage, order_by: 'name', order_direction: 'asc' } }),
    placeholderData: keepPreviousData,
  })
  const organization = useQuery({ queryKey: ['organization'], queryFn: () => api<Organization>('/organization') })
  // System roles (Super-Admin) belong to platform staff and cannot be given
  // to members of an organization.
  const roles = useQuery({
    queryKey: ['roles'],
    queryFn: () => api<Role[]>('/role'),
    select: (items) => items.filter((role) => !role.is_system),
    enabled: can(session, 'role.view.all'),
  })
  const ownerId = organization.data?.owner?.id

  return (
    <>
      <PageHeader
        title={t('members.title')}
        description={t('members.subtitle')}
        actions={
          can(session, 'user.create') ? (
            <Button variant="primary" icon="plus" onClick={() => setInviting(true)}>
              {t('members.invite')}
            </Button>
          ) : null
        }
      />
      <div className="toolbar">
        <input className="input toolbar-search" type="search" placeholder={t('members.search')} value={search} onChange={(e) => setSearch(e.target.value)} />
        <div className="segmented">
          {(
            [
              ['', 'members.filterAll'],
              ['pending', 'members.filterPending'],
              ['accepted', 'members.filterAccepted'],
            ] as const
          ).map(([value, label]) => (
            <button
              key={value}
              type="button"
              className={filter === value ? 'active' : ''}
              onClick={() => {
                setFilter(value)
                setPage(1)
              }}
            >
              {t(label)}
            </button>
          ))}
        </div>
      </div>
      <Card flush>
        {members.isLoading ? (
          <div className="p-4">
            <Spinner />
          </div>
        ) : (members.data?.data ?? []).length === 0 ? (
          <EmptyState icon="users" title={t('members.empty')} />
        ) : (
          <MembersTable users={members.data!.data} ownerId={ownerId} selfId={session?.user.id} onSelect={setSelected} />
        )}
        {members.data ? (
          <Pagination
            page={page}
            perPage={perPage}
            total={members.data.meta.total}
            onPage={setPage}
            onPerPage={(size) => {
              setPerPage(size)
              setPage(1)
            }}
          />
        ) : null}
      </Card>
      <InviteDialog open={inviting} roles={roles.data ?? []} onClose={() => setInviting(false)} />
      {selected ? (
        <MemberDrawer
          user={selected}
          roles={roles.data ?? []}
          isOwner={selected.id === ownerId}
          isSelf={selected.id === session?.user.id}
          onClose={() => setSelected(null)}
        />
      ) : null}
    </>
  )
}

function MembersTable({ users, ownerId, selfId, onSelect }: { users: User[]; ownerId?: number; selfId?: number; onSelect: (user: User) => void }) {
  const t = useT()
  const locale = useLocale()
  const when = (value?: string | null) =>
    value ? new Intl.DateTimeFormat(intlLocale(locale), { dateStyle: 'medium' }).format(new Date(value)) : '—'
  return (
    <div className="table-wrap">
      <table className="table">
        <thead>
          <tr>
            <th>{t('members.colName')}</th>
            <th>{t('members.colRoles')}</th>
            <th>{t('members.colStatus')}</th>
            <th>{t('members.colLastSignIn')}</th>
          </tr>
        </thead>
        <tbody>
          {users.map((user) => (
            <tr key={user.id} className="clickable" onClick={() => onSelect(user)}>
              <td>
                <div className="member-cell">
                  <span className="avatar sm">{initials(user.name)}</span>
                  <span className="member-meta">
                    <span className="member-name">
                      {user.name} {user.id === ownerId ? <Badge tone="info">{t('members.owner')}</Badge> : null}
                      {user.id === selfId ? <Badge tone="neutral">{t('members.you')}</Badge> : null}
                    </span>
                    <span className="small muted">{user.email}</span>
                  </span>
                </div>
              </td>
              <td className="small">{(user.roles ?? []).map((role) => role.name).join(', ') || '—'}</td>
              <td>
                <MemberStatus user={user} />
              </td>
              <td className="small muted">{when(user.last_login_at)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function MemberStatus({ user }: { user: User }) {
  const t = useT()
  if (user.invitation_status === 'pending') return <Badge tone="warn">{t('members.invited')}</Badge>
  return user.is_active ? <Badge tone="ok">{t('members.active')}</Badge> : <Badge tone="neutral">{t('members.inactive')}</Badge>
}

function RolePicker({ roles, value, onChange }: { roles: Role[]; value: number[]; onChange: (ids: number[]) => void }) {
  return (
    <div className="role-picker">
      {roles.map((role) => (
        <label key={role.id} className="check-row">
          <input
            type="checkbox"
            checked={value.includes(role.id)}
            onChange={(e) => onChange(e.target.checked ? [...value, role.id] : value.filter((id) => id !== role.id))}
          />
          {role.name}
        </label>
      ))}
    </div>
  )
}

function InviteDialog({ open, roles, onClose }: { open: boolean; roles: Role[]; onClose: () => void }) {
  const t = useT()
  const session = useSession()
  const client = useQueryClient()
  const { push } = useToast()
  const errors = useApiErrors()
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [roleIds, setRoleIds] = useState<number[]>([])
  const [busy, setBusy] = useState(false)
  const close = () => {
    setName('')
    setEmail('')
    setRoleIds([])
    errors.clear()
    onClose()
  }
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    errors.clear()
    try {
      await api('/user', {
        method: 'POST',
        idempotent: true,
        body: {
          name: name.trim(),
          email: email.trim(),
          send_invitation: true,
          roles: roleIds,
          branch_id: session?.user.active_branch_id ?? session?.user.branch_id ?? undefined,
        },
      })
      push(t('members.invitedToast', { email: email.trim() }), 'success')
      await client.invalidateQueries({ queryKey: ['members'] })
      close()
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Modal open={open} title={t('members.inviteTitle')} description={t('members.inviteHint')} onClose={close}>
      <form onSubmit={submit} className="stack">
        <Field label={t('members.name')} error={errors.field('name')}>
          <input className="input" name="name" autoComplete="off" required value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label={t('auth.email')} error={errors.field('email')}>
          <input className="input" type="email" name="email" autoComplete="off" required value={email} onChange={(e) => setEmail(e.target.value)} />
        </Field>
        {roles.length > 0 ? (
          <Field label={t('members.roles')} error={errors.field('roles')}>
            <RolePicker roles={roles} value={roleIds} onChange={setRoleIds} />
          </Field>
        ) : null}
        {errors.box}
        <div className="modal-actions">
          <Button variant="ghost" type="button" onClick={close}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" type="submit" disabled={busy || name.trim().length < 2 || !email.trim()}>
            {busy ? <Spinner /> : <Icon name="mail" />} {t('members.sendInvite')}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

// PATCH /user/{id} needs the name and e-mail; other fields it is not sent
// keep their value, and an empty string clears one.
function updateBody(user: User, changes: Record<string, unknown>) {
  return { name: user.name, email: user.email, ...changes }
}

function MemberDrawer({
  user,
  roles,
  isOwner,
  isSelf,
  onClose,
}: {
  user: User
  roles: Role[]
  isOwner: boolean
  isSelf: boolean
  onClose: () => void
}) {
  const t = useT()
  const session = useSession()
  const client = useQueryClient()
  const { push } = useToast()
  const errors = useApiErrors()
  const [name, setName] = useState(user.name)
  const [phone, setPhone] = useState(user.phone ?? '')
  const [branchId, setBranchId] = useState(user.branch_id ? String(user.branch_id) : '')
  const [roleIds, setRoleIds] = useState<number[]>((user.roles ?? []).map((role) => role.id))
  const [busy, setBusy] = useState(false)
  const [confirm, setConfirm] = useState<'delete' | 'reset2fa' | null>(null)
  const canUpdate = can(session, 'user.update')
  const canAssign = can(session, 'user.role.assign') && !isSelf
  const branches = useQuery({ ...branchesQuery, enabled: can(session, 'branch.view.all') })

  const run = async (action: () => Promise<unknown>, closeAfter = false) => {
    setBusy(true)
    errors.clear()
    try {
      await action()
      await client.invalidateQueries({ queryKey: ['members'] })
      push(t('members.done'), 'success')
      if (closeAfter) onClose()
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
      setConfirm(null)
    }
  }

  const save = (event: FormEvent) => {
    event.preventDefault()
    void run(async () => {
      await api(`/user/${user.id}`, {
        method: 'PATCH',
        body: updateBody(user, { name: name.trim(), phone: phone.trim(), ...(branchId ? { branch_id: Number(branchId) } : {}) }),
      })
      const original = (user.roles ?? []).map((role) => role.id).sort().join(',')
      if (canAssign && roleIds.length > 0 && [...roleIds].sort().join(',') !== original) {
        await api('/user/assign-role', { method: 'POST', body: { user_id: user.id, roles: roleIds } })
      }
    })
  }

  return (
    <Drawer open title={user.name} subtitle={user.email} onClose={onClose} size="md">
      <div className="stack g-4">
        <div className="flex g-2 align-center">
          <MemberStatus user={user} />
          {isOwner ? <Badge tone="info">{t('members.owner')}</Badge> : null}
        </div>
        <form onSubmit={save} className="stack">
          <Field label={t('members.name')} error={errors.field('name')}>
            <input className="input" name="name" disabled={!canUpdate} value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label={t('signup.phone')} error={errors.field('phone')}>
            <input className="input" type="tel" name="phone" disabled={!canUpdate} value={phone} onChange={(e) => setPhone(e.target.value)} />
          </Field>
          {branches.data && branches.data.length > 1 ? (
            <Field label={t('members.branch')} error={errors.field('branch_id')}>
              <Select
                value={branchId}
                disabled={!canUpdate}
                ariaLabel={t('members.branch')}
                onChange={setBranchId}
                options={branches.data.map((branch) => ({ value: String(branch.id), label: branch.name }))}
              />
            </Field>
          ) : null}
          {roles.length > 0 ? (
            <Field label={t('members.roles')} error={errors.field('roles')}>
              {canAssign ? <RolePicker roles={roles} value={roleIds} onChange={setRoleIds} /> : <span className="small">{(user.roles ?? []).map((r) => r.name).join(', ')}</span>}
            </Field>
          ) : null}
          {errors.box}
          {canUpdate ? (
            <div>
              <Button variant="primary" type="submit" disabled={busy || name.trim().length < 2}>
                {busy ? <Spinner /> : <Icon name="check" />} {t('members.save')}
              </Button>
            </div>
          ) : null}
        </form>
        {!isSelf ? (
          <div className="stack">
            <div className="flex g-2 wrap">
              {canUpdate && user.invitation_status === 'pending' ? (
                <Button icon="mail" disabled={busy} onClick={() => void run(() => api(`/user/${user.id}/invitation`, { method: 'POST' }))}>
                  {t('members.resendInvite')}
                </Button>
              ) : null}
              {canUpdate && user.invitation_status !== 'pending' ? (
                <Button
                  icon={user.is_active ? 'pause' : 'play'}
                  disabled={busy}
                  onClick={() =>
                    void run(() => api(`/user/${user.id}`, { method: 'PATCH', body: updateBody(user, { is_active: !user.is_active }) }), true)
                  }
                >
                  {user.is_active ? t('members.deactivate') : t('members.activate')}
                </Button>
              ) : null}
              {can(session, 'user.two_factor.reset') ? (
                <Button icon="shield" disabled={busy} onClick={() => setConfirm('reset2fa')}>
                  {t('members.resetTwoFactor')}
                </Button>
              ) : null}
              {can(session, 'user.delete') ? (
                <Button variant="danger" icon="trash" disabled={busy} onClick={() => setConfirm('delete')}>
                  {t('members.delete')}
                </Button>
              ) : null}
            </div>
          </div>
        ) : null}
      </div>
      <ConfirmDialog
        open={confirm === 'delete'}
        title={t('members.delete')}
        description={t('members.deleteConfirm', { name: user.name })}
        confirmLabel={t('members.delete')}
        danger
        busy={busy}
        onConfirm={() => void run(() => api(`/user/${user.id}`, { method: 'DELETE' }), true)}
        onCancel={() => setConfirm(null)}
      />
      <ConfirmDialog
        open={confirm === 'reset2fa'}
        title={t('members.resetTwoFactor')}
        description={t('members.resetTwoFactorConfirm', { name: user.name })}
        confirmLabel={t('members.resetTwoFactor')}
        danger
        busy={busy}
        onConfirm={() => void run(() => api(`/user/${user.id}/two-factor`, { method: 'DELETE' }))}
        onCancel={() => setConfirm(null)}
      />
    </Drawer>
  )
}
