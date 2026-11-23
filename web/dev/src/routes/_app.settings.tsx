import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { useDeferredValue, useEffect, useMemo, useState, type DragEvent } from 'react'
import { api } from '../api/client'
import type { DevTarget, DevTenant, Page, SettingsGroup, SettingsItem } from '../api/types'
import { sourceTone } from '../format'
import { Select } from '../components/inputs'
import { SettingsList } from '../components/SettingsList'
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
  Modal,
  PageHeader,
  StatTile,
} from '../components/ui'

export const Route = createFileRoute('/_app/settings')({
  validateSearch: (search: Record<string, unknown>): { tenant?: number } => ({
    tenant: typeof search.tenant === 'number' ? search.tenant : undefined,
  }),
  component: SettingsPage,
})

type TargetKind = 'global' | 'tenant'

/** Loads the tenant named in ?tenant= (deep link from the Tenants panel). */
function useLinkedTenant(id: number | undefined) {
  return useQuery({
    queryKey: ['tenants', 'detail', id],
    queryFn: ({ signal }) => api<DevTenant>(`/dev/api/tenants/${id}`, { signal }),
    enabled: id !== undefined,
    select: (tenant): DevTarget => ({
      id: tenant.id,
      type: 'tenant',
      name: tenant.name,
      code: tenant.code,
      overrides: tenant.overrides,
    }),
  })
}

type DragState = { type: 'setting'; key: string } | { type: 'group'; id: number } | null

interface GroupEditor {
  mode: 'create' | 'edit'
  id?: number
  name: string
  description: string
  sortOrder: string
}

const displayValue = (value: unknown): string => {
  if (value === null || value === undefined) return '-'
  if (typeof value === 'boolean') return value ? 'on' : 'off'
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

const inputValue = (item: SettingsItem): string => {
  if (item.type === 'json') return JSON.stringify(item.value ?? null, null, 2)
  if (item.value === null || item.value === undefined) return ''
  return String(item.value)
}

const groupKey = (group: SettingsGroup): string => (group.id > 0 ? `id:${group.id}` : `slug:${group.slug}`)

const targetLabel = (kind: TargetKind, tenant: DevTarget | null): string => {
  if (kind === 'global') return 'global'
  return tenant?.name ?? 'the selected tenant'
}

function SettingsPage() {
  const { push } = useToast()
  const queryClient = useQueryClient()
  const [targetKind, setTargetKind] = useState<TargetKind>('global')
  const [view, setView] = useState<'list' | 'board'>('list')
  const { tenant: linkedTenantID } = Route.useSearch()
  const linkedTenant = useLinkedTenant(linkedTenantID)
  useEffect(() => {
    if (!linkedTenant.data) return
    setTargetKind('tenant')
    setTenant(linkedTenant.data)
  }, [linkedTenant.data])
  const [tenant, setTenant] = useState<DevTarget | null>(null)
  const [tenantSearch, setTenantSearch] = useState('')
  const [groupSearch, setGroupSearch] = useState('')
  const [settingSearch, setSettingSearch] = useState('')
  const [selectedGroupKey, setSelectedGroupKey] = useState<string | null>(null)
  const [dragging, setDragging] = useState<DragState>(null)
  const [overGroup, setOverGroup] = useState<number | null>(null)
  const [groupEditor, setGroupEditor] = useState<GroupEditor | null>(null)
  const [groupToDelete, setGroupToDelete] = useState<SettingsGroup | null>(null)
  const [editingKey, setEditingKey] = useState<string | null>(null)
  const [movingKey, setMovingKey] = useState<string | null>(null)
  const [moveTargetId, setMoveTargetId] = useState('')
  const [formValue, setFormValue] = useState('')
  const [formError, setFormError] = useState<string | null>(null)
  const deferredTenantSearch = useDeferredValue(tenantSearch)

  const targetID = targetKind === 'tenant' ? tenant?.id : undefined
  const targetReady = targetKind === 'global' || Boolean(targetID)
  const effectiveKey = ['settings', 'effective', targetKind, targetID ?? 0]

  const groups = useQuery({
    queryKey: ['settings', 'groups'],
    queryFn: ({ signal }) => api<SettingsGroup[]>('/dev/api/settings/groups', { signal }),
  })

  const tenants = useQuery({
    queryKey: ['settings', 'tenants', deferredTenantSearch],
    queryFn: ({ signal }) =>
      api<Page<DevTarget>>('/dev/api/targets', {
        query: { type: 'tenant', search: deferredTenantSearch || undefined, per_page: 20 },
        signal,
        unwrap: false,
      }),
    enabled: targetKind === 'tenant',
    placeholderData: keepPreviousData,
  })

  const effective = useQuery({
    queryKey: effectiveKey,
    queryFn: ({ signal }) =>
      api<SettingsItem[]>('/dev/api/settings', {
        query: { target_type: targetKind, target_id: targetID },
        signal,
      }),
    enabled: targetReady,
  })

  const globalSettings = useQuery({
    queryKey: ['settings', 'effective', 'global', 0],
    queryFn: ({ signal }) => api<SettingsItem[]>('/dev/api/settings', { signal }),
    enabled: targetKind === 'tenant',
    staleTime: 30_000,
  })

  useEffect(() => {
    if (targetKind !== 'tenant' || tenant) return
    const first = tenants.data?.data[0]
    if (first) setTenant(first)
  }, [targetKind, tenant, tenants.data])

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['settings'] })
    void queryClient.invalidateQueries({ queryKey: ['actions'] })
    void queryClient.invalidateQueries({ queryKey: ['overview'] })
  }

  const assignGroup = useMutation({
    mutationFn: ({ key, groupId }: { key: string; groupId: number }) =>
      api<{ key: string; group_id: number }>(`/dev/api/settings/${encodeURIComponent(key)}/group`, {
        method: 'PATCH',
        body: { group_id: groupId },
      }),
    onSuccess: (_result, variables) => {
      const group = (groups.data ?? []).find((item) => item.id === variables.groupId)
      push(`${variables.key} moved to ${group?.name ?? 'the new group'}`, 'success')
      setMovingKey(null)
      void effective.refetch()
      void groups.refetch()
      void queryClient.invalidateQueries({ queryKey: ['actions'] })
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Unable to move the setting', 'error'),
  })

  const reorderGroups = useMutation({
    mutationFn: async (ordered: SettingsGroup[]) => {
      for (let index = 0; index < ordered.length; index++) {
        const group = ordered[index]
        await api<SettingsGroup>(`/dev/api/settings/groups/${group.id}`, {
          method: 'PATCH',
          body: {
            name: group.name,
            description: group.description ?? null,
            sort_order: index + 1,
          },
        })
      }
    },
    onSuccess: () => {
      push('Group order updated', 'success')
      invalidate()
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Unable to reorder groups', 'error'),
  })

  const saveGroup = useMutation({
    mutationFn: (editor: GroupEditor) => {
      const body = {
        name: editor.name.trim(),
        description: editor.description.trim() || null,
        sort_order: Number.parseInt(editor.sortOrder, 10) || 0,
      }
      if (editor.mode === 'create') {
        return api<SettingsGroup>('/dev/api/settings/groups', { method: 'POST', body })
      }
      return api<SettingsGroup>(`/dev/api/settings/groups/${editor.id}`, { method: 'PATCH', body })
    },
    onSuccess: () => {
      setGroupEditor(null)
      push('Settings group saved', 'success')
      invalidate()
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Unable to save the group', 'error'),
  })

  const deleteGroup = useMutation({
    mutationFn: (id: number) => api<{ deleted: boolean }>(`/dev/api/settings/groups/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      setGroupToDelete(null)
      push('Settings group deleted; assigned settings fell back to their catalog groups', 'success')
      invalidate()
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Unable to delete the group', 'error'),
  })

  const placeSettings = useMutation({
    mutationFn: ({ groupId, keys }: { groupId: number; keys: string[] }) =>
      api(`/dev/api/settings/groups/${groupId}/keys`, { method: 'PUT', body: { keys } }),
    onSuccess: () => invalidate(),
    onError: (error) => push(error instanceof Error ? error.message : 'Unable to move the setting', 'error'),
  })

  const reorderGroupIds = useMutation({
    mutationFn: (ids: number[]) => api('/dev/api/settings/groups/order', { method: 'PUT', body: { ids } }),
    onSuccess: () => invalidate(),
    onError: (error) => push(error instanceof Error ? error.message : 'Unable to reorder groups', 'error'),
  })

  const saveSetting = useMutation({
    mutationFn: ({ item, value }: { item: SettingsItem; value: unknown }) =>
      api<SettingsItem>(`/dev/api/settings/${encodeURIComponent(item.key)}`, {
        method: 'PATCH',
        body: { target_type: targetKind, target_id: targetID, value },
      }),
    onSuccess: (_result, variables) => {
      push(`${variables.item.key} updated for ${targetLabel(targetKind, tenant)}`, 'success')
      setEditingKey(null)
      invalidate()
    },
    onError: (error) => {
      const message = error instanceof Error ? error.message : 'Unable to save the setting'
      setFormError(message)
      push(message, 'error')
    },
  })

  const resetSetting = useMutation({
    mutationFn: (item: SettingsItem) =>
      api<{ reset: boolean }>(`/dev/api/settings/${encodeURIComponent(item.key)}`, {
        method: 'DELETE',
        query: { target_type: targetKind, target_id: targetID },
      }),
    onSuccess: (_result, item) => {
      push(`${item.key} now inherits again`, 'success')
      setEditingKey(null)
      invalidate()
    },
    onError: (error) => {
      const message = error instanceof Error ? error.message : 'Unable to reset the setting'
      setFormError(message)
      push(message, 'error')
    },
  })

  const sync = useMutation({
    mutationFn: () => api<{ created: number }>('/dev/api/settings/sync', { method: 'POST' }),
    onSuccess: (result) => {
      push(
        result.created > 0
          ? `Catalog synced - created ${result.created} setting${result.created === 1 ? '' : 's'}`
          : 'Catalog is already in sync',
        'success',
      )
      invalidate()
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Settings sync failed', 'error'),
  })

  const settings = effective.data ?? []
  const groupList = groups.data ?? []
  const boardGroups = useMemo(() => {
    const known = new Set(groupList.map((group) => group.id))
    const extra = new Map<string, SettingsGroup>()
    for (const item of settings) {
      if (!known.has(item.group.id)) extra.set(groupKey(item.group), item.group)
    }
    return [...groupList, ...extra.values()].sort((left, right) => {
      if (left.sort_order !== right.sort_order) return left.sort_order - right.sort_order
      return left.name.localeCompare(right.name)
    })
  }, [groupList, settings])

  const byGroup = useMemo(() => {
    const map = new Map<string, SettingsItem[]>()
    for (const group of boardGroups) map.set(groupKey(group), [])
    for (const item of settings) {
      const key = groupKey(item.group)
      const list = map.get(key) ?? []
      list.push(item)
      map.set(key, list)
    }
    return map
  }, [boardGroups, settings])

  const visibleGroups = useMemo(() => {
    const needle = groupSearch.trim().toLowerCase()
    if (!needle) return boardGroups
    return boardGroups.filter((group) =>
      [group.name, group.slug, group.description].some((value) => value?.toLowerCase().includes(needle)),
    )
  }, [boardGroups, groupSearch])

  const selectedGroup = boardGroups.find((group) => groupKey(group) === selectedGroupKey) ?? null
  const selectedSettings = selectedGroup ? byGroup.get(groupKey(selectedGroup)) ?? [] : []
  const visibleSettings = useMemo(() => {
    const needle = settingSearch.trim().toLowerCase()
    if (!needle) return selectedSettings
    return selectedSettings.filter((item) =>
      [item.key, item.description, item.group.name].some((value) => value.toLowerCase().includes(needle)),
    )
  }, [selectedSettings, settingSearch])

  useEffect(() => {
    if (boardGroups.length === 0) {
      if (selectedGroupKey) setSelectedGroupKey(null)
      return
    }
    if (selectedGroupKey && boardGroups.some((group) => groupKey(group) === selectedGroupKey)) return
    setSelectedGroupKey(groupKey(boardGroups[0]))
  }, [boardGroups, selectedGroupKey])

  const editing = editingKey ? settings.find((item) => item.key === editingKey) ?? null : null
  const editingGlobal = editing
    ? targetKind === 'global'
      ? editing
      : globalSettings.data?.find((item) => item.key === editing.key) ?? null
    : null
  const moving = movingKey ? settings.find((item) => item.key === movingKey) ?? null : null
  const moveTargets = boardGroups.filter((group) => group.id > 0 && group.id !== moving?.group.id)
  const groupEditorIsSystem =
    groupEditor?.mode === 'edit'
      ? boardGroups.find((group) => group.id === groupEditor.id)?.is_system ?? false
      : false
  const overriddenCount = settings.filter((item) => item.overridden).length
  const editableCount = settings.filter((item) => item.editable).length

  const openEditor = (item: SettingsItem) => {
    setEditingKey(item.key)
    setFormValue(inputValue(item))
    setFormError(null)
  }

  const openMove = (item: SettingsItem) => {
    const target = boardGroups.find((group) => group.id > 0 && group.id !== item.group.id)
    setMovingKey(item.key)
    setMoveTargetId(target ? String(target.id) : '')
  }

  const chooseTargetKind = (kind: TargetKind) => {
    if (kind === targetKind) return
    setTargetKind(kind)
    setEditingKey(null)
    if (kind === 'tenant') setTenant(null)
  }

  const onSettingDragStart = (event: DragEvent, key: string) => {
    event.dataTransfer.setData('application/x-setting-key', key)
    event.dataTransfer.setData('text/plain', key)
    event.dataTransfer.effectAllowed = 'move'
    setDragging({ type: 'setting', key })
  }

  const onGroupDragStart = (event: DragEvent, id: number) => {
    event.dataTransfer.setData('application/x-group-id', String(id))
    event.dataTransfer.setData('text/plain', `group:${id}`)
    event.dataTransfer.effectAllowed = 'move'
    setDragging({ type: 'group', id })
  }

  const onGroupDrop = (event: DragEvent, group: SettingsGroup) => {
    event.preventDefault()
    const settingKey = event.dataTransfer.getData('application/x-setting-key')
    const groupID = Number.parseInt(event.dataTransfer.getData('application/x-group-id'), 10)
    setOverGroup(null)
    setDragging(null)
    if (group.id <= 0) {
      push('Run Sync catalog before assigning settings to this group', 'error')
      return
    }
    if (settingKey) {
      const item = settings.find((candidate) => candidate.key === settingKey)
      if (item && item.group.id === group.id) return
      assignGroup.mutate({ key: settingKey, groupId: group.id })
      return
    }
    if (groupID) {
      const from = boardGroups.findIndex((candidate) => candidate.id === groupID)
      const to = boardGroups.findIndex((candidate) => candidate.id === group.id)
      if (from < 0 || to < 0 || from === to) return
      const next = [...boardGroups]
      const [moved] = next.splice(from, 1)
      next.splice(to, 0, moved)
      reorderGroups.mutate(next)
    }
  }

  const saveEditing = () => {
    if (!editing) return
    setFormError(null)
    let value: unknown = formValue
    if (editing.type === 'bool') {
      value = formValue === 'true'
    } else if (editing.type === 'int') {
      const parsed = Number.parseInt(formValue, 10)
      if (!Number.isFinite(parsed)) {
        setFormError('Enter a valid integer.')
        return
      }
      value = parsed
    } else if (editing.type === 'json') {
      try {
        value = JSON.parse(formValue)
      } catch {
        setFormError('Enter valid JSON.')
        return
      }
    }
    saveSetting.mutate({ item: editing, value })
  }

  const toggleBool = (item: SettingsItem) => {
    if (!item.editable || saveSetting.isPending) return
    saveSetting.mutate({ item, value: !Boolean(item.value) })
  }

  return (
    <>
      <PageHeader
        title="Settings"
        description="Grouped runtime settings with global and tenant overrides. Drag settings onto a group, or use the editor's group selector."
        actions={
          <>
            <div className="pill-toggle" role="tablist" aria-label="Layout">
              <button type="button" className={view === 'list' ? 'active' : ''} onClick={() => setView('list')}>
                All settings
              </button>
              <button type="button" className={view === 'board' ? 'active' : ''} onClick={() => setView('board')}>
                Groups board
              </button>
            </div>
            <Button icon="refresh" onClick={() => invalidate()} disabled={effective.isFetching || groups.isFetching}>
              Refresh
            </Button>
            <Button variant="primary" icon="settings" onClick={() => sync.mutate()} disabled={sync.isPending}>
              {sync.isPending ? 'Syncing...' : 'Sync catalog'}
            </Button>
          </>
        }
      />

      <Card>
        <CardHeader
          title="Resolution target"
          sub="Settings resolve tenant -> global -> code default. Per-user overrides are not available yet."
          actions={
            <div className="pill-toggle">
              {(['global', 'tenant'] as TargetKind[]).map((kind) => (
                <button
                  key={kind}
                  type="button"
                  className={targetKind === kind ? 'active' : ''}
                  onClick={() => chooseTargetKind(kind)}
                >
                  {kind === 'global' ? 'Global' : 'Tenant'}
                </button>
              ))}
            </div>
          }
        />
        {targetKind === 'global' ? (
          <div className="flex g-3">
            <Badge tone="accent">global</Badge>
            <span className="small dim">
              Values written here apply to every tenant without an override. Global-only keys accept a global
              value only.
            </span>
          </div>
        ) : (
          <div className="filters mb-0">
            <label className="field grow">
              <span className="field-label">Search tenants</span>
              <input
                className="input"
                placeholder="Tenant name or code"
                value={tenantSearch}
                onChange={(event) => setTenantSearch(event.target.value)}
              />
            </label>
            <label className="field grow">
              <span className="field-label">Tenant</span>
              <Select
                value={tenant ? String(tenant.id) : ''}
                placeholder="Select a tenant…"
                onChange={(value) => {
                  const id = Number.parseInt(value, 10)
                  setTenant([...(tenants.data?.data ?? []), ...(tenant ? [tenant] : [])].find((item) => item.id === id) ?? null)
                  setEditingKey(null)
                }}
                options={[
                  ...(tenant && !(tenants.data?.data ?? []).some((item) => item.id === tenant.id)
                    ? [{ value: String(tenant.id), label: tenant.name }]
                    : []),
                  ...(tenants.data?.data ?? []).map((item) => ({
                    value: String(item.id),
                    label: item.name,
                    text: `${item.name} ${item.code ?? ''}`,
                    hint: item.code ?? `#${item.id}`,
                  })),
                ]}
              />
            </label>
            {tenants.isFetching ? (
              <span className="small dim field-pad">
                <span className="spinner" /> searching
              </span>
            ) : null}
          </div>
        )}
      </Card>

      <div className="grid cols-4 mt-24">
        <StatTile label="Settings" value={settings.length} hint="catalog keys" />
        <StatTile label="Groups" value={boardGroups.length} hint="catalog + custom" />
        <StatTile
          label="Set here"
          value={overriddenCount}
          tone={overriddenCount > 0 ? 'accent' : undefined}
          hint={`stored at ${targetKind}`}
        />
        <StatTile label="Editable" value={editableCount} hint="at this target" />
      </div>

      {groups.isLoading || (targetKind === 'global' ? effective.isLoading : effective.isLoading && targetReady) ? (
        <Card className="mt-24">
          <LoadingBlock label="Loading settings..." />
        </Card>
      ) : groups.isError ? (
        <Card className="mt-24">
          <ErrorBox message={groups.error instanceof Error ? groups.error.message : 'Unable to load settings groups.'} />
        </Card>
      ) : effective.isError ? (
        <Card className="mt-24">
          <ErrorBox message={effective.error instanceof Error ? effective.error.message : 'Unable to load settings.'} />
        </Card>
      ) : groupList.length === 0 ? (
        <Card className="mt-24">
          <EmptyState
            title="No settings groups yet"
            description="Run the catalog sync to materialize the built-in groups and settings. You can then drag keys between groups."
            icon="settings"
            action={
              <Button variant="primary" icon="settings" onClick={() => sync.mutate()} disabled={sync.isPending}>
                Sync catalog
              </Button>
            }
          />
        </Card>
      ) : !targetReady ? (
        <Card className="mt-24">
          <EmptyState title="Choose a tenant" description="Select a tenant above to inspect its overrides." icon="settings" />
        </Card>
      ) : view === 'list' ? (
        <SettingsList
          items={settings}
          groups={boardGroups}
          target={targetLabel(targetKind, tenant)}
          savingKey={
            saveSetting.isPending
              ? saveSetting.variables?.item.key ?? null
              : resetSetting.isPending
                ? resetSetting.variables?.key ?? null
                : null
          }
          onSave={(item, value) => saveSetting.mutate({ item, value })}
          onReset={(item) => resetSetting.mutate(item)}
          onDetails={openEditor}
          onPlace={(groupId, keys) => placeSettings.mutate({ groupId, keys })}
          onReorderGroups={(ids) => reorderGroupIds.mutate(ids)}
          onNewGroup={() =>
            setGroupEditor({ mode: 'create', name: '', description: '', sortOrder: String(boardGroups.length + 1) })
          }
          onEditGroup={(group) =>
            setGroupEditor({
              mode: 'edit',
              id: group.id,
              name: group.name,
              description: group.description ?? '',
              sortOrder: String(group.sort_order),
            })
          }
          onDeleteGroup={setGroupToDelete}
        />
      ) : (
        <div className="settings-layout mt-24">
          <Card className="group-pane">
            <CardHeader
              title="Groups"
              sub={`${boardGroups.length} total - ${visibleGroups.length} shown`}
              actions={
                <Button
                  size="sm"
                  variant="ghost"
                  icon="plus"
                  onClick={() =>
                    setGroupEditor({
                      mode: 'create',
                      name: '',
                      description: '',
                      sortOrder: String(boardGroups.length + 1),
                    })
                  }
                >
                  New group
                </Button>
              }
            />
            <input
              className="input"
              placeholder="Search groups"
              value={groupSearch}
              onChange={(event) => setGroupSearch(event.target.value)}
            />
            <div className="group-list">
              {visibleGroups.length === 0 ? (
                <div className="small dim drop-hint">No groups match.</div>
              ) : (
                visibleGroups.map((group) => {
                  const items = byGroup.get(groupKey(group)) ?? []
                  const active = groupKey(group) === selectedGroupKey
                  return (
                    <div
                      key={groupKey(group)}
                      role="button"
                      tabIndex={0}
                      className={`group-list-item ${active ? 'selected' : ''} ${overGroup === group.id ? 'drop-target' : ''}`}
                      onClick={() => {
                        setSelectedGroupKey(groupKey(group))
                        setSettingSearch('')
                      }}
                      onKeyDown={(event) => {
                        if (event.key === 'Enter' || event.key === ' ') {
                          event.preventDefault()
                          setSelectedGroupKey(groupKey(group))
                          setSettingSearch('')
                        }
                      }}
                      onDragOver={(event) => {
                        if (!dragging || group.id <= 0) return
                        event.preventDefault()
                        event.dataTransfer.dropEffect = 'move'
                        setOverGroup(group.id)
                      }}
                      onDragLeave={(event) => {
                        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setOverGroup(null)
                      }}
                      onDrop={(event) => onGroupDrop(event, group)}
                    >
                      <span
                        className="drag-handle"
                        title={group.id > 0 ? 'Drag to reorder groups' : 'Run Sync catalog first'}
                        draggable={group.id > 0}
                        onDragStart={(event) => {
                          if (group.id > 0) onGroupDragStart(event, group.id)
                        }}
                        onDragEnd={() => {
                          setDragging(null)
                          setOverGroup(null)
                        }}
                        onClick={(event) => event.stopPropagation()}
                      >
                        <Icon name="grip" />
                      </span>
                      <div className="min-0 fill">
                        <div className="flex g-2 min-0">
                          <span className={active ? 'group-name active' : 'group-name'}>{group.name}</span>
                          {group.is_system ? <Badge>system</Badge> : null}
                          {group.id <= 0 ? <Badge tone="warn">needs sync</Badge> : null}
                        </div>
                        <div className="small dim truncate">{group.description || group.slug}</div>
                      </div>
                      <Badge>{items.length}</Badge>
                    </div>
                  )
                })
              )}
            </div>
          </Card>

          <Card className="group-detail">
            {selectedGroup ? (
              <>
                <CardHeader
                  title={selectedGroup.name}
                  sub={selectedGroup.description || selectedGroup.slug}
                  actions={
                    <div className="flex g-2">
                      <Badge tone={selectedGroup.is_system ? 'neutral' : 'info'}>
                        {selectedGroup.is_system ? 'system' : 'custom'}
                      </Badge>
                      <Badge>{selectedSettings.length} settings</Badge>
                      {selectedGroup.id > 0 ? (
                        <>
                          <Button
                            size="sm"
                            icon="pencil"
                            onClick={() =>
                              setGroupEditor({
                                mode: 'edit',
                                id: selectedGroup.id,
                                name: selectedGroup.name,
                                description: selectedGroup.description ?? '',
                                sortOrder: String(selectedGroup.sort_order),
                              })
                            }
                          >
                            Edit
                          </Button>
                          {!selectedGroup.is_system ? (
                            <Button
                              size="sm"
                              variant="danger"
                              icon="trash"
                              onClick={() => setGroupToDelete(selectedGroup)}
                            >
                              Delete
                            </Button>
                          ) : null}
                        </>
                      ) : null}
                    </div>
                  }
                />
                <div className="filters">
                  <label className="field grow">
                    <span className="field-label">Filter settings</span>
                    <input
                      className="input"
                      placeholder="Key or description"
                      value={settingSearch}
                      onChange={(event) => setSettingSearch(event.target.value)}
                    />
                  </label>
                </div>
                {visibleSettings.length === 0 ? (
                  <EmptyState
                    title={
                      selectedSettings.length === 0
                        ? selectedGroup.slug === 'ungrouped'
                          ? 'Ungrouped is empty'
                          : 'No settings in this group'
                        : 'No settings match'
                    }
                    description={
                      selectedSettings.length === 0
                        ? selectedGroup.slug === 'ungrouped'
                          ? 'Drag a setting here to park it, or use the Move button on any setting.'
                          : 'Drag a setting onto this group in the left list, or use the Move button on a setting.'
                        : 'Try a different filter.'
                    }
                    icon="settings"
                  />
                ) : (
                  <div className="setting-list">
                    {visibleSettings.map((item) => (
                      <div
                        key={item.key}
                        className={`setting-row ${dragging?.type === 'setting' && dragging.key === item.key ? 'dragging' : ''}`}
                        draggable
                        onDragStart={(event) => onSettingDragStart(event, item.key)}
                        onDragEnd={() => {
                          setDragging(null)
                          setOverGroup(null)
                        }}
                      >
                        <span className="drag-handle" title="Drag to another group">
                          <Icon name="grip" />
                        </span>
                        <span className="setting-main">
                          <span className="flex g-2 min-0">
                            <span className="setting-key">{item.key}</span>
                            <Badge>{item.type}</Badge>
                            {!item.editable ? <Badge>global only</Badge> : null}
                          </span>
                          <span className="small dim truncate">{item.description}</span>
                        </span>
                        <span className="setting-value" title={displayValue(item.value)}>
                          {displayValue(item.value)}
                        </span>
                        <Badge tone={sourceTone(item.source)}>{item.source}</Badge>
                        {item.overridden ? <Badge tone="ok">set here</Badge> : null}
                        {item.type === 'bool' ? (
                          <button
                            type="button"
                            className={`switch ${item.value ? 'on' : ''}`}
                            role="switch"
                            aria-checked={Boolean(item.value)}
                            aria-label={`${item.key} at ${targetLabel(targetKind, tenant)}`}
                            disabled={!item.editable || saveSetting.isPending}
                            onClick={() => toggleBool(item)}
                          />
                        ) : null}
                        <Button
                          size="sm"
                          variant="ghost"
                          icon="folder"
                          title="Move to group"
                          onClick={() => openMove(item)}
                        >
                          Move
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          icon="settings"
                          title="Edit value and details"
                          onClick={() => openEditor(item)}
                        />
                      </div>
                    ))}
                  </div>
                )}
              </>
            ) : (
              <EmptyState title="Select a group" description="Choose a group from the list to manage its settings." icon="settings" />
            )}
          </Card>
        </div>
      )}

      <Modal
        open={groupEditor !== null}
        title={groupEditor?.mode === 'create' ? 'New settings group' : 'Edit settings group'}
        description={
          groupEditorIsSystem
            ? 'Catalog-managed group: renaming works now, but Sync catalog restores the catalog name, description and sort order. The slug stays stable.'
            : groupEditor?.mode === 'create'
              ? 'Create a custom group. You can drag settings into it right after saving.'
              : 'Rename, describe or reorder this custom group.'
        }
        onClose={() => setGroupEditor(null)}
        actions={
          <>
            <Button variant="ghost" onClick={() => setGroupEditor(null)} disabled={saveGroup.isPending}>
              Cancel
            </Button>
            <Button
              variant="primary"
              icon="check"
              disabled={!groupEditor || saveGroup.isPending || groupEditor.name.trim().length < 2}
              onClick={() => groupEditor && saveGroup.mutate(groupEditor)}
            >
              {saveGroup.isPending ? 'Saving...' : 'Save group'}
            </Button>
          </>
        }
      >
        {groupEditor ? (
          <div className="stack">
            <label className="field">
              <span className="field-label">Name</span>
              <input
                className="input"
                autoFocus
                value={groupEditor.name}
                onChange={(event) => setGroupEditor({ ...groupEditor, name: event.target.value })}
              />
            </label>
            <label className="field">
              <span className="field-label">Description</span>
              <input
                className="input"
                value={groupEditor.description}
                onChange={(event) => setGroupEditor({ ...groupEditor, description: event.target.value })}
              />
            </label>
            <label className="field">
              <span className="field-label">Sort order</span>
              <input
                className="input"
                inputMode="numeric"
                value={groupEditor.sortOrder}
                onChange={(event) =>
                  setGroupEditor({ ...groupEditor, sortOrder: event.target.value.replace(/\D/g, '').slice(0, 4) })
                }
              />
            </label>
            {groupEditorIsSystem ? (
              <div className="small warn-text">
                This is a catalog group; the next Sync catalog will restore its catalog metadata.
              </div>
            ) : null}
          </div>
        ) : null}
      </Modal>

      <Drawer
        open={editing !== null}
        title={editing?.key ?? 'Setting'}
        subtitle={editing?.description}
        onClose={() => setEditingKey(null)}
        actions={
          editing ? (
            <Button
              size="sm"
              variant="ghost"
              icon="refresh"
              disabled={!editing.overridden || resetSetting.isPending}
              onClick={() => resetSetting.mutate(editing)}
            >
              Reset
            </Button>
          ) : undefined
        }
      >
        {editing ? (
          <div className="stack">
            <div className="flex g-2 wrap">
              <Badge>{editing.type}</Badge>
              <Badge tone={sourceTone(editing.source)}>{editing.source}</Badge>
              <Badge>{editing.scope} scope</Badge>
              {editing.overridden ? <Badge tone="ok">set here</Badge> : null}
            </div>
            <dl className="kv">
              <dt>Code default</dt>
              <dd>{displayValue(editing.default)}</dd>
              <dt>Global</dt>
              <dd>{editingGlobal ? displayValue(editingGlobal.value) : 'catalog default'}</dd>
              {targetKind === 'tenant' ? (
                <>
                  <dt>Tenant</dt>
                  <dd>{editing.overridden ? displayValue(editing.value) : 'inherit'}</dd>
                </>
              ) : null}
              <dt>Effective</dt>
              <dd>
                {displayValue(editing.value)} - {editing.source}
              </dd>
            </dl>
            <label className="field">
              <span className="field-label">Move to group</span>
              <Select
                value={editing.group.id > 0 ? String(editing.group.id) : ''}
                placeholder="Unsynced group - run Sync catalog"
                disabled={assignGroup.isPending}
                onChange={(value) => {
                  const id = Number.parseInt(value, 10)
                  if (!id || id === editing.group.id) return
                  assignGroup.mutate({ key: editing.key, groupId: id })
                }}
                options={boardGroups
                  .filter((group) => group.id > 0)
                  .map((group) => ({ value: String(group.id), label: group.name }))}
              />
            </label>
            {!editing.editable ? (
              <ErrorBox message="This key is global-only; it cannot be overridden per tenant." />
            ) : null}
            <label className="field">
              <span className="field-label">Value</span>
              {editing.type === 'bool' ? (
                <Select
                  value={formValue === 'true' ? 'true' : 'false'}
                  disabled={!editing.editable}
                  onChange={setFormValue}
                  options={[
                    { value: 'true', label: 'Enabled' },
                    { value: 'false', label: 'Disabled' },
                  ]}
                />
              ) : editing.type === 'json' ? (
                <textarea
                  className="textarea mono"
                  rows={8}
                  value={formValue}
                  disabled={!editing.editable}
                  onChange={(event) => setFormValue(event.target.value)}
                />
              ) : (
                <input
                  className="input"
                  inputMode={editing.type === 'int' ? 'numeric' : undefined}
                  value={formValue}
                  disabled={!editing.editable}
                  onChange={(event) => setFormValue(event.target.value)}
                />
              )}
            </label>
            {formError ? <ErrorBox message={formError} /> : null}
            <div className="flex">
              <Button
                variant="primary"
                icon="check"
                disabled={!editing.editable || saveSetting.isPending}
                onClick={saveEditing}
              >
                {saveSetting.isPending ? 'Saving...' : `Save at ${targetLabel(targetKind, tenant)}`}
              </Button>
              <Button
                variant="ghost"
                icon="refresh"
                disabled={!editing.overridden || resetSetting.isPending}
                onClick={() => resetSetting.mutate(editing)}
              >
                Clear stored value
              </Button>
            </div>
          </div>
        ) : null}
      </Drawer>

      <Modal
        open={moving !== null}
        title={`Move ${moving?.key ?? 'setting'}`}
        description="Choose a destination group. The move is saved immediately."
        onClose={() => setMovingKey(null)}
        actions={
          <>
            <Button variant="ghost" onClick={() => setMovingKey(null)} disabled={assignGroup.isPending}>
              Cancel
            </Button>
            <Button
              variant="primary"
              icon="folder"
              disabled={!moving || !moveTargetId || assignGroup.isPending}
              onClick={() => {
                if (!moving || !moveTargetId) return
                assignGroup.mutate({ key: moving.key, groupId: Number.parseInt(moveTargetId, 10) })
              }}
            >
              {assignGroup.isPending ? 'Moving...' : 'Move'}
            </Button>
          </>
        }
      >
        {moving ? (
          <div className="stack">
            <label className="field">
              <span className="field-label">Destination group</span>
              <Select
                value={moveTargetId}
                placeholder="No other groups"
                onChange={setMoveTargetId}
                options={moveTargets.map((group) => ({
                  value: String(group.id),
                  label: group.name,
                  hint: group.is_system ? 'system' : undefined,
                }))}
              />
            </label>
            <div className="small dim">
              Currently in <b>{moving.group.name}</b>. You can also drag the row onto a group in the left list.
            </div>
            {moving.group.id <= 0 ? (
              <div className="small warn-text">
                Run Sync catalog first so this setting has a materialized group row.
              </div>
            ) : null}
          </div>
        ) : null}
      </Modal>

      <ConfirmDialog
        open={groupToDelete !== null}
        title={`Delete ${groupToDelete?.name ?? 'group'}?`}
        description="Settings assigned to this group fall back to their catalog groups. The settings themselves are not deleted."
        confirmLabel="Delete group"
        danger
        busy={deleteGroup.isPending}
        onCancel={() => setGroupToDelete(null)}
        onConfirm={() => {
          if (groupToDelete) deleteGroup.mutate(groupToDelete.id)
        }}
      />
    </>
  )
}
