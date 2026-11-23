import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { notificationPath } from '../components/notifications'
import { api, type Schemas } from '../api/client'
import { useToast } from '../components/Toasts'
import { Badge, Card, Spinner } from '../components/ui'
import { useT, type MessageKey } from '../i18n'

export const Route = createFileRoute('/_app/settings/notifications')({
  component: NotificationsPage,
})

type Preference = Schemas['NotificationPreference']

const channelLabels: Record<Preference['channel'], MessageKey> = {
  mail: 'notifications.channel.mail',
  sms: 'notifications.channel.sms',
  push: 'notifications.channel.push',
}

function NotificationsPage() {
  const t = useT()
  const { push } = useToast()
  const queryClient = useQueryClient()
  const prefs = useQuery({ queryKey: ['notification-preferences'], queryFn: () => api<Preference[]>(notificationPath('/notification/preferences')) })
  const [saving, setSaving] = useState<string | null>(null)

  const toggle = async (pref: Preference) => {
    const key = `${pref.event_type}:${pref.channel}`
    setSaving(key)
    try {
      await api(notificationPath('/notification/preferences'), {
        method: 'PUT',
        body: { preferences: [{ event_type: pref.event_type, channel: pref.channel, enabled: !pref.enabled }] },
      })
      await queryClient.invalidateQueries({ queryKey: ['notification-preferences'] })
    } catch (cause) {
      push(cause instanceof Error ? cause.message : String(cause), 'error')
    } finally {
      setSaving(null)
    }
  }

  // One row per event, one switch per channel.
  const events = new Map<string, Preference[]>()
  for (const pref of prefs.data ?? []) {
    events.set(pref.event_type, [...(events.get(pref.event_type) ?? []), pref])
  }

  return (
    <Card title={t('notifications.preferences')} flush>
      {!prefs.data ? (
        <div className="p-4">
          <Spinner />
        </div>
      ) : (
        <ul className="pref-list">
          {[...events.entries()].map(([event, items]) => (
            <li key={event} className="pref-row">
              <div className="pref-meta">
                <div className="pref-name">{items[0].description || event}</div>
              </div>
              <div className="pref-channels">
                {items.map((pref) =>
                  pref.mandatory ? (
                    <Badge key={pref.channel} tone="neutral" title={t('notifications.mandatory')}>
                      {t(channelLabels[pref.channel])} · {t('notifications.mandatory')}
                    </Badge>
                  ) : (
                    <label key={pref.channel} className="check-row">
                      <input
                        type="checkbox"
                        checked={pref.enabled}
                        disabled={saving === `${pref.event_type}:${pref.channel}`}
                        onChange={() => void toggle(pref)}
                      />
                      {t(channelLabels[pref.channel])}
                    </label>
                  ),
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
    </Card>
  )
}
