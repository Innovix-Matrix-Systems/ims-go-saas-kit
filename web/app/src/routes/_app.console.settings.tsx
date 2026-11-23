import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { api, type Schemas } from '../api/client'
import { can, useSession } from '../auth/session'
import { useToast } from '../components/Toasts'
import { Badge, Card, PageHeader, Spinner } from '../components/ui'
import { useT } from '../i18n'

export const Route = createFileRoute('/_app/console/settings')({
  component: PlatformSettingsPage,
})

type Setting = Schemas['SettingItem']

// Long free text gets a text area (bank details span several lines).
const multiline = new Set(['billing.payment_instructions'])

function PlatformSettingsPage() {
  const t = useT()
  const settings = useQuery({ queryKey: ['console', 'settings'], queryFn: () => api<Setting[]>('/internal/settings', { asStaff: true }) })

  const groups = new Map<string, { name: string; order: number; items: Setting[] }>()
  for (const item of settings.data ?? []) {
    const slug = item.group?.slug ?? 'ungrouped'
    const group = groups.get(slug) ?? { name: item.group?.name ?? slug, order: item.group?.sort_order ?? 0, items: [] }
    group.items.push(item)
    groups.set(slug, group)
  }
  const ordered = [...groups.entries()].sort((a, b) => a[1].order - b[1].order)

  return (
    <>
      <PageHeader title={t('console.settings')} description={t('console.settingsSubtitle')} />
      {!settings.data ? (
        <Spinner />
      ) : (
        <div className="stack g-4">
          {ordered.map(([slug, group]) => (
            <Card key={slug} title={group.name}>
              <div>
                {group.items
                  .sort((a, b) => a.sort_order - b.sort_order)
                  .map((item) => (
                    <PlatformSetting key={item.key} item={item} />
                  ))}
              </div>
            </Card>
          ))}
        </div>
      )}
    </>
  )
}

function display(value: unknown): string {
  if (value == null) return ''
  return typeof value === 'string' ? value : JSON.stringify(value)
}

function PlatformSetting({ item }: { item: Setting }) {
  const t = useT()
  const session = useSession()
  const client = useQueryClient()
  const { push } = useToast()
  const [draft, setDraft] = useState(display(item.value))
  const [busy, setBusy] = useState(false)
  useEffect(() => setDraft(display(item.value)), [item.value])
  const editable = item.editable && can(session, 'setting.internal.update')

  const save = async (value: unknown) => {
    setBusy(true)
    try {
      await api(`/internal/settings/${encodeURIComponent(item.key)}`, { method: 'PATCH', asStaff: true, body: { value, target_type: 'global' } })
      await client.invalidateQueries({ queryKey: ['console', 'settings'] })
      await client.invalidateQueries({ queryKey: ['settings'] })
      push(t('form.saved'), 'success')
    } catch (cause) {
      push(cause instanceof Error ? cause.message : String(cause), 'error')
      setDraft(display(item.value))
    } finally {
      setBusy(false)
    }
  }

  const commit = () => {
    if (draft === display(item.value)) return
    if (item.type === 'int') {
      const number = Number(draft)
      return Number.isInteger(number) ? void save(number) : setDraft(display(item.value))
    }
    if (item.type === 'json') {
      try {
        return void save(JSON.parse(draft))
      } catch {
        return setDraft(display(item.value))
      }
    }
    void save(draft)
  }

  return (
    <div className={`setting-row ${multiline.has(item.key) ? 'wide' : ''}`}>
      <div className="setting-meta">
        <div>{item.description || item.key}</div>
        <div className="small muted mono">
          {item.key} {item.scope === 'tenant' ? <Badge tone="info">{t('console.overridable')}</Badge> : null}
        </div>
      </div>
      <div className="setting-control">
        {item.type === 'bool' ? (
          <button
            type="button"
            role="switch"
            aria-checked={item.value === true}
            aria-label={item.description || item.key}
            className={`switch ${item.value === true ? 'on' : ''}`}
            disabled={busy || !editable}
            onClick={() => void save(item.value !== true)}
          />
        ) : multiline.has(item.key) ? (
          <textarea className="input" rows={5} disabled={busy || !editable} value={draft} onChange={(e) => setDraft(e.target.value)} onBlur={commit} />
        ) : (
          <input
            className="input"
            type={item.type === 'int' ? 'number' : 'text'}
            disabled={busy || !editable}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onBlur={commit}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                e.currentTarget.blur()
              }
            }}
          />
        )}
      </div>
    </div>
  )
}
