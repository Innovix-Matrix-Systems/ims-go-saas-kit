import { useEffect, useMemo, useRef, useState, type DragEvent } from 'react'
import type { SettingsGroup, SettingsItem } from '../api/types'
import { sourceTone } from '../format'
import { Select } from './inputs'
import { Button, Card, Badge, EmptyState, Icon } from './ui'

/** Unit shown after a value, inferred from the key ("…_days" → days). */
function unitFor(key: string): string | undefined {
  const match = /_(days|hours|minutes|seconds|mb|kb|percent)$/.exec(key)
  if (!match) return undefined
  return match[1] === 'mb' ? 'MB' : match[1] === 'kb' ? 'KB' : match[1] === 'percent' ? '%' : match[1]
}

function humanize(key: string): string {
  const last = key.split('.').pop() ?? key
  return last.replace(/_/g, ' ').replace(/\b\w/g, (letter) => letter.toUpperCase())
}

function show(value: unknown): string {
  if (value === null || value === undefined) return '—'
  if (typeof value === 'boolean') return value ? 'On' : 'Off'
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

const typeLabel: Record<SettingsItem['type'], string> = { string: 'string', int: 'integer', bool: 'boolean', json: 'json' }

type Drag = { type: 'setting'; key: string } | { type: 'group'; id: number } | null

const UNGROUPED = 'ungrouped'

/**
 * Every setting on one page, grouped, with inline editors (like a Laravel
 * settings screen): switches for booleans, inputs that save on Enter/blur,
 * the code default and a reset link under each control.
 *
 * Drag and drop: drag a setting by its grip onto another setting (it lands
 * before it) or onto a group (it lands at the end). Ungrouped settings wait
 * in the left rail; drag groups in the rail to reorder them.
 */
export function SettingsList({
  items,
  groups,
  target,
  savingKey,
  onSave,
  onReset,
  onDetails,
  onPlace,
  onReorderGroups,
  onNewGroup,
  onEditGroup,
  onDeleteGroup,
}: {
  items: SettingsItem[]
  groups: SettingsGroup[]
  target: string
  savingKey: string | null
  onSave: (item: SettingsItem, value: unknown) => void
  onReset: (item: SettingsItem) => void
  onDetails: (item: SettingsItem) => void
  onPlace: (groupId: number, keys: string[]) => void
  onReorderGroups: (ids: number[]) => void
  /** Group management: catalog groups can be renamed and reordered, custom ones deleted too. */
  onNewGroup: () => void
  onEditGroup: (group: SettingsGroup) => void
  onDeleteGroup: (group: SettingsGroup) => void
}) {
  const [search, setSearch] = useState('')
  const [group, setGroup] = useState('')
  const [type, setType] = useState('')
  const [drag, setDrag] = useState<Drag>(null)
  const [over, setOver] = useState<string | null>(null)

  // Full (unfiltered) order per group: drops are computed against it.
  const keysByGroup = useMemo(() => {
    const map = new Map<string, SettingsItem[]>()
    for (const item of [...items].sort((a, b) => a.sort_order - b.sort_order)) {
      const list = map.get(item.group.slug) ?? []
      list.push(item)
      map.set(item.group.slug, list)
    }
    return map
  }, [items])

  const orderedGroups = useMemo(
    () => [...groups].sort((a, b) => a.sort_order - b.sort_order || a.name.localeCompare(b.name)),
    [groups],
  )
  const ungroupedGroup = orderedGroups.find((item) => item.slug === UNGROUPED)
  const ungrouped = keysByGroup.get(UNGROUPED) ?? []
  const filtering = search.trim() !== '' || group !== '' || type !== ''

  const visible = (item: SettingsItem) => {
    const needle = search.trim().toLowerCase()
    if (type && item.type !== type) return false
    return !needle || `${item.key} ${humanize(item.key)} ${item.description}`.toLowerCase().includes(needle)
  }
  const sections = orderedGroups
    .filter((item) => item.slug !== UNGROUPED && (!group || item.slug === group))
    .map((item) => ({ group: item, items: (keysByGroup.get(item.slug) ?? []).filter(visible) }))
    .filter((section) => !filtering || section.items.length > 0)

  const dropSetting = (targetGroup: SettingsGroup, beforeKey?: string) => {
    if (drag?.type !== 'setting' || targetGroup.id <= 0) return
    const keys = (keysByGroup.get(targetGroup.slug) ?? []).map((item) => item.key).filter((key) => key !== drag.key)
    const index = beforeKey ? keys.indexOf(beforeKey) : -1
    keys.splice(index >= 0 ? index : keys.length, 0, drag.key)
    onPlace(targetGroup.id, keys)
  }

  const dropGroup = (targetId: number) => {
    if (drag?.type !== 'group' || drag.id === targetId) return
    const ids = orderedGroups.filter((item) => item.id > 0).map((item) => item.id).filter((id) => id !== drag.id)
    ids.splice(ids.indexOf(targetId), 0, drag.id)
    onReorderGroups(ids)
  }

  const zone = (id: string, onDrop: () => void, accepts: 'setting' | 'group' | 'any' = 'setting') => ({
    onDragOver: (event: DragEvent) => {
      if (!drag || (accepts !== 'any' && drag.type !== accepts)) return
      event.preventDefault()
      event.stopPropagation()
      if (over !== id) setOver(id)
    },
    onDragLeave: () => setOver((current) => (current === id ? null : current)),
    onDrop: (event: DragEvent) => {
      event.preventDefault()
      event.stopPropagation()
      onDrop()
      setDrag(null)
      setOver(null)
    },
  })

  const startSetting = (key: string) => (event: DragEvent) => {
    event.dataTransfer.effectAllowed = 'move'
    event.dataTransfer.setData('text/plain', key)
    const row = (event.currentTarget as HTMLElement).closest('.dnd-row') as HTMLElement | null
    if (row) event.dataTransfer.setDragImage(row, 20, 20)
    setDrag({ type: 'setting', key })
  }
  const end = () => {
    setDrag(null)
    setOver(null)
  }

  return (
    <div className="settings-dnd mt-24">
      <aside className="settings-rail">
        <Card tight>
          <div
            className={`rail-drop ${over === 'ungrouped' ? 'over' : ''}`}
            {...zone('ungrouped', () => ungroupedGroup && dropSetting(ungroupedGroup))}
          >
            <div className="rail-title">
              Ungrouped <span className="count-pill">{ungrouped.length}</span>
            </div>
            {ungrouped.length === 0 ? (
              <div className="small dim">Drag a setting here to park it outside a group.</div>
            ) : (
              ungrouped.map((item) => (
                <div key={item.key} className="rail-item dnd-row" draggable onDragStart={startSetting(item.key)} onDragEnd={end}>
                  <Icon name="grip" />
                  <span className="min-0">
                    <span className="block strong">{humanize(item.key)}</span>
                    <span className="mono small dim">{item.key}</span>
                  </span>
                </div>
              ))
            )}
          </div>
        </Card>
        <Card tight className="mt-16">
          <div className="rail-title">
            Groups
            <span className="spacer" />
            <Button size="sm" variant="ghost" icon="plus" onClick={onNewGroup}>
              New group
            </Button>
          </div>
          <div className="small dim mb-8">
            Drag to reorder. Drop a setting on a group to move it there. Catalog groups come from code and can be
            renamed; groups you add can also be deleted.
          </div>
          {orderedGroups
            .filter((item) => item.slug !== UNGROUPED)
            .map((item) => (
              <div
                key={item.slug}
                className={`rail-group ${over === `rail:${item.slug}` ? 'over' : ''} ${item.id <= 0 ? 'unsynced' : ''}`}
                draggable={item.id > 0}
                onDragStart={(event) => {
                  event.dataTransfer.effectAllowed = 'move'
                  event.dataTransfer.setData('text/plain', item.slug)
                  setDrag({ type: 'group', id: item.id })
                }}
                onDragEnd={end}
                {...zone(`rail:${item.slug}`, () => (drag?.type === 'group' ? dropGroup(item.id) : dropSetting(item)), 'any')}
              >
                <Icon name="grip" />
                <span className="fill">{item.name}</span>
                {item.id > 0 ? (
                  <span className="rail-actions">
                    <button
                      type="button"
                      className="icon-btn ghost"
                      title={`Rename ${item.name}`}
                      aria-label={`Rename ${item.name}`}
                      onClick={() => onEditGroup(item)}
                    >
                      <Icon name="pencil" />
                    </button>
                    {!item.is_system ? (
                      <button
                        type="button"
                        className="icon-btn ghost"
                        title={`Delete ${item.name}`}
                        aria-label={`Delete ${item.name}`}
                        onClick={() => onDeleteGroup(item)}
                      >
                        <Icon name="trash" />
                      </button>
                    ) : null}
                  </span>
                ) : null}
                <span className="count-pill">{keysByGroup.get(item.slug)?.length ?? 0}</span>
              </div>
            ))}
        </Card>
      </aside>

      <div className="min-0">
        <div className="filters">
          <label className="field grow">
            <span className="field-label">Search</span>
            <input
              className="input"
              placeholder="Key, label or description"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
            />
          </label>
          <label className="field minw-field">
            <span className="field-label">Group</span>
            <Select
              value={group}
              onChange={setGroup}
              options={[
                { value: '', label: 'All groups' },
                ...orderedGroups.filter((item) => item.slug !== UNGROUPED).map((item) => ({ value: item.slug, label: item.name })),
              ]}
            />
          </label>
          <label className="field minw-field">
            <span className="field-label">Type</span>
            <Select
              value={type}
              onChange={setType}
              options={[
                { value: '', label: 'All types' },
                { value: 'string', label: 'String' },
                { value: 'int', label: 'Integer' },
                { value: 'bool', label: 'Boolean' },
                { value: 'json', label: 'JSON' },
              ]}
            />
          </label>
        </div>
        {sections.length === 0 ? (
          <Card>
            <EmptyState title="No settings match" description="Try another search or filter." icon="settings" />
          </Card>
        ) : (
          <div className="stack">
            {sections.map((section) => (
              <Card key={section.group.slug} tight>
                <div
                  className={`setting-group-drop ${over === `group:${section.group.slug}` ? 'over' : ''}`}
                  {...zone(`group:${section.group.slug}`, () => dropSetting(section.group))}
                >
                  <div className="setting-group-head">
                    <b>{section.group.name}</b>
                    <span className="count-pill">{section.items.length}</span>
                    {section.group.description ? <span className="small dim">{section.group.description}</span> : null}
                  </div>
                  {section.items.length === 0 ? (
                    <div className="drop-hint">Drag settings here</div>
                  ) : null}
                  {section.items.map((item) => (
                    <div
                      key={item.key}
                      className={`dnd-row ${over === `row:${item.key}` ? 'over' : ''} ${
                        drag?.type === 'setting' && drag.key === item.key ? 'dragging' : ''
                      }`}
                      {...zone(`row:${item.key}`, () => dropSetting(section.group, item.key))}
                    >
                      <span
                        className="drag-grip"
                        draggable
                        title="Drag to reorder or move to another group"
                        onDragStart={startSetting(item.key)}
                        onDragEnd={end}
                      >
                        <Icon name="grip" />
                      </span>
                      <SettingRow
                        item={item}
                        target={target}
                        saving={savingKey === item.key}
                        onSave={(value) => onSave(item, value)}
                        onReset={() => onReset(item)}
                        onDetails={() => onDetails(item)}
                      />
                    </div>
                  ))}
                </div>
              </Card>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

function SettingRow({
  item,
  target,
  saving,
  onSave,
  onReset,
  onDetails,
}: {
  item: SettingsItem
  target: string
  saving: boolean
  onSave: (value: unknown) => void
  onReset: () => void
  onDetails: () => void
}) {
  const unit = unitFor(item.key)
  const [draft, setDraft] = useState(item.value === null || item.value === undefined ? '' : String(item.value))
  const [error, setError] = useState<string | null>(null)
  // Enter saves, and the blur that follows must not save the same value again.
  const sent = useRef<string | null>(null)
  useEffect(() => {
    sent.current = null
    setDraft(item.value === null || item.value === undefined ? '' : String(item.value))
    setError(null)
  }, [item.value])

  const commit = () => {
    const current = item.value === null || item.value === undefined ? '' : String(item.value)
    if (draft === current || draft === sent.current) return
    if (item.type === 'int') {
      if (!/^-?\d+$/.test(draft.trim())) {
        setError('Enter a whole number')
        return
      }
      sent.current = draft
      onSave(Number.parseInt(draft, 10))
      return
    }
    sent.current = draft
    onSave(draft)
  }

  return (
    <div className={`setting-row ${item.editable ? '' : 'readonly'}`}>
      <div className="setting-info">
        <div className="flex wrap g-2">
          <b>{humanize(item.key)}</b>
          <span className="mono small dim">{item.key}</span>
          {item.overridden ? <Badge tone={sourceTone(item.source)}>set at {target}</Badge> : null}
          {!item.overridden && item.source !== 'default' ? <Badge tone={sourceTone(item.source)}>from {item.source}</Badge> : null}
        </div>
        <div className="small dim">{item.description}</div>
        <div className="flex wrap mt-8 g-1">
          <span className="tag">{typeLabel[item.type]}</span>
          {unit ? <span className="tag">{unit}</span> : null}
          {item.scope === 'global' ? <span className="tag">global only</span> : null}
        </div>
      </div>
      <div className="setting-control">
        <div className="flex g-2">
          {item.type === 'bool' ? (
            <button
              type="button"
              role="switch"
              aria-checked={item.value === true}
              aria-label={item.key}
              className={`switch ${item.value === true ? 'on' : ''}`}
              disabled={!item.editable || saving}
              onClick={() => onSave(!(item.value === true))}
            />
          ) : item.type === 'json' ? (
            <button type="button" className="json-preview" onClick={onDetails} disabled={!item.editable}>
              <span className="mono small">{show(item.value)}</span>
              <Icon name="pencil" />
            </button>
          ) : (
            <input
              className={`input ${error ? 'invalid' : ''}`}
              inputMode={item.type === 'int' ? 'numeric' : undefined}
              value={draft}
              disabled={!item.editable || saving}
              aria-label={item.key}
              onChange={(event) => {
                setDraft(event.target.value)
                setError(null)
              }}
              onBlur={commit}
              onKeyDown={(event) => {
                if (event.key === 'Enter') commit()
                if (event.key === 'Escape') setDraft(item.value === null ? '' : String(item.value))
              }}
            />
          )}
          {unit && item.type !== 'bool' ? <span className="small dim">{unit}</span> : null}
          {saving ? <span className="spinner" /> : null}
        </div>
        {error ? <div className="small danger-text">{error}</div> : null}
        <div className="setting-meta">
          <span>
            Default: <span className="mono">{show(item.default)}</span>
          </span>
          {item.overridden ? (
            <Button size="sm" variant="ghost" onClick={onReset} disabled={saving}>
              Reset
            </Button>
          ) : null}
          <Button size="sm" variant="ghost" onClick={onDetails}>
            Details
          </Button>
        </div>
      </div>
    </div>
  )
}
