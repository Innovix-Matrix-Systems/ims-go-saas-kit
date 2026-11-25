import type { ReactNode } from 'react'
import { LanguageMenu } from './LanguageMenu'
import { Icon, type IconName } from './ui'

/** A centred, full-page message: suspended, maintenance, errors, not found. */
export function StatusScreen({
  icon,
  tone = 'accent',
  title,
  body,
  reference,
  actions,
}: {
  icon: IconName
  tone?: 'accent' | 'warn' | 'danger'
  title: string
  body: ReactNode
  reference?: string
  actions?: ReactNode
}) {
  return (
    <div className="login-wrap">
      <div className="login-top">
        <LanguageMenu />
      </div>
      <div className="login-card">
        <div className="card">
          <div className={`status-icon ${tone === 'accent' ? '' : tone}`}>
            <Icon name={icon} />
          </div>
          <h1 className="login-title">{title}</h1>
          <p className="login-sub">{body}</p>
          {actions ? <div className="modal-actions">{actions}</div> : null}
          {reference ? <div className="status-ref">{reference}</div> : null}
        </div>
      </div>
    </div>
  )
}
