import type { Session } from '../auth/session'
import type { MessageKey } from '../i18n'
import type { IconName } from './ui'

// Every screen of the signed-in app, in one list: the sidebar, the command
// palette, the shortcuts sheet and the top bar title all read it. A new page
// adds itself here with its group, icon, `g` shortcut letter (unique; checked
// below) and the permission it needs, if any.

export type NavGroup = 'workspace' | 'organization' | 'account'

export const navGroups: NavGroup[] = ['workspace', 'organization', 'account']

export const navGroupLabels: Record<NavGroup, MessageKey> = {
  workspace: 'nav.group.workspace',
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
  { to: '/org/members', label: 'org.members', icon: 'users', group: 'organization', key: 'm', keywords: 'users team invite people', permission: 'user.view.all', tenantOnly: true },
  { to: '/org/settings', label: 'org.settings', icon: 'building', group: 'organization', key: 'o', keywords: 'organization company owner transfer policies', tenantOnly: true },
  { to: '/settings/profile', label: 'settings.profile', icon: 'user', group: 'account', key: 'p', keywords: 'name language e-mail' },
  { to: '/settings/security', label: 'settings.security', icon: 'shield', group: 'account', key: 's', keywords: 'password two-factor 2fa sessions devices' },
  { to: '/notifications', label: 'notifications.title', icon: 'bell', group: 'workspace', key: 'n', keywords: 'inbox alerts' },
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
    if (page.tenantOnly && session?.isPlatformUser !== false) return false
    if (page.permission && !session?.permissions.has(page.permission)) return false
    return true
  })
}

/** The page a path belongs to (detail routes map to their list). */
export function pageFor(pathname: string): Page | undefined {
  return [...pages]
    .filter((page) => (page.to === '/' ? pathname === '/' : pathname === page.to || pathname.startsWith(`${page.to}/`)))
    .pop()
}
