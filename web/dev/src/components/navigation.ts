import type { IconName } from './ui'

// Every dashboard screen, in one list: the sidebar, the command palette, the
// shortcuts sheet and the top bar title all read it. A new page adds itself
// here with its group, icon and `g` shortcut letter (unique; checked below).

export interface Page {
  to: string
  label: string
  icon: IconName
  group: NavGroup
  /** `g` then this letter opens the page. */
  key?: string
  /** Words the palette also matches, such as an old name. */
  keywords?: string
  /** Only shown where the Playground is (local and development). */
  localOnly?: boolean
}

export type NavGroup = 'Monitor' | 'Operate' | 'Configure' | 'Data' | 'Diagnose' | 'Sandbox'

export const navGroups: NavGroup[] = ['Monitor', 'Operate', 'Configure', 'Data', 'Diagnose', 'Sandbox']

export const pages: Page[] = [
  { to: '/', label: 'Overview', icon: 'overview', group: 'Monitor', key: 'o' },
  { to: '/requests', label: 'Requests', icon: 'requests', group: 'Monitor', key: 'r', keywords: 'telescope http' },
  { to: '/logs', label: 'Logs', icon: 'logs', group: 'Monitor', key: 'l' },
  { to: '/exceptions', label: 'Exceptions', icon: 'exceptions', group: 'Monitor', key: 'x', keywords: 'errors' },
  { to: '/queue', label: 'Queue', icon: 'queue', group: 'Operate', key: 'q', keywords: 'jobs river' },
  { to: '/commands', label: 'Commands', icon: 'terminal', group: 'Operate', key: 'c' },
  { to: '/scheduler', label: 'Scheduler', icon: 'clock', group: 'Operate', key: 'h', keywords: 'cron schedule tasks' },
  { to: '/backups', label: 'Backups', icon: 'backup', group: 'Operate', key: 'b' },
  { to: '/deliveries', label: 'Deliveries', icon: 'mail', group: 'Operate', key: 'd', keywords: 'mail sms templates' },
  { to: '/settings', label: 'Settings', icon: 'settings', group: 'Configure', key: 's' },
  { to: '/flags', label: 'Feature flags', icon: 'flags', group: 'Configure', key: 'f' },
  { to: '/tenants', label: 'Tenants', icon: 'tenants', group: 'Data', key: 't' },
  { to: '/sessions', label: 'Sessions', icon: 'sessions', group: 'Data', key: 'e', keywords: 'tokens' },
  { to: '/files', label: 'Files', icon: 'files', group: 'Data', key: 'm', keywords: 'media uploads' },
  { to: '/developers', label: 'Developers', icon: 'developers', group: 'Data', key: 'u', keywords: 'users access' },
  { to: '/inspector', label: 'Inspector', icon: 'inspector', group: 'Diagnose', key: 'i', keywords: 'routes config health' },
  { to: '/actions', label: 'Audit log', icon: 'actions', group: 'Diagnose', key: 'a', keywords: 'actions history' },
  { to: '/playground', label: 'Playground', icon: 'playground', group: 'Sandbox', key: 'p', localOnly: true },
  { to: '/kit', label: 'Component kit', icon: 'grid', group: 'Sandbox', key: 'k', localOnly: true },
]

if (import.meta.env.DEV) {
  const keys = pages.flatMap((page) => (page.key ? [page.key] : []))
  const duplicate = keys.find((key, index) => keys.indexOf(key) !== index)
  if (duplicate) throw new Error(`navigation: shortcut g ${duplicate} is used twice`)
}

/** The pages this developer can open. */
export function visiblePages(local: boolean | undefined): Page[] {
  return pages.filter((page) => !page.localOnly || local)
}

/** The page a path belongs to (detail routes map to their list). */
export function pageFor(pathname: string): Page | undefined {
  if (pathname.startsWith('/jobs')) return pages.find((page) => page.to === '/queue')
  return [...pages]
    .filter((page) => (page.to === '/' ? pathname === '/' : pathname === page.to || pathname.startsWith(`${page.to}/`)))
    .pop()
}
