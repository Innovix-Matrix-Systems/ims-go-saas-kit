import { useQuery } from '@tanstack/react-query'
import { Link, Outlet, useRouterState } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { api, fetchMe, getCachedMe, logoutRedirect } from '../api/client'
import type { SystemInfo } from '../api/types'
import { initials } from '../format'
import { AppearanceMenu } from './AppearanceMenu'
import { CommandPalette } from './CommandPalette'
import { MaintenancePill } from './Maintenance'
import { navGroups, pageFor, visiblePages, type NavGroup } from './navigation'
import { ShortcutsSheet } from './ShortcutsSheet'
import { useShortcuts } from './shortcuts'
import { Badge, ErrorBox, Icon, Kbd, Spinner, type BadgeTone } from './ui'

const SIDEBAR_KEY = 'ims.dev.sidebar'

function readCollapsed(): boolean {
  try {
    return localStorage.getItem(SIDEBAR_KEY) === 'collapsed'
  } catch {
    return false
  }
}

export function AppShell() {
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  const [commandOpen, setCommandOpen] = useState(false)
  const [helpOpen, setHelpOpen] = useState(false)
  const [collapsed, setCollapsed] = useState(readCollapsed)
  const [menuOpen, setMenuOpen] = useState(false)
  const meQuery = useQuery({
    queryKey: ['me'],
    queryFn: fetchMe,
    staleTime: 60_000,
    retry: false,
    placeholderData: getCachedMe() ?? undefined,
  })
  const system = useQuery({
    queryKey: ['system-info'],
    queryFn: ({ signal }) => api<SystemInfo>('/api/v1/system/info', { signal }),
    staleTime: 5 * 60_000,
    retry: false,
  })
  const me = meQuery.data
  const pages = visiblePages(me?.playground)
  const current = pageFor(pathname)
  const environment = system.data?.environment ?? ''
  const production = environment.toLowerCase() === 'production'
  const canManage = Boolean(me && (me.is_super_admin || me.permissions.includes('dev.manage')))

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

  // One nav group; Sandbox (local-only pages) goes last, after Reference.
  const navGroup = (group: NavGroup) => {
    const items = pages.filter((page) => page.group === group)
    if (items.length === 0) return null
    return (
      <div key={group} className="nav-group">
        <div className="nav-section">{group}</div>
        {items.map((page) => (
          <Link
            key={page.to}
            to={page.to}
            className="nav-item"
            activeProps={{ className: 'nav-item active' }}
            activeOptions={{ exact: page.to === '/' }}
            title={collapsed ? page.label : undefined}
          >
            <Icon name={page.icon} />
            <span className="nav-label">{page.label}</span>
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

  if (meQuery.isError && !me) {
    const message = meQuery.error instanceof Error ? meQuery.error.message : 'Unable to load identity'
    return (
      <div className="login-wrap">
        <div className="login-card card">
          <h1 className="login-title">Access unavailable</h1>
          <p className="login-sub">{message}</p>
          <ErrorBox message="The developer dashboard requires the dev.view permission. Sign in with a super-admin or developer account." />
          <div className="modal-actions">
            <Link to="/login" className="btn ghost">
              Sign in
            </Link>
            <button type="button" className="btn primary" onClick={logoutRedirect}>
              Sign out
            </button>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className={`app ${collapsed ? 'sidebar-collapsed' : ''} ${menuOpen ? 'menu-open' : ''} ${production ? 'production' : ''}`}>
      {production ? <div className="production-line" role="presentation" /> : null}
      <aside className="sidebar" aria-label="Navigation">
        <div className="brand">
          <div className="brand-mark">D</div>
          <div className="brand-text">
            <div className="brand-title">Dev dashboard</div>
            <div className="brand-meta">
              {environment ? (
                <Badge tone={envTone(environment)} title="Environment">
                  {environment}
                </Badge>
              ) : null}
              {system.data ? <span className="brand-version">v{system.data.version}</span> : null}
            </div>
          </div>
        </div>
        <nav className="nav">
          {navGroups.filter((group) => group !== 'Sandbox').map(navGroup)}
          <div className="nav-group">
            <div className="nav-section">Reference</div>
            <a className="nav-item external" href="/docs" target="_blank" rel="noreferrer" title={collapsed ? 'API docs' : undefined}>
              <Icon name="docs" />
              <span className="nav-label">API docs</span>
              <Icon name="external" className="icon-inline nav-ext" />
            </a>
          </div>
          {navGroup('Sandbox')}
        </nav>
        <div className="sidebar-footer">
          <div className="user-card">
            <div className="avatar">{me ? initials(me.name) : <Spinner />}</div>
            <div className="user-meta">
              <div className="user-name">{me?.name ?? 'Developer'}</div>
              <div className="user-email" title={me?.email}>
                {me ? (me.is_super_admin ? 'Super-admin' : 'Developer') : 'loading…'}
              </div>
            </div>
            <button type="button" className="icon-btn" title="Sign out" aria-label="Sign out" onClick={logoutRedirect}>
              <Icon name="logout" />
            </button>
          </div>
          <button
            type="button"
            className="sidebar-toggle"
            onClick={() => setCollapsed((value) => !value)}
            aria-label={collapsed ? 'Expand the sidebar' : 'Collapse the sidebar'}
            title={collapsed ? 'Expand the sidebar ([)' : 'Collapse the sidebar ([)'}
          >
            <Icon name="chevron" className={collapsed ? '' : 'flip-x'} />
            <span className="nav-label">Collapse</span>
            <Kbd>[</Kbd>
          </button>
        </div>
      </aside>
      <div className="sidebar-scrim" onClick={() => setMenuOpen(false)} role="presentation" />
      <div className="main">
        <header className="topbar">
          <button type="button" className="icon-btn menu-btn" aria-label="Open the menu" onClick={() => setMenuOpen(true)}>
            <Icon name="list" />
          </button>
          <div className="topbar-title">{current?.label ?? 'Developer dashboard'}</div>
          <div className="topbar-right">
            <MaintenancePill />
            {me && !canManage ? (
              <Badge tone="neutral" title="You can look but not change anything (no dev.manage permission).">
                Read-only
              </Badge>
            ) : null}
            <button type="button" className="command-trigger" onClick={() => setCommandOpen(true)}>
              <Icon name="search" />
              <span className="command-label">Search or jump to…</span>
              <span className="keys">
                <Kbd>Ctrl</Kbd>
                <Kbd>k</Kbd>
              </span>
            </button>
            <AppearanceMenu />
            <button type="button" className="icon-btn" title="Keyboard shortcuts (?)" aria-label="Keyboard shortcuts" onClick={() => setHelpOpen(true)}>
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

function envTone(env: string): BadgeTone {
  switch (env.toLowerCase()) {
    case 'production':
      return 'danger'
    case 'staging':
      return 'warn'
    default:
      return 'neutral'
  }
}
