import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { api, type Schemas } from '../api/client'
import { can, useSession } from '../auth/session'
import { Field, useApiErrors } from '../components/form'
import { useToast } from '../components/Toasts'
import { Badge, Button, Card, ConfirmDialog, Drawer, EmptyState, Icon, Modal, PageHeader, Spinner } from '../components/ui'
import { useT } from '../i18n'

export const Route = createFileRoute('/_app/org/roles')({
  component: RolesPage,
})

type Role = Schemas['Role']
type Permission = Schemas['Permission']

function RolesPage() {
  const t = useT()
  const session = useSession()
  const [creating, setCreating] = useState(false)
  const [selected, setSelected] = useState<number | null>(null)
  // System roles (Super-Admin) are platform roles; organizations never see them.
  const roles = useQuery({
    queryKey: ['roles'],
    queryFn: () => api<Role[]>('/role'),
    select: (items) => items.filter((role) => !role.is_system),
  })

  return (
    <>
      <PageHeader
        title={t('org.roles')}
        description={t('roles.subtitle')}
        actions={
          can(session, 'role.create') ? (
            <Button variant="primary" icon="plus" onClick={() => setCreating(true)}>
              {t('roles.create')}
            </Button>
          ) : null
        }
      />
      <Card flush>
        {roles.isLoading ? (
          <div className="p-4">
            <Spinner />
          </div>
        ) : (roles.data ?? []).length === 0 ? (
          <EmptyState icon="shield" title={t('roles.empty')} />
        ) : (
          <ul className="role-list">
            {roles.data!.map((role) => (
              <li key={role.id}>
                <button type="button" className="role-row" onClick={() => setSelected(role.id)}>
                  <span className="role-meta">
                    <span className="member-name">
                      {role.name} {role.managed ? <Badge tone="info">{t('roles.managed')}</Badge> : null}
                    </span>
                    {role.description ? <span className="small muted">{role.description}</span> : null}
                  </span>
                  <Icon name="chevron" />
                </button>
              </li>
            ))}
          </ul>
        )}
      </Card>
      <CreateRoleDialog open={creating} onClose={() => setCreating(false)} onCreated={(id) => setSelected(id)} />
      {selected != null ? <RoleDrawer id={selected} onClose={() => setSelected(null)} /> : null}
    </>
  )
}

function CreateRoleDialog({ open, onClose, onCreated }: { open: boolean; onClose: () => void; onCreated: (id: number) => void }) {
  const t = useT()
  const client = useQueryClient()
  const errors = useApiErrors()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [busy, setBusy] = useState(false)
  const close = () => {
    setName('')
    setDescription('')
    errors.clear()
    onClose()
  }
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setBusy(true)
    errors.clear()
    try {
      const role = await api<Role>('/role', { method: 'POST', body: { name: name.trim(), description: description.trim() || null } })
      await client.invalidateQueries({ queryKey: ['roles'] })
      close()
      onCreated(role.id)
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }
  return (
    <Modal open={open} title={t('roles.createTitle')} onClose={close}>
      <form onSubmit={submit} className="stack">
        <Field label={t('roles.name')} error={errors.field('name')}>
          <input className="input" name="role" autoComplete="off" required value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <Field label={`${t('roles.description')} (${t('form.optional')})`} error={errors.field('description')}>
          <input className="input" name="description" autoComplete="off" value={description} onChange={(e) => setDescription(e.target.value)} />
        </Field>
        {errors.box}
        <div className="modal-actions">
          <Button variant="ghost" type="button" onClick={close}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" type="submit" disabled={busy || name.trim().length < 2}>
            {busy ? <Spinner /> : <Icon name="plus" />} {t('roles.create')}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

function RoleDrawer({ id, onClose }: { id: number; onClose: () => void }) {
  const session = useSession()
  const role = useQuery({ queryKey: ['role', id], queryFn: () => api<Role>(`/role/${id}`) })
  const canUpdate = can(session, 'role.update')
  const grantable = useQuery({
    queryKey: ['grantable-permissions'],
    queryFn: () => api<Permission[]>('/role/grantable-permissions'),
    enabled: canUpdate,
  })
  return (
    <Drawer open title={role.data?.name ?? '…'} subtitle={role.data?.description ?? undefined} onClose={onClose} size="lg">
      {!role.data || (canUpdate && !grantable.data) ? (
        <Spinner />
      ) : (
        <RoleEditor key={role.data.updated_at} role={role.data} grantable={grantable.data ?? []} editable={canUpdate && !role.data.managed} onDeleted={onClose} />
      )}
    </Drawer>
  )
}

function RoleEditor({ role, grantable, editable, onDeleted }: { role: Role; grantable: Permission[]; editable: boolean; onDeleted: () => void }) {
  const t = useT()
  const session = useSession()
  const client = useQueryClient()
  const { push } = useToast()
  const errors = useApiErrors()
  const held = role.permissions ?? []
  const [name, setName] = useState(role.name)
  const [description, setDescription] = useState(role.description ?? '')
  const [chosen, setChosen] = useState<Set<number>>(() => new Set(held.map((p) => p.id)))
  const [busy, setBusy] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)

  // Every permission worth showing: what the editor may grant, plus whatever
  // the role already holds (shown even when the editor could not grant it).
  const byId = new Map<number, Permission>()
  for (const permission of [...grantable, ...held]) byId.set(permission.id, permission)
  const grantableIds = new Set(grantable.map((p) => p.id))
  const groups = new Map<string, Permission[]>()
  for (const permission of [...byId.values()].sort((a, b) => a.name.localeCompare(b.name))) {
    groups.set(permission.group, [...(groups.get(permission.group) ?? []), permission])
  }
  const visibleGroups = editable ? [...groups.entries()] : [...groups.entries()].map(([g, items]) => [g, items.filter((p) => chosen.has(p.id))] as const).filter(([, items]) => items.length > 0)

  const refresh = async () => {
    await client.invalidateQueries({ queryKey: ['roles'] })
    await client.invalidateQueries({ queryKey: ['role', role.id] })
    await client.invalidateQueries({ queryKey: ['session'] })
  }

  const run = async (action: () => Promise<unknown>) => {
    setBusy(true)
    errors.clear()
    try {
      await action()
      await refresh()
      push(t('form.saved'), 'success')
    } catch (cause) {
      errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
    } finally {
      setBusy(false)
    }
  }

  const saveDetails = (event: FormEvent) => {
    event.preventDefault()
    void run(() => api(`/role/${role.id}`, { method: 'PATCH', body: { name: name.trim(), description: description.trim() || null } }))
  }
  const savePermissions = () => void run(() => api('/role/assign-permission', { method: 'POST', body: { id: role.id, permissions: [...chosen] } }))

  const toggle = (id: number, on: boolean) =>
    setChosen((current) => {
      const next = new Set(current)
      if (on) next.add(id)
      else next.delete(id)
      return next
    })
  const setGroup = (items: Permission[], on: boolean) =>
    setChosen((current) => {
      const next = new Set(current)
      for (const p of items) {
        if (!grantableIds.has(p.id)) continue
        if (on) next.add(p.id)
        else next.delete(p.id)
      }
      return next
    })

  return (
    <div className="stack g-4">
      {role.managed ? (
        <div className="banner">
          <Icon name="shield" /> {t('roles.managedHint')}
        </div>
      ) : null}
      {editable ? (
        <form onSubmit={saveDetails} className="stack">
          <div className="form-grid-2">
            <Field label={t('roles.name')} error={errors.field('name')}>
              <input className="input" name="role" autoComplete="off" required value={name} onChange={(e) => setName(e.target.value)} />
            </Field>
            <Field label={`${t('roles.description')} (${t('form.optional')})`} error={errors.field('description')}>
              <input className="input" name="description" autoComplete="off" value={description} onChange={(e) => setDescription(e.target.value)} />
            </Field>
          </div>
          <div>
            <Button type="submit" icon="check" disabled={busy || name.trim().length < 2}>
              {t('roles.save')}
            </Button>
          </div>
        </form>
      ) : null}
      <div className="stack">
        <div className="flex">
          <span className="strong">{t('roles.permissions')}</span>
          <span className="small muted">{t('roles.permissionCount', { count: chosen.size })}</span>
        </div>
        {editable ? <p className="small muted">{t('roles.permissionsHint')}</p> : null}
        <div className="permission-groups">
          {visibleGroups.map(([group, items]) => (
            <fieldset key={group} className="permission-group">
              <legend>
                <span className="permission-group-name">{group}</span>
                {editable ? (
                  <span className="permission-group-actions">
                    <button type="button" className="link-button" onClick={() => setGroup(items, true)}>
                      {t('roles.selectAll')}
                    </button>
                    <button type="button" className="link-button" onClick={() => setGroup(items, false)}>
                      {t('roles.selectNone')}
                    </button>
                  </span>
                ) : null}
              </legend>
              {items.map((permission) => {
                const allowed = editable && grantableIds.has(permission.id)
                return (
                  <label key={permission.id} className="check-row permission-row" title={editable && !allowed ? t('roles.notGrantable') : undefined}>
                    <input type="checkbox" checked={chosen.has(permission.id)} disabled={!allowed || busy} onChange={(e) => toggle(permission.id, e.target.checked)} />
                    <span className="permission-meta">
                      <span>{permission.description || permission.name}</span>
                      <span className="small muted mono">{permission.name}</span>
                    </span>
                  </label>
                )
              })}
            </fieldset>
          ))}
        </div>
      </div>
      {errors.box}
      {editable ? (
        <div className="flex wrap">
          <Button variant="primary" icon="check" disabled={busy} onClick={savePermissions}>
            {t('roles.savePermissions')}
          </Button>
          <span className="spacer" />
          {can(session, 'role.delete') ? (
            <Button variant="danger" icon="trash" disabled={busy} onClick={() => setConfirmDelete(true)}>
              {t('roles.delete')}
            </Button>
          ) : null}
        </div>
      ) : null}
      <ConfirmDialog
        open={confirmDelete}
        title={t('roles.delete')}
        description={t('roles.deleteConfirm', { name: role.name })}
        confirmLabel={t('roles.delete')}
        danger
        busy={busy}
        onConfirm={() =>
          void (async () => {
            setBusy(true)
            try {
              await api(`/role/${role.id}`, { method: 'DELETE' })
              await client.invalidateQueries({ queryKey: ['roles'] })
              push(t('members.done'), 'success')
              setConfirmDelete(false)
              onDeleted()
            } catch (cause) {
              errors.setError(cause instanceof Error ? cause : new Error(String(cause)))
              setConfirmDelete(false)
            } finally {
              setBusy(false)
            }
          })()
        }
        onCancel={() => setConfirmDelete(false)}
      />
    </div>
  )
}
