import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useState } from 'react'
import { api, type Schemas } from '../api/client'
import { can, useSession } from '../auth/session'
import { useT } from '../i18n'
import { useDismiss } from './inputs'
import { useToast } from './Toasts'
import { Icon } from './ui'

type Branch = Schemas['Branch']

export const branchesQuery = {
  queryKey: ['branches', 'all'],
  queryFn: () => api<{ data: Branch[] }>('/branch', { envelope: true, query: { per_page: 100, order_by: 'name', order_direction: 'asc' } }).then((page) => page.data),
}

/** The top bar's branch picker: shown to members who may switch and have somewhere to go. */
export function BranchSwitcher() {
  const t = useT()
  const session = useSession()
  const client = useQueryClient()
  const { push } = useToast()
  const [open, setOpen] = useState(false)
  const close = useCallback(() => setOpen(false), [])
  const ref = useDismiss(open, close)
  const allowed = !!session && !session.isPlatformUser && can(session, 'branch.view.all') && can(session, 'user.branch.switch')
  const branches = useQuery({ ...branchesQuery, enabled: allowed })
  if (!allowed || !branches.data || branches.data.length < 2) return null

  const currentId = session!.user.active_branch_id ?? session!.user.branch_id
  const current = branches.data.find((branch) => branch.id === currentId)

  const choose = async (branch: Branch) => {
    setOpen(false)
    if (branch.id === currentId) return
    try {
      await api(`/user/switch-branch/${branch.id}`, { method: 'PATCH' })
      // Everything on screen may belong to the old branch.
      await client.invalidateQueries()
      push(t('branches.switched', { name: branch.name }), 'success')
    } catch (cause) {
      push(cause instanceof Error ? cause.message : String(cause), 'error')
    }
  }

  return (
    <div className="appearance" ref={ref}>
      <button type="button" className="branch-trigger" title={t('branches.switch')} aria-expanded={open} onClick={() => setOpen((value) => !value)}>
        <Icon name="building" />
        <span className="branch-trigger-name">{current?.name ?? t('branches.switch')}</span>
      </button>
      {open ? (
        <div className="ui-popover align-right branch-menu" role="menu" aria-label={t('branches.switch')}>
          {branches.data.map((branch) => (
            <button key={branch.id} type="button" role="menuitemradio" aria-checked={branch.id === currentId} className="branch-option" onClick={() => void choose(branch)}>
              <span>{branch.name}</span>
              {branch.id === currentId ? <Icon name="check" /> : null}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  )
}
