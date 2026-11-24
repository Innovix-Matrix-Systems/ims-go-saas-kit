import { useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { changeActing, useSession } from '../auth/session'
import { useT } from '../i18n'
import { Button, Icon } from './ui'

/**
 * Always visible while staff impersonate an organization or are logged in as
 * a user, so nobody forgets whose data they are changing.
 */
export function ActingBanner() {
  const t = useT()
  const session = useSession()
  const navigate = useNavigate()
  const [busy, setBusy] = useState(false)
  const acting = session?.acting
  if (!acting) return null

  const stop = async () => {
    setBusy(true)
    try {
      await changeActing(null)
      await navigate({ to: acting.kind === 'tenant' ? '/console/organizations' : '/console/users' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className={`acting-banner ${acting.kind}`} role="status">
      <Icon name={acting.kind === 'tenant' ? 'tenants' : 'user'} />
      <span className="acting-text">
        {acting.kind === 'tenant' ? t('acting.tenant', { name: acting.name }) : t('acting.user', { name: acting.name, email: acting.email })}
      </span>
      <Button size="sm" variant="primary" disabled={busy} onClick={() => void stop()}>
        {acting.kind === 'tenant' ? t('acting.stop') : t('acting.return')}
      </Button>
    </div>
  )
}
