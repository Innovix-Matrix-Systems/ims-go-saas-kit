import { Link, useNavigate } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useCallback, useState } from 'react'
import { can, signOut, useSession, type Session } from '../auth/session'
import { useT, type MessageKey } from '../i18n'
import { subscriptionStatusQuery } from './billing'
import { useStopActing } from './console'
import { useDismiss } from './inputs'
import { organizationQuery } from './organization'
import { Avatar, Icon, type IconName } from './ui'

/** Whether the session works inside one organization (members, and staff acting in one). */
export function inOrganization(session: Session | undefined): boolean {
  return !!session && (!session.isPlatformUser || !!session.acting)
}

/** The current organization's name, or null outside one. */
export function useOrganizationName(): string | null {
  const session = useSession()
  const organization = useQuery({ ...organizationQuery, enabled: inOrganization(session) })
  return inOrganization(session) ? (organization.data?.name ?? null) : null
}

/** The organization's mark: its initials on the accent, until it has a logo. */
export function OrgMark({ name, size }: { name: string; size?: 'sm' | 'lg' | 'xl' }) {
  const letters = name
    .trim()
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((word) => word[0]!.toUpperCase())
    .join('')
  return (
    <span className={`org-mark ${size ?? ''}`} aria-hidden="true">
      {letters || '·'}
    </span>
  )
}

interface MenuLink {
  to: string
  icon: IconName
  label: MessageKey
  show: boolean
}

/**
 * The sidebar's header: which organization this is (its mark, name and
 * plan), opening to its profile, settings and billing. Staff outside an
 * organization see the platform console.
 */
export function WorkspaceMenu({ collapsed }: { collapsed: boolean }) {
  const t = useT()
  const session = useSession()
  const [open, setOpen] = useState(false)
  const close = useCallback(() => setOpen(false), [])
  const ref = useDismiss(open, close)
  const inside = inOrganization(session)
  const organization = useQuery({ ...organizationQuery, enabled: inside })
  const billing = inside && can(session, 'billing.view')
  const subscription = useQuery({ ...subscriptionStatusQuery, enabled: billing })
  const stopActing = useStopActing()

  const name = inside ? (organization.data?.name ?? '…') : t('app.name')
  const sub = !inside
    ? t('workspace.console')
    : session?.acting
      ? t('workspace.impersonating')
      : billing && subscription.data?.package
        ? t('workspace.plan', { plan: subscription.data.package.name })
        : (session?.user.roles?.map((role) => role.name).join(', ') ?? t('role.member'))

  const links: MenuLink[] = inside
    ? [
        { to: '/org', icon: 'building', label: 'workspace.profile', show: true },
        { to: '/org/members', icon: 'users', label: 'org.members', show: can(session, 'user.view.all') },
        { to: '/org/billing', icon: 'package', label: 'org.billing', show: billing },
        { to: '/org/settings', icon: 'settings', label: 'nav.orgSettings', show: true },
      ]
    : [
        { to: '/console/organizations', icon: 'tenants', label: 'console.organizations', show: can(session, 'tenant.view.all') },
        { to: '/console/settings', icon: 'settings', label: 'console.settings', show: can(session, 'setting.internal.view') },
      ]

  return (
    <div className="workspace" ref={ref}>
      <button
        type="button"
        className="workspace-trigger"
        aria-expanded={open}
        aria-haspopup="menu"
        title={collapsed ? name : undefined}
        onClick={() => setOpen((value) => !value)}
      >
        {inside ? <OrgMark name={name} /> : <span className="brand-mark">I</span>}
        <span className="workspace-text">
          <span className="workspace-name">{name}</span>
          <span className="workspace-sub">{sub}</span>
        </span>
        <Icon name="chevron" className="workspace-caret" />
      </button>
      {open ? (
        <div className="ui-popover workspace-menu" role="menu" aria-label={name}>
          <div className="workspace-menu-head">
            {inside ? <OrgMark name={name} size="lg" /> : <span className="brand-mark">I</span>}
            <span className="workspace-text">
              <span className="workspace-name">{name}</span>
              <span className="workspace-sub">{inside && organization.data ? organization.data.code : sub}</span>
            </span>
          </div>
          <div className="menu-sep" />
          {links
            .filter((link) => link.show)
            .map((link) => (
              <Link key={link.to} to={link.to} role="menuitem" className="menu-item" onClick={close}>
                <Icon name={link.icon} />
                {t(link.label)}
              </Link>
            ))}
          {session?.acting ? (
            <>
              <div className="menu-sep" />
              <button
                type="button"
                role="menuitem"
                className="menu-item"
                onClick={() => {
                  close()
                  void stopActing()
                }}
              >
                <Icon name="logout" />
                {t('workspace.stopActing')}
              </button>
            </>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}

/** The sidebar's footer: who is signed in, opening to their profile, settings and sign-out. */
export function AccountMenu() {
  const t = useT()
  const navigate = useNavigate()
  const session = useSession()
  const [open, setOpen] = useState(false)
  const close = useCallback(() => setOpen(false), [])
  const ref = useDismiss(open, close)
  const user = session?.user
  const role = session?.isPlatformUser ? t('role.superAdmin') : user?.roles?.map((item) => item.name).join(', ') || t('role.member')

  const items: MenuLink[] = [
    { to: '/profile', icon: 'user', label: 'account.profile', show: true },
    { to: '/settings/profile', icon: 'settings', label: 'account.settings', show: true },
    { to: '/settings/security', icon: 'shield', label: 'settings.security', show: true },
    { to: '/notifications', icon: 'bell', label: 'notifications.title', show: true },
  ]

  return (
    <div className="account" ref={ref}>
      <button type="button" className="user-card" aria-expanded={open} aria-haspopup="menu" onClick={() => setOpen((value) => !value)} title={user?.name}>
        <Avatar name={user?.name ?? '…'} photo={user?.photo} />
        <span className="user-meta">
          <span className="user-name">{user?.name ?? '…'}</span>
          <span className="user-email">{user ? role : t('common.loading')}</span>
        </span>
        <Icon name="chevron" className="workspace-caret up" />
      </button>
      {open && user ? (
        <div className="ui-popover align-up account-menu" role="menu" aria-label={user.name}>
          <div className="workspace-menu-head">
            <Avatar name={user.name} photo={user.photo} size="sm" />
            <span className="workspace-text">
              <span className="workspace-name">{user.name}</span>
              <span className="workspace-sub">{user.email}</span>
            </span>
          </div>
          <div className="menu-sep" />
          {items.map((item) => (
            <Link key={item.to} to={item.to} role="menuitem" className="menu-item" onClick={close}>
              <Icon name={item.icon} />
              {t(item.label)}
            </Link>
          ))}
          <div className="menu-sep" />
          <button
            type="button"
            role="menuitem"
            className="menu-item"
            onClick={async () => {
              close()
              await signOut()
              await navigate({ to: '/login' })
            }}
          >
            <Icon name="logout" />
            {t('common.signOut')}
          </button>
        </div>
      ) : null}
    </div>
  )
}
