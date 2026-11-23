import { createFileRoute } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { api } from '../api/client'
import { allTenantsQuery } from '../components/console'
import { Select } from '../components/inputs'
import { useToast } from '../components/Toasts'
import { Badge, Button, Card, PageHeader, Spinner } from '../components/ui'
import { useT, type MessageKey } from '../i18n'

export const Route = createFileRoute('/_app/console/flags')({
  component: FlagsPage,
})

interface Flag {
  key: string
  value: boolean
  rollout: number | null
  default: boolean
  source: 'global' | 'tenant' | 'user' | 'default'
  description: string
}

function FlagsPage() {
  const t = useT()
  const tenants = useQuery(allTenantsQuery)
  // '' is everyone (the global value); otherwise an organization id.
  const [target, setTarget] = useState('')
  const query = target ? { target_type: 'tenant', target_id: target } : { target_type: 'global' }
  const flags = useQuery({
    queryKey: ['console', 'flags', target],
    queryFn: () => api<Flag[]>('/internal/feature-flags', { asStaff: true, query }),
  })

  return (
    <>
      <PageHeader title={t('console.flags')} description={t('console.flagsSubtitle')} />
      <div className="toolbar">
        <span className="field-label">{t('console.flagTarget')}</span>
        <Select
          className="toolbar-select"
          value={target}
          ariaLabel={t('console.flagTarget')}
          onChange={setTarget}
          options={[{ value: '', label: t('console.everyone') }, ...(tenants.data ?? []).map((tenant) => ({ value: String(tenant.id), label: tenant.name }))]}
        />
      </div>
      <Card flush>
        {!flags.data ? (
          <div className="p-4">
            <Spinner />
          </div>
        ) : (
          <div className="flag-list">
            {flags.data.map((flag) => (
              <FlagRow key={`${target}:${flag.key}`} flag={flag} target={target} />
            ))}
          </div>
        )}
      </Card>
    </>
  )
}

function FlagRow({ flag, target }: { flag: Flag; target: string }) {
  const t = useT()
  const client = useQueryClient()
  const { push } = useToast()
  const [rollout, setRollout] = useState(flag.rollout == null ? '' : String(flag.rollout))
  const [busy, setBusy] = useState(false)
  useEffect(() => setRollout(flag.rollout == null ? '' : String(flag.rollout)), [flag.rollout])
  const scope = target ? { target_type: 'tenant', target_id: Number(target) } : { target_type: 'global' }
  // An organization's own value can be reset to the platform's.
  const overridden = target ? flag.source === 'tenant' : flag.source === 'global'

  const run = async (action: () => Promise<unknown>) => {
    setBusy(true)
    try {
      await action()
      await client.invalidateQueries({ queryKey: ['console', 'flags'] })
      push(t('form.saved'), 'success')
    } catch (cause) {
      push(cause instanceof Error ? cause.message : String(cause), 'error')
    } finally {
      setBusy(false)
    }
  }

  const saveRollout = () => {
    const value = rollout.trim()
    if (value === (flag.rollout == null ? '' : String(flag.rollout))) return
    const number = Number(value)
    if (value === '' || !Number.isInteger(number) || number < 0 || number > 100) {
      setRollout(flag.rollout == null ? '' : String(flag.rollout))
      return
    }
    void run(() => api('/internal/feature-flags', { method: 'PATCH', asStaff: true, body: { ...scope, rollout: { [flag.key]: number } } }))
  }

  return (
    <div className="flag-row">
      <div className="setting-meta">
        <div>{flag.description || flag.key}</div>
        <div className="small muted mono flex wrap">
          {flag.key}
          <Badge tone={overridden ? 'info' : 'neutral'}>{t(`console.source.${flag.source}` as MessageKey)}</Badge>
        </div>
      </div>
      {!target ? (
        <label className="flag-rollout" title={t('console.rolloutHint')}>
          <span className="small muted">{t('console.rollout')}</span>
          <input
            className="input"
            type="number"
            min="0"
            max="100"
            placeholder="100"
            disabled={busy}
            value={rollout}
            onChange={(e) => setRollout(e.target.value)}
            onBlur={saveRollout}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                e.currentTarget.blur()
              }
            }}
          />
          <span className="small muted">%</span>
        </label>
      ) : null}
      {overridden && target ? (
        <Button
          size="sm"
          variant="ghost"
          disabled={busy}
          onClick={() =>
            void run(() => api(`/internal/feature-flags/${encodeURIComponent(flag.key)}`, { method: 'DELETE', asStaff: true, query: scope }))
          }
        >
          {t('console.reset')}
        </Button>
      ) : null}
      <button
        type="button"
        role="switch"
        aria-checked={flag.value}
        aria-label={flag.description || flag.key}
        className={`switch ${flag.value ? 'on' : ''}`}
        disabled={busy}
        onClick={() => void run(() => api('/internal/feature-flags', { method: 'PATCH', asStaff: true, body: { ...scope, flags: { [flag.key]: !flag.value } } }))}
      />
    </div>
  )
}
