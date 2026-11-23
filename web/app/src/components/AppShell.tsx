import { Link, Outlet, useNavigate, useRouterState } from '@tanstack/react-router'
import { useEffect, useRef, useState, type CSSProperties, type KeyboardEvent, type PointerEvent } from 'react'
import { signOut, useSession } from '../auth/session'
import { initials } from '../format'
import { useT } from '../i18n'
import { ActingBanner } from './ActingBanner'
import { AppearanceMenu } from './AppearanceMenu'
import { BranchSwitcher } from './BranchSwitcher'
import { JobsTray } from './JobsTray'
import { CommandPalette } from './CommandPalette'
import { navGroupLabels, navGroups, pageFor, visiblePages, type NavGroup } from './navigation'
import { NotificationBell } from './NotificationBell'
import { ShortcutsSheet } from './ShortcutsSheet'
import { useShortcuts } from './shortcuts'
import { Icon, Kbd, Spinner } from './ui'

const SIDEBAR_KEY = 'ims.app.sidebar'
const WIDTH_KEY = 'ims.app.sidebar-width'
const DEFAULT_WIDTH = 240
const MIN_WIDTH = 200
const MAX_WIDTH = 400

const clampWidth = (value: number) => Math.round(Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, value)))

function readWidth(): number {
  try {
    const stored = Number(localStorage.getItem(WIDTH_KEY))
    return stored ? clampWidth(stored) : DEFAULT_WIDTH
  } catch {
    return DEFAULT_WIDTH
  }
}

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
  const [width, setWidth] = useState(readWidth)
  const [resizing, setResizing] = useState(false)
  const startX = useRef(0)
  const startWidth = useRef(0)
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

  useEffect(() => {
    try {
      localStorage.setItem(WIDTH_KEY, String(width))
    } catch {
      /* storage disabled */
    }
  }, [width])

  // Drag the sidebar's edge to resize it (pointer capture keeps the drag
  // going over the page); double-click resets the width.
  const startResize = (event: PointerEvent<HTMLDivElement>) => {
    event.currentTarget.setPointerCapture(event.pointerId)
    startX.current = event.clientX
    startWidth.current = width
    setResizing(true)
  }
  const resize = (event: PointerEvent<HTMLDivElement>) => {
    if (resizing) setWidth(clampWidth(startWidth.current + event.clientX - startX.current))
  }
  const resizeWithKeys = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
      event.preventDefault()
      setWidth((value) => clampWidth(value + (event.key === 'ArrowRight' ? 16 : -16)))
    }
  }

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
    <div
      className={`app ${collapsed ? 'sidebar-collapsed' : ''} ${menuOpen ? 'menu-open' : ''} ${resizing ? 'sidebar-resizing' : ''}`}
      style={{ '--sidebar-width': `${width}px` } as CSSProperties}
    >
      <aside className="sidebar" aria-label="Navigation">
        <div className="brand">
          <div className="brand-mark">I</div>
          <div className="brand-text">
            <div className="brand-title">{t('app.name')}</div>
          </div>
          <button
            type="button"
            className={`icon-btn sidebar-pin ${collapsed ? '' : 'pinned'}`}
            onClick={() => setCollapsed((value) => !value)}
            aria-pressed={!collapsed}
            aria-label={collapsed ? t('nav.expand') : t('nav.collapse')}
            title={`${collapsed ? t('nav.expand') : t('nav.collapse')} ([)`}
          >
            <Icon name="pin" />
          </button>
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
        </div>
        {collapsed ? null : (
          <div
            className="sidebar-resizer"
            role="separator"
            aria-orientation="vertical"
            aria-label={t('nav.resize')}
            aria-valuemin={MIN_WIDTH}
            aria-valuemax={MAX_WIDTH}
            aria-valuenow={width}
            tabIndex={0}
            title={t('nav.resize')}
            onPointerDown={startResize}
            onPointerMove={resize}
            onPointerUp={() => setResizing(false)}
            onPointerCancel={() => setResizing(false)}
            onDoubleClick={() => setWidth(DEFAULT_WIDTH)}
            onKeyDown={resizeWithKeys}
          />
        )}
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
            <JobsTray />
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
