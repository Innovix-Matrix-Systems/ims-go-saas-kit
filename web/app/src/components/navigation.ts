import { can, type Session } from '../auth/session'
import type { MessageKey } from '../i18n'
import type { IconName } from './ui'

// Every screen of the signed-in app, in one list: the sidebar, the command
// palette, the shortcuts sheet and the top bar title all read it. A new page
// adds itself here with its group, icon, `g` shortcut letter (unique; checked
// below) and the permission it needs, if any.

export type NavGroup = 'workspace' | 'console' | 'organization' | 'account'

export const navGroups: NavGroup[] = ['workspace', 'console', 'organization', 'account']

export const navGroupLabels: Record<NavGroup, MessageKey> = {
  workspace: 'nav.group.workspace',
  console: 'nav.group.console',
  organization: 'nav.group.organization',
  account: 'nav.group.account',
}

export interface Page {
  to: string
  label: MessageKey
  icon: IconName
  group: NavGroup
  /** `g` then this letter opens the page. */
  key?: string
  /** Words the palette also matches. */
  keywords?: string
  /** Shown only when the session holds this permission. */
  permission?: string
  /** Shown only to platform staff (accounts without an organization). */
  platformOnly?: boolean
  /** Shown only to members of an organization. */
  tenantOnly?: boolean
}

export const pages: Page[] = [
  { to: '/', label: 'nav.home', icon: 'home', group: 'workspace', key: 'h', keywords: 'dashboard overview' },
  { to: '/console/organizations', label: 'console.organizations', icon: 'tenants', group: 'console', key: 'c', keywords: 'tenants customers companies impersonate suspend', permission: 'tenant.view.all', platformOnly: true },
  { to: '/console/users', label: 'console.users', icon: 'users', group: 'console', key: 'u', keywords: 'people accounts log in as support', permission: 'user.view.all', platformOnly: true },
  { to: '/console/activity', label: 'console.auditLog', icon: 'activity', group: 'console', key: 'd', keywords: 'audit log activity history system impersonation', permission: 'activity.view.all', platformOnly: true },
  { to: '/console/approvals', label: 'console.approvals', icon: 'check', group: 'console', key: 'v', keywords: 'payments bank transfer review approve reject', permission: 'payment_approval.view.all', platformOnly: true },
  { to: '/console/plans', label: 'console.plans', icon: 'package', group: 'console', key: 'l', keywords: 'packages pricing features limits', permission: 'package.view.all', platformOnly: true },
  { to: '/console/staff', label: 'console.staff', icon: 'shield', group: 'console', key: 'x', keywords: 'internal users team admins', permission: 'user.internal.create', platformOnly: true },
  { to: '/console/announcements', label: 'console.announcements', icon: 'bell', group: 'console', key: 'k', keywords: 'broadcast notify maintenance news message', permission: 'notification.create', platformOnly: true },
  { to: '/console/flags', label: 'console.flags', icon: 'flags', group: 'console', key: 'e', keywords: 'feature toggles rollout', permission: 'feature_flag.internal.update', platformOnly: true },
  { to: '/console/settings', label: 'console.settings', icon: 'settings', group: 'console', key: 'y', keywords: 'platform global bank details payment instructions', permission: 'setting.internal.view', platformOnly: true },
  { to: '/org/members', label: 'org.members', icon: 'users', group: 'organization', key: 'm', keywords: 'users team invite people', permission: 'user.view.all', tenantOnly: true },
  { to: '/org/roles', label: 'org.roles', icon: 'shield', group: 'organization', key: 'r', keywords: 'permissions access', permission: 'role.view.all', tenantOnly: true },
  { to: '/org/branches', label: 'org.branches', icon: 'tenants', group: 'organization', key: 'b', keywords: 'locations offices shops', permission: 'branch.view.all', tenantOnly: true },
  { to: '/org/activity', label: 'org.activity', icon: 'activity', group: 'organization', key: 'a', keywords: 'audit log history changes', permission: 'activity.view', tenantOnly: true },
  { to: '/org/files', label: 'org.files', icon: 'files', group: 'organization', key: 'f', keywords: 'uploads documents images', permission: 'file.view.all', tenantOnly: true },
  { to: '/org/webhooks', label: 'org.webhooks', icon: 'actions', group: 'organization', key: 'w', keywords: 'integrations events api', permission: 'webhook.manage', tenantOnly: true },
  { to: '/org/billing', label: 'org.billing', icon: 'package', group: 'organization', key: 'i', keywords: 'plan subscription payments invoice upgrade', permission: 'billing.view', tenantOnly: true },
  { to: '/org/settings', label: 'nav.orgSettings', icon: 'building', group: 'organization', key: 'o', keywords: 'organization company owner transfer policies', tenantOnly: true },
  { to: '/settings/profile', label: 'settings.profile', icon: 'user', group: 'account', key: 'p', keywords: 'name language e-mail' },
  { to: '/settings/security', label: 'settings.security', icon: 'shield', group: 'account', key: 's', keywords: 'password two-factor 2fa sessions devices' },
  { to: '/notifications', label: 'notifications.title', icon: 'bell', group: 'workspace', key: 'n', keywords: 'inbox alerts' },
  { to: '/jobs', label: 'jobs.title', icon: 'download', group: 'workspace', key: 'j', keywords: 'export import download background files' },
  { to: '/settings/notifications', label: 'settings.notificationSettings', icon: 'settings', group: 'account', keywords: 'notification preferences' },
  { to: '/settings/privacy', label: 'settings.privacy', icon: 'eye', group: 'account', keywords: 'export delete account data' },
]

if (import.meta.env.DEV) {
  const keys = pages.flatMap((page) => (page.key ? [page.key] : []))
  const duplicate = keys.find((key, index) => keys.indexOf(key) !== index)
  if (duplicate) throw new Error(`navigation: shortcut g ${duplicate} is used twice`)
}

/** The pages this session can open. */
export function visiblePages(session: Session | undefined): Page[] {
  return pages.filter((page) => {
    if (page.platformOnly && !session?.isPlatformUser) return false
    // Organization pages: members, and staff while impersonating one.
    if (page.tenantOnly && session?.isPlatformUser !== false && session?.acting?.kind !== 'tenant') return false
    if (page.permission && !can(session, page.permission)) return false
    return true
  })
}

/** The page a path belongs to (detail routes map to their list). */
export function pageFor(pathname: string): Page | undefined {
  return [...pages]
    .filter((page) => (page.to === '/' ? pathname === '/' : pathname === page.to || pathname.startsWith(`${page.to}/`)))
    .pop()
}
