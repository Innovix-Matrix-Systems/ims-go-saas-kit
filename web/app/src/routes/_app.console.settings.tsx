import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { can, useSession } from '../auth/session'
import { useToast } from '../components/Toasts'
import { Badge, Card, PageHeader } from '../components/ui'
import { useT } from '../i18n'
import { SettingsSkeleton } from '../components/Skeleton'
import { SettingRow, useGroupName, type Setting } from '../components/SettingField'

export const Route = createFileRoute('/_app/console/settings')({
  component: PlatformSettingsPage,
})


function PlatformSettingsPage() {
  const t = useT()
  const groupName = useGroupName()
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
        <Card>
          <SettingsSkeleton rows={6} />
        </Card>
      ) : (
        <div className="stack g-4">
          {ordered.map(([slug, group]) => (
            <Card key={slug} title={groupName(slug, group.name)}>
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

function PlatformSetting({ item }: { item: Setting }) {
  const t = useT()
  const session = useSession()
  const client = useQueryClient()
  const { push } = useToast()
  const save = async (value: unknown) => {
    try {
      await api(`/internal/settings/${encodeURIComponent(item.key)}`, { method: 'PATCH', asStaff: true, body: { value, target_type: 'global' } })
      await client.invalidateQueries({ queryKey: ['console', 'settings'] })
      await client.invalidateQueries({ queryKey: ['settings'] })
      push(t('form.saved'), 'success')
    } catch (cause) {
      push(cause instanceof Error ? cause.message : String(cause), 'error')
      throw cause
    }
  }
  return (
    <SettingRow
      item={item}
      save={save}
      editable={item.editable && can(session, 'setting.internal.update')}
      badge={item.scope === 'tenant' ? <Badge tone="info">{t('console.overridable')}</Badge> : undefined}
    />
  )
}
