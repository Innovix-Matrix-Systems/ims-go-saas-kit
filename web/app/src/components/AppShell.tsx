import { Link, Outlet, useNavigate, useRouterState } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { signOut, useSession } from '../auth/session'
import { initials } from '../format'
import { useT } from '../i18n'
import { ActingBanner } from './ActingBanner'
import { AppearanceMenu } from './AppearanceMenu'
import { BranchSwitcher } from './BranchSwitcher'
import { CommandPalette } from './CommandPalette'
import { navGroupLabels, navGroups, pageFor, visiblePages, type NavGroup } from './navigation'
import { NotificationBell } from './NotificationBell'
import { ShortcutsSheet } from './ShortcutsSheet'
import { useShortcuts } from './shortcuts'
import { Icon, Kbd, Spinner } from './ui'

const SIDEBAR_KEY = 'ims.app.sidebar'

function readCollapsed(): boolean {
  try {
    return localStorage.getItem(SIDEBAR_KEY) === 'collapsed'
  } catch {
    return false
  }
}

/** The signed-in frame: sidebar, top bar, and the current page. */
export function AppShell() {
  const t = useT()
  const navigate = useNavigate()
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  const session = useSession()
  const [commandOpen, setCommandOpen] = useState(false)
  const [helpOpen, setHelpOpen] = useState(false)
  const [collapsed, setCollapsed] = useState(readCollapsed)
  const [menuOpen, setMenuOpen] = useState(false)
  const pages = visiblePages(session)
  const current = pageFor(pathname)
  const user = session?.user

  useEffect(() => {
    try {
      localStorage.setItem(SIDEBAR_KEY, collapsed ? 'collapsed' : 'expanded')
    } catch {
      /* storage disabled */
    }
  }, [collapsed])

  // The off-canvas menu (narrow screens) closes once a page is picked.
  useEffect(() => setMenuOpen(false), [pathname])

  useShortcuts({
    pages,
    onPalette: () => setCommandOpen((value) => !value),
    onHelp: () => setHelpOpen(true),
    onToggleSidebar: () => setCollapsed((value) => !value),
  })

  const handleSignOut = async () => {
    await signOut()
    await navigate({ to: '/login' })
  }

  const navGroup = (group: NavGroup) => {
    const items = pages.filter((page) => page.group === group)
    if (items.length === 0) return null
    return (
      <div key={group} className="nav-group">
        <div className="nav-section">{t(navGroupLabels[group])}</div>
        {items.map((page) => (
          <Link
            key={page.to}
            to={page.to}
            className="nav-item"
            activeProps={{ className: 'nav-item active' }}
            activeOptions={{ exact: page.to === '/' }}
            title={collapsed ? t(page.label) : undefined}
          >
            <Icon name={page.icon} />
            <span className="nav-label">{t(page.label)}</span>
            {page.key ? (
              <span className="nav-key" aria-label={`Shortcut g then ${page.key}`}>
                <Kbd>g</Kbd>
                <Kbd>{page.key}</Kbd>
              </span>
            ) : null}
          </Link>
        ))}
      </div>
    )
  }

  const roleLabel = session?.isPlatformUser
    ? t('role.superAdmin')
    : (user?.roles?.map((role) => role.name).join(', ') || t('role.member'))

  return (
    <div className={`app ${collapsed ? 'sidebar-collapsed' : ''} ${menuOpen ? 'menu-open' : ''}`}>
      <aside className="sidebar" aria-label="Navigation">
        <div className="brand">
          <div className="brand-mark">I</div>
          <div className="brand-text">
            <div className="brand-title">{t('app.name')}</div>
          </div>
        </div>
        <nav className="nav">{navGroups.map(navGroup)}</nav>
        <div className="sidebar-footer">
          <div className="user-card">
            <div className="avatar">{user ? initials(user.name) : <Spinner />}</div>
            <div className="user-meta">
              <div className="user-name">{user?.name ?? '…'}</div>
              <div className="user-email" title={user?.email}>
                {user ? roleLabel : t('common.loading')}
              </div>
            </div>
            <button
              type="button"
              className="icon-btn"
              title={t('common.signOut')}
              aria-label={t('common.signOut')}
              onClick={() => void handleSignOut()}
            >
              <Icon name="logout" />
            </button>
          </div>
          <button
            type="button"
            className="sidebar-toggle"
            onClick={() => setCollapsed((value) => !value)}
            aria-label={collapsed ? t('nav.expand') : t('nav.collapse')}
            title={`${collapsed ? t('nav.expand') : t('nav.collapse')} ([)`}
          >
            <Icon name="chevron" className={collapsed ? '' : 'flip-x'} />
            <span className="nav-label">{t('nav.collapse')}</span>
            <Kbd>[</Kbd>
          </button>
        </div>
      </aside>
      <div className="sidebar-scrim" onClick={() => setMenuOpen(false)} role="presentation" />
      <div className="main">
        <ActingBanner />
        <header className="topbar">
          <button type="button" className="icon-btn menu-btn" aria-label={t('nav.openMenu')} onClick={() => setMenuOpen(true)}>
            <Icon name="list" />
          </button>
          <div className="topbar-title">{current ? t(current.label) : t('app.name')}</div>
          <div className="topbar-right">
            <button type="button" className="command-trigger" onClick={() => setCommandOpen(true)}>
              <Icon name="search" />
              <span className="command-label">{t('nav.searchOrJump')}</span>
              <span className="keys">
                <Kbd>Ctrl</Kbd>
                <Kbd>k</Kbd>
              </span>
            </button>
            <BranchSwitcher />
            <NotificationBell />
            <AppearanceMenu />
            <button
              type="button"
              className="icon-btn"
              title={`${t('nav.shortcuts')} (?)`}
              aria-label={t('nav.shortcuts')}
              onClick={() => setHelpOpen(true)}
            >
              <span className="icon-btn-text">?</span>
            </button>
          </div>
        </header>
        <main className="page">
          <div key={pathname} className="page-anim">
            <Outlet />
          </div>
        </main>
      </div>
      <CommandPalette open={commandOpen} onOpenChange={setCommandOpen} />
      <ShortcutsSheet open={helpOpen} pages={pages} onClose={() => setHelpOpen(false)} />
    </div>
  )
}
