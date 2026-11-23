import { createFileRoute } from '@tanstack/react-router'
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState, type FormEvent } from 'react'
import { api, type Schemas, type User } from '../api/client'
import { can, useSession } from '../auth/session'
import { Field, useApiErrors } from '../components/form'
import { Select } from '../components/inputs'
import { Pagination } from '../components/Pagination'
import { useToast } from '../components/Toasts'
import { Badge, Button, Card, ConfirmDialog, Drawer, EmptyState, Icon, PageHeader, Spinner } from '../components/ui'
import { useT } from '../i18n'
import { TableSkeleton } from '../components/Skeleton'

export const Route = createFileRoute('/_app/org/branches')({
  component: BranchesPage,
})

type Branch = Schemas['Branch']
interface Page {
  data: Branch[]
  meta: Schemas['PaginationMeta']
}

function BranchesPage() {
  const t = useT()
  const session = useSession()
  const [search, setSearch] = useState('')
  const [query, setQuery] = useState('')
  const [page, setPage] = useState(1)
  const [perPage, setPerPage] = useState(20)
  // null: closed; 'new': the create form; a branch: its editor.
  const [editing, setEditing] = useState<Branch | 'new' | null>(null)

  useEffect(() => {
    const id = window.setTimeout(() => {
      setQuery(search.trim())
      setPage(1)
    }, 300)
    return () => window.clearTimeout(id)
  }, [search])

  const branches = useQuery({
    queryKey: ['branches', query, page, perPage],
    queryFn: () => api<Page>('/branch', { envelope: true, query: { search: query, page, per_page: perPage, order_by: 'name', order_direction: 'asc' } }),
    placeholderData: keepPreviousData,
  })
  const currentId = session?.user.active_branch_id ?? session?.user.branch_id

  return (
    <>
      <PageHeader
        title={t('org.branches')}
        description={t('branches.subtitle')}
        actions={
          can(session, 'branch.create') ? (
            <Button variant="primary" icon="plus" onClick={() => setEditing('new')}>
              {t('branches.create')}
            </Button>
          ) : null
        }
      />
      <div className="toolbar">
        <input className="input toolbar-search" type="search" placeholder={t('branches.search')} value={search} onChange={(e) => setSearch(e.target.value)} />
      </div>
      <Card flush>
        {branches.isLoading ? (
          <TableSkeleton columns={3} avatar={false} />
        ) : (branches.data?.data ?? []).length === 0 ? (
          <EmptyState icon="building" title={t('branches.empty')} />
        ) : (
          <div className="table-wrap">
            <table className="table">
              <thead>
                <tr>
                  <th>{t('branches.name')}</th>
                  <th>{t('branches.address')}</th>
                  <th>{t('branches.phone')}</th>
                </tr>
              </thead>
              <tbody>
                {branches.data!.data.map((branch) => (
                  <tr key={branch.id} className="clickable" onClick={() => setEditing(branch)}>
                    <td>
                      <span className="member-name">
                        {branch.name} {branch.id === currentId ? <Badge tone="info">{t('branches.current')}</Badge> : null}
                      </span>
                    </td>
                    <td className="small muted">{branch.address || '—'}</td>
                    <td className="small muted">{branch.phone || '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {branches.data ? (
          <Pagination
            page={page}
            perPage={perPage}
            total={branches.data.meta.total}
            onPage={setPage}
            onPerPage={(size) => {
              setPerPage(size)
              setPage(1)
            }}
          />
        ) : null}
      </Card>
      {editing ? <BranchDrawer branch={editing === 'new' ? null : editing} onClose={() => setEditing(null)} /> : null}
    </>
  )
}

function BranchDrawer({ branch, onClose }: { branch: Branch | null; onClose: () => void }) {
  const t = useT()
  const session = useSession()
  const client = useQueryClient()
  const { push } = useToast()
  const errors = useApiErrors()
  const editable = branch ? can(session, 'branch.update') : can(session, 'branch.create')
  const [name, setName] = useState(branch?.name ?? '')
  const [address, setAddress] = useState(branch?.address ?? '')
  const [phone, setPhone] = useState(branch?.phone ?? '')
  const [email, setEmail] = useState(branch?.email ?? '')
  const [description, setDescription] = useState(branch?.description ?? '')
  const [managerId, setManagerId] = useState(branch?.manager_id ? String(branch.manager_id) : '')
  const [busy, setBusy] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const members = useQuery({
    queryKey: ['members', 'accepted-all'],
    queryFn: () => api<User[]>('/user', { query: { invitation: 'accepted', per_page: 100, order_by: 'name', order_direction: 'asc' } }),
    enabled: editable && can(session, 'user.view.all'),
  })

  const done = async () => {
    await client.invalidateQueries({ queryKey: ['branches'] })
    // Deleting the branch you work in moves you back to your own.
    await client.invalidateQueries({ queryKey: ['session'] })
    push(t('form.saved'), 'success')
    onClose()
  }

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    errors.clear()
    const body = {
      name: name.trim(),
      address: address.trim() || null,
      phone: phone.trim() || null,
      email: email.trim() || null,
      description: description.trim() || null,
      manager_id: managerId ? Number(managerId) : null,
    }
    try {
      if (branch) await api(`/branch/${branch.id}`, { method: 'PATCH', body })
      else await api('/branch', { method: 'POST', idempotent: true, body })
      await done()
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  const remove = async () => {
    if (!branch) return
    setBusy(true)
    errors.clear()
    try {
      await api(`/branch/${branch.id}`, { method: 'DELETE' })
      await done()
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
      setConfirmDelete(false)
    }
  }

  // Those who may only look get the branch as text, not a form they cannot use.
  if (branch && !editable) {
    const manager = members.data?.find((user) => user.id === branch.manager_id)
    const none = <span className="muted">{t('profilePage.notSet')}</span>
    return (
      <Drawer open title={branch.name} onClose={onClose} size="md">
        <dl className="details">
          <dt>{t('branches.name')}</dt>
          <dd>{branch.name}</dd>
          <dt>{t('branches.address')}</dt>
          <dd>{branch.address || none}</dd>
          <dt>{t('branches.phone')}</dt>
          <dd>{branch.phone ? <a href={`tel:${branch.phone}`}>{branch.phone}</a> : none}</dd>
          <dt>{t('branches.email')}</dt>
          <dd>{branch.email ? <a href={`mailto:${branch.email}`}>{branch.email}</a> : none}</dd>
          <dt>{t('branches.manager')}</dt>
          <dd>{manager?.name ?? (branch.manager_id ? `#${branch.manager_id}` : none)}</dd>
          <dt>{t('branches.description')}</dt>
          <dd>{branch.description || none}</dd>
        </dl>
      </Drawer>
    )
  }

  return (
    <Drawer open title={branch ? branch.name : t('branches.createTitle')} onClose={onClose} size="md">
      <form onSubmit={submit} className="stack">
        <Field label={t('branches.name')} error={errors.field('name')}>
          <input className="input" name="branch" autoComplete="off" required disabled={!editable} value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label={`${t('branches.address')} (${t('form.optional')})`} error={errors.field('address')}>
          <input className="input" name="address" autoComplete="street-address" disabled={!editable} value={address} onChange={(e) => setAddress(e.target.value)} />
        </Field>
        <div className="form-grid-2">
          <Field label={`${t('branches.phone')} (${t('form.optional')})`} error={errors.field('phone')}>
            <input className="input" type="tel" name="phone" autoComplete="off" disabled={!editable} value={phone} onChange={(e) => setPhone(e.target.value)} />
          </Field>
          <Field label={`${t('branches.email')} (${t('form.optional')})`} error={errors.field('email')}>
            <input className="input" type="email" name="email" autoComplete="off" disabled={!editable} value={email} onChange={(e) => setEmail(e.target.value)} />
          </Field>
        </div>
        {members.data ? (
          <Field label={t('branches.manager')} error={errors.field('manager_id')}>
            <Select
              value={managerId}
              disabled={!editable}
              ariaLabel={t('branches.manager')}
              onChange={setManagerId}
              options={[
                { value: '', label: t('branches.noManager') },
                ...members.data.map((user) => ({ value: String(user.id), label: user.name, text: `${user.name} ${user.email}`, hint: user.email })),
              ]}
            />
          </Field>
        ) : null}
        <Field label={`${t('branches.description')} (${t('form.optional')})`} error={errors.field('description')}>
          <textarea className="input" rows={3} disabled={!editable} value={description} onChange={(e) => setDescription(e.target.value)} />
        </Field>
        {errors.box}
        {editable ? (
          <div className="flex wrap">
            <Button variant="primary" type="submit" disabled={busy || name.trim().length < 2}>
              {busy ? <Spinner /> : <Icon name="check" />} {branch ? t('branches.save') : t('branches.create')}
            </Button>
            <span className="spacer" />
            {branch && can(session, 'branch.delete') ? (
              <Button variant="danger" icon="trash" type="button" disabled={busy} onClick={() => setConfirmDelete(true)}>
                {t('branches.delete')}
              </Button>
            ) : null}
          </div>
        ) : null}
      </form>
      <ConfirmDialog
        open={confirmDelete}
        title={t('branches.delete')}
        description={t('branches.deleteConfirm', { name: branch?.name ?? '' })}
        confirmLabel={t('branches.delete')}
        danger
        busy={busy}
        onConfirm={() => void remove()}
        onCancel={() => setConfirmDelete(false)}
      />
    </Drawer>
  )
}
