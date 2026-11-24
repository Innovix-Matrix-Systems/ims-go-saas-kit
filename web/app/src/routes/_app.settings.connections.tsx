import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api, type Schemas } from '../api/client'
import { relativeTimeIn } from '../format'
import { allTenantsQuery } from '../components/console'
import { ListSkeleton } from '../components/Skeleton'
import { useToast } from '../components/Toasts'
import { Badge, Button, Card, ConfirmDialog, CopyField, EmptyState, Icon } from '../components/ui'
import { intlLocale, useLocale, useT, type MessageKey } from '../i18n'
import { useSession } from '../auth/session'
import { organizationQuery } from '../components/organization'

export const Route = createFileRoute('/_app/settings/connections')({
  component: ConnectionsPage,
})

type Connection = Schemas['MCPConnection']

const scopeLabels: Record<string, MessageKey> = {
  'mcp:read': 'connections.scope.read',
  'mcp:write': 'connections.scope.write',
}

/**
 * Whether the server accepts AI agents (MCP), and the address to give them:
 * the standard protected-resource document answers only when MCP is on.
 */
const mcpQuery = {
  queryKey: ['mcp', 'resource'],
  queryFn: async (): Promise<string | null> => {
    try {
      const response = await fetch('/.well-known/oauth-protected-resource/mcp', { headers: { Accept: 'application/json' } })
      if (!response.ok) return null
      const body = (await response.json()) as { resource?: string }
      return body.resource ?? null
    } catch {
      return null
    }
  },
  staleTime: 5 * 60_000,
  retry: false,
}

/** The AI agents (MCP clients) that act as the signed-in person, and the way to cut them off. */
function ConnectionsPage() {
  const t = useT()
  const locale = intlLocale(useLocale())
  const session = useSession()
  const client = useQueryClient()
  const { push } = useToast()
  const [revoking, setRevoking] = useState<Connection | null>(null)
  const [busy, setBusy] = useState(false)
  const mcp = useQuery(mcpQuery)
  const connections = useQuery({ queryKey: ['connections'], queryFn: () => api<Connection[]>('/user/connections') })
  const organization = useQuery({ ...organizationQuery, enabled: !!session && !session.isPlatformUser })
  const tenants = useQuery({ ...allTenantsQuery, enabled: !!session?.isPlatformUser && !session.acting })
  const date = (value: string) => new Intl.DateTimeFormat(locale, { dateStyle: 'medium' }).format(new Date(value))

  const orgName = (id?: number | null) => {
    if (!id) return t('connections.platform')
    if (organization.data?.id === id) return organization.data.name
    return tenants.data?.find((tenant) => tenant.id === id)?.name ?? `#${id}`
  }

  const revoke = async () => {
    if (!revoking) return
    setBusy(true)
    try {
      await api(`/user/connections/${revoking.id}`, { method: 'DELETE' })
      await client.invalidateQueries({ queryKey: ['connections'] })
      push(t('connections.revoked', { name: revoking.client_name }), 'success')
    } catch (cause) {
      push(cause instanceof Error ? cause.message : String(cause), 'error')
    } finally {
      setBusy(false)
      setRevoking(null)
    }
  }

  const items = connections.data ?? []

  return (
    <div className="stack g-4">
      <Card title={t('connections.title')} description={t('connections.subtitle')} flush>
        {connections.isLoading ? (
          <ListSkeleton rows={2} avatar />
        ) : items.length === 0 ? (
          <EmptyState icon="actions" title={t('connections.none')} description={mcp.data ? t('connections.noneHint') : undefined} />
        ) : (
          <ul className="connection-list">
            {items.map((item) => {
              const writes = item.scopes.includes('mcp:write')
              return (
                <li key={item.id} className="connection">
                  <span className="connection-icon" aria-hidden="true">
                    <Icon name="actions" />
                  </span>
                  <span className="connection-meta">
                    <span className="connection-name">
                      {item.client_name}
                      {writes ? <Badge tone="warn">{t('connections.canChange')}</Badge> : <Badge tone="neutral">{t('connections.readOnly')}</Badge>}
                    </span>
                    <span className="small muted">
                      {item.scopes.map((scope) => (scopeLabels[scope] ? t(scopeLabels[scope]) : scope)).join(' · ')}
                    </span>
                    <span className="small muted">
                      {t('connections.actsIn', { org: orgName(item.tenant_id) })} · {t('connections.connected', { date: date(item.created_at) })} ·{' '}
                      {item.last_used_at ? t('connections.lastUsed', { when: relativeTimeIn(locale, item.last_used_at) }) : t('connections.neverUsed')}
                    </span>
                  </span>
                  <Button size="sm" variant="danger" icon="x" onClick={() => setRevoking(item)}>
                    {t('connections.disconnect')}
                  </Button>
                </li>
              )
            })}
          </ul>
        )}
      </Card>

      <Card title={t('connections.howTitle')}>
        {mcp.isLoading ? null : mcp.data ? (
          <div className="stack">
            <p className="muted m-0">{t('connections.howBody')}</p>
            <div className="details">
              <span className="muted">{t('connections.address')}</span>
              <CopyField value={mcp.data} />
            </div>
            <ol className="connection-steps">
              <li>{t('connections.step1')}</li>
              <li>{t('connections.step2')}</li>
              <li>{t('connections.step3')}</li>
            </ol>
          </div>
        ) : (
          <div className="banner">
            <span>{t('connections.off')}</span>
          </div>
        )}
      </Card>

      <ConfirmDialog
        open={revoking != null}
        title={t('connections.disconnect')}
        description={revoking ? t('connections.disconnectConfirm', { name: revoking.client_name }) : ''}
        confirmLabel={t('connections.disconnect')}
        danger
        busy={busy}
        onConfirm={() => void revoke()}
        onCancel={() => setRevoking(null)}
      />
    </div>
  )
}
