import { Link } from '@tanstack/react-router'
import { useEffect, useRef, useState, type ButtonHTMLAttributes, type ReactNode } from 'react'
import { prettyJSON } from '../format'
import { useToast } from './Toasts'

export type IconName =
  | 'overview'
  | 'telescope'
  | 'requests'
  | 'logs'
  | 'exceptions'
  | 'queue'
  | 'actions'
  | 'system'
  | 'sessions'
  | 'settings'
  | 'flags'
  | 'files'
  | 'developers'
  | 'docs'
  | 'logout'
  | 'refresh'
  | 'search'
  | 'copy'
  | 'external'
  | 'chevron'
  | 'x'
  | 'play'
  | 'pause'
  | 'trash'
  | 'check'
  | 'alert'
  | 'clock'
  | 'activity'
  | 'terminal'
  | 'playground'
  | 'grip'
  | 'plus'
  | 'pencil'
  | 'folder'
  | 'eye'
  | 'grid'
  | 'list'
  | 'download'
  | 'backup'
  | 'mail'
  | 'calendar'
  | 'tenants'
  | 'inspector'
  | 'shield'
  | 'package'
  | 'route'
  | 'sun'
  | 'moon'
  | 'monitor'
  | 'palette'
  | 'home'
  | 'user'
  | 'users'
  | 'bell'
  | 'globe'
  | 'key'
  | 'building'

const paths: Record<IconName, ReactNode> = {
  home: <><path d="M3.5 11 12 4l8.5 7" /><path d="M5.5 9.5V20h13V9.5M10 20v-5h4v5" /></>,
  user: <><circle cx="12" cy="8" r="4" /><path d="M4.5 20.5a7.5 7.5 0 0 1 15 0" /></>,
  users: <><circle cx="9" cy="8" r="3.5" /><path d="M2.5 20a6.5 6.5 0 0 1 13 0" /><path d="M15.5 4.6a3.5 3.5 0 0 1 0 6.8M18 14.2a6.5 6.5 0 0 1 3.5 5.8" /></>,
  bell: <><path d="M6 16.5V11a6 6 0 0 1 12 0v5.5l1.5 2H4.5z" /><path d="M10 20.5a2 2 0 0 0 4 0" /></>,
  globe: <><circle cx="12" cy="12" r="8.5" /><path d="M3.5 12h17M12 3.5c2.5 2.6 3.5 5.4 3.5 8.5s-1 5.9-3.5 8.5c-2.5-2.6-3.5-5.4-3.5-8.5s1-5.9 3.5-8.5z" /></>,
  key: <><circle cx="8" cy="15" r="4" /><path d="m10.8 12.2 8.7-8.7M16 7l2.5 2.5M14 9l2 2" /></>,
  building: <><rect x="4.5" y="3.5" width="15" height="17" rx="1.5" /><path d="M9 7.5h.01M15 7.5h.01M9 11h.01M15 11h.01M9 14.5h.01M15 14.5h.01M10 20.5v-3h4v3" /></>,
  tenants: <><path d="M3 21h18M5 21V7l7-4 7 4v14" /><path d="M9 21v-5h6v5M9 10h.01M15 10h.01M9 13h.01M15 13h.01" /></>,
  calendar: <><rect x="3.5" y="5" width="17" height="15" rx="2" /><path d="M3.5 10h17M8 3v4M16 3v4" /></>,
  inspector: <><circle cx="10.5" cy="10.5" r="6" /><path d="m20 20-5-5M8 10.5h5M10.5 8v5" /></>,
  shield: <><path d="M12 3 4.5 6v6c0 4.4 3.2 7.8 7.5 9 4.3-1.2 7.5-4.6 7.5-9V6z" /><path d="m9 12 2 2 4-4" /></>,
  package: <><path d="m12 3 8 4.5v9L12 21l-8-4.5v-9z" /><path d="m4 7.5 8 4.5 8-4.5M12 12v9" /></>,
  route: <><circle cx="6" cy="18" r="2" /><circle cx="18" cy="6" r="2" /><path d="M8 18h7a3 3 0 0 0 0-6H9a3 3 0 0 1 0-6h7" /></>,
  sun: <><circle cx="12" cy="12" r="4" /><path d="M12 2.5v2M12 19.5v2M4.6 4.6 6 6M18 18l1.4 1.4M2.5 12h2M19.5 12h2M4.6 19.4 6 18M18 6l1.4-1.4" /></>,
  moon: <path d="M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5z" />,
  monitor: <><rect x="3" y="4" width="18" height="12" rx="2" /><path d="M8 20h8M12 16v4" /></>,
  palette: <><path d="M12 3a9 9 0 1 0 0 18c1.1 0 1.7-.8 1.7-1.7 0-.5-.2-.9-.5-1.2-.3-.3-.5-.7-.5-1.2 0-.9.8-1.7 1.7-1.7H16a5 5 0 0 0 5-5c0-4-4-7.2-9-7.2z" /><circle cx="7.5" cy="11" r="1" /><circle cx="10.5" cy="7.5" r="1" /><circle cx="15" cy="7.5" r="1" /></>,
  overview: <><rect x="3" y="3" width="7" height="7" rx="1.5" /><rect x="14" y="3" width="7" height="7" rx="1.5" /><rect x="3" y="14" width="7" height="7" rx="1.5" /><rect x="14" y="14" width="7" height="7" rx="1.5" /></>,
  telescope: <><circle cx="12" cy="12" r="8" /><path d="M12 4v4M12 16v4M4 12h4M16 12h4" /><circle cx="12" cy="12" r="2.5" /></>,
  requests: <><path d="M4 7h16M4 12h10M4 17h13" /><circle cx="20" cy="17" r="2" /></>,
  logs: <><path d="M5 4h10l4 4v12H5z" /><path d="M9 12h6M9 16h6M9 8h3" /></>,
  exceptions: <><path d="M12 3 2.5 20h19z" /><path d="M12 9v5M12 17.2v.3" /></>,
  queue: <><path d="M4 6h16M4 12h16M4 18h16" /><circle cx="7" cy="6" r="1.2" /><circle cx="7" cy="12" r="1.2" /><circle cx="7" cy="18" r="1.2" /></>,
  actions: <><path d="M13 2 4 14h7l-1 8 9-12h-7z" /></>,
  system: <><circle cx="12" cy="12" r="3" /><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.9.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.9 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.9l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.9.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.9-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.9V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z" /></>,
  sessions: <><rect x="3" y="5" width="18" height="14" rx="2" /><path d="M8 21h8M12 19v2M7 10h4M7 13h7" /></>,
  settings: <><path d="M4 7h9M17 7h3M4 17h3M11 17h9" /><circle cx="15" cy="7" r="2" /><circle cx="9" cy="17" r="2" /></>,
  flags: <><path d="M5 21V4" /><path d="M5 5h11l-2 4 2 4H5" /></>,
  files: <><path d="M4 6h6l2 2h8v11H4z" /><path d="M4 10h16" /></>,
  developers: <><circle cx="9" cy="8" r="3" /><path d="M3 20c0-3.3 2.7-5.5 6-5.5s6 2.2 6 5.5" /><path d="M17 8h4M19 6v4" /></>,
  docs: <><path d="M6 3h9l4 4v14H6z" /><path d="M9 12h6M9 16h6M9 8h3" /></>,
  logout: <><path d="M10 4H5v16h5" /><path d="M15 8l4 4-4 4M9 12h10" /></>,
  refresh: <><path d="M20 11a8 8 0 1 0-2.3 6.3" /><path d="M20 4v7h-7" /></>,
  search: <><circle cx="11" cy="11" r="6" /><path d="m20 20-4.2-4.2" /></>,
  copy: <><rect x="9" y="9" width="11" height="11" rx="2" /><path d="M5 15H4V4h11v1" /></>,
  external: <><path d="M14 4h6v6M20 4l-9 9" /><path d="M20 14v6H4V4h6" /></>,
  chevron: <path d="m9 6 6 6-6 6" />,
  x: <path d="M6 6l12 12M18 6 6 18" />,
  play: <path d="m8 5 11 7-11 7z" />,
  pause: <><path d="M9 5v14M15 5v14" /></>,
  trash: <><path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13" /></>,
  check: <path d="m5 12 5 5 9-10" />,
  alert: <><circle cx="12" cy="12" r="9" /><path d="M12 7v6M12 16.5v.5" /></>,
  clock: <><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 2" /></>,
  activity: <path d="M3 12h4l2.5-7 4 14 2.5-7h5" />,
  terminal: <><rect x="3" y="4" width="18" height="16" rx="2" /><path d="m7 9 3 3-3 3M12.5 15H17" /></>,
  playground: <><path d="M9 3h6M10 3v5L5.5 18.5A2 2 0 0 0 7.3 21h9.4a2 2 0 0 0 1.8-2.5L14 8V3" /><path d="M7.5 15h9" /></>,
  grip: <><circle cx="9" cy="6" r="1" /><circle cx="15" cy="6" r="1" /><circle cx="9" cy="12" r="1" /><circle cx="15" cy="12" r="1" /><circle cx="9" cy="18" r="1" /><circle cx="15" cy="18" r="1" /></>,
  plus: <><path d="M12 5v14M5 12h14" /></>,
  pencil: <><path d="m4 20 4.5-1 10-10a2.1 2.1 0 0 0-3-3l-10 10z" /><path d="m13.5 8.5 3 3" /></>,
  folder: <><path d="M3 7a2 2 0 0 1 2-2h5l2 2h7a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" /></>,
  eye: <><path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12z" /><circle cx="12" cy="12" r="3" /></>,
  grid: <><rect x="3" y="3" width="7" height="7" rx="1.5" /><rect x="14" y="3" width="7" height="7" rx="1.5" /><rect x="3" y="14" width="7" height="7" rx="1.5" /><rect x="14" y="14" width="7" height="7" rx="1.5" /></>,
  list: <><path d="M8 6h13M8 12h13M8 18h13" /><circle cx="4" cy="6" r="1" /><circle cx="4" cy="12" r="1" /><circle cx="4" cy="18" r="1" /></>,
  download: <><path d="M12 3v12M7 10l5 5 5-5" /><path d="M5 20h14" /></>,
  mail: <><rect x="3" y="5" width="18" height="14" rx="2" /><path d="m3.5 6.5 8.5 6.5 8.5-6.5" /></>,
  backup: <><ellipse cx="12" cy="5" rx="7" ry="3" /><path d="M5 5v6c0 1.7 3.1 3 7 3s7-1.3 7-3V5" /><path d="M5 11v6c0 1.7 3.1 3 7 3" /><path d="M17 16v6M14.5 19.5 17 22l2.5-2.5" /></>,
}

export function Icon({ name, className, style }: { name: IconName; className?: string; style?: React.CSSProperties }) {
  return (
    <svg
      className={className}
      style={style}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.7"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {paths[name]}
    </svg>
  )
}

type ButtonVariant = 'default' | 'primary' | 'danger' | 'ghost'
type ButtonSize = 'default' | 'sm'

export function Button({
  variant = 'default',
  size = 'default',
  icon,
  children,
  className,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant
  size?: ButtonSize
  icon?: IconName
}) {
  return (
    <button
      className={['btn', variant !== 'default' ? variant : '', size === 'sm' ? 'sm' : '', className ?? ''].join(' ').trim()}
      {...rest}
    >
      {icon ? <Icon name={icon} /> : null}
      {children}
    </button>
  )
}

/**
 * A card. With a title it gets a header row (title, description, actions)
 * over its body; `flush` drops the body padding for a table or list.
 */
export function Card({
  children,
  className,
  tight,
  title,
  description,
  actions,
  flush,
}: {
  children: ReactNode
  className?: string
  tight?: boolean
  title?: ReactNode
  description?: ReactNode
  actions?: ReactNode
  flush?: boolean
}) {
  if (title === undefined) {
    return <div className={`card ${tight ? 'tight' : ''} ${flush ? 'flush' : ''} ${className ?? ''}`}>{children}</div>
  }
  return (
    <section className={`card framed ${className ?? ''}`}>
      <header className="card-head">
        <div className="card-head-text">
          <h2 className="card-title">{title}</h2>
          {description ? <div className="card-sub">{description}</div> : null}
        </div>
        {actions ? <div className="card-actions">{actions}</div> : null}
      </header>
      <div className={flush ? 'card-flush' : 'card-body'}>{children}</div>
    </section>
  )
}

export function CardHeader({ title, sub, actions }: { title: string; sub?: string; actions?: ReactNode }) {
  return (
    <div className="card-header">
      <div>
        <div className="card-title">{title}</div>
        {sub ? <div className="card-sub">{sub}</div> : null}
      </div>
      {actions ? <div className="card-actions">{actions}</div> : null}
    </div>
  )
}

/**
 * Every screen starts with this: title, a one-line description, actions,
 * and optional meta such as "updated 12 s ago".
 */
export function PageHeader({
  title,
  description,
  meta,
  actions,
}: {
  title: string
  description?: ReactNode
  meta?: ReactNode
  actions?: ReactNode
}) {
  return (
    <div className="page-header">
      <div className="page-header-text">
        <h1 className="page-title">{title}</h1>
        {description ? <p className="page-sub">{description}</p> : null}
        {meta ? <div className="page-meta">{meta}</div> : null}
      </div>
      {actions ? <div className="page-actions">{actions}</div> : null}
    </div>
  )
}

/** "← Back to …" above a detail page. */
export function BackLink({ to, search, children }: { to: string; search?: Record<string, unknown>; children: ReactNode }) {
  return (
    <Link to={to} search={search} className="back-link">
      <Icon name="chevron" className="flip-x" /> {children}
    </Link>
  )
}

/** A detail page's summary: label/value pairs in one strip under the title. */
export function DetailStrip({ items }: { items: { label: string; value: ReactNode; wide?: boolean }[] }) {
  return (
    <dl className="detail-strip">
      {items.map((item) => (
        <div key={item.label} className={item.wide ? 'wide' : undefined}>
          <dt>{item.label}</dt>
          <dd>{item.value}</dd>
        </div>
      ))}
    </dl>
  )
}

// ── Layout primitives: routes use these instead of inline styles ─────────

type Space = 0 | 1 | 2 | 3 | 4 | 5 | 6

/** A vertical stack with a gap from the spacing scale. */
export function Stack({ gap = 3, className, children }: { gap?: Space; className?: string; children: ReactNode }) {
  return <div className={`l-stack g-${gap} ${className ?? ''}`}>{children}</div>
}

/** A wrapping row, vertically centred. */
export function Cluster({
  gap = 2,
  justify,
  align,
  className,
  children,
}: {
  gap?: Space
  justify?: 'between' | 'end'
  align?: 'start' | 'end'
  className?: string
  children: ReactNode
}) {
  const classes = ['l-cluster', `g-${gap}`, justify ? `justify-${justify}` : '', align ? `align-${align}` : '', className ?? '']
  return <div className={classes.join(' ').trim()}>{children}</div>
}

/** An aside (a list, filters) next to the main content; stacks under 1100 px. */
export function Split({
  aside,
  asideWidth = 'md',
  children,
  className,
}: {
  aside: ReactNode
  asideWidth?: 'sm' | 'md' | 'lg'
  children: ReactNode
  className?: string
}) {
  return (
    <div className={`l-split aside-${asideWidth} ${className ?? ''}`}>
      <div className="l-split-aside">{aside}</div>
      <div className="l-split-main">{children}</div>
    </div>
  )
}

/** A responsive grid: as many columns of at least `min` as fit. */
export function Grid({
  min = 'md',
  gap = 3,
  className,
  children,
}: {
  min?: 'sm' | 'md' | 'lg'
  gap?: Space
  className?: string
  children: ReactNode
}) {
  return <div className={`l-grid min-${min} g-${gap} ${className ?? ''}`}>{children}</div>
}

function AnimatedNumber({ value }: { value: number }) {
  const [display, setDisplay] = useState(value)
  const previous = useRef(value)
  useEffect(() => {
    const from = previous.current
    previous.current = value
    if (from === value) {
      setDisplay(value)
      return
    }
    let frame = 0
    const started = performance.now()
    const duration = 520
    const tick = (now: number) => {
      const progress = Math.min(1, (now - started) / duration)
      const eased = 1 - Math.pow(1 - progress, 3)
      setDisplay(Math.round(from + (value - from) * eased))
      if (progress < 1) frame = requestAnimationFrame(tick)
    }
    frame = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(frame)
  }, [value])
  return <>{display.toLocaleString()}</>
}

export type StatTone = 'ok' | 'warn' | 'danger' | 'accent'

/**
 * A summary number: label, value and hint. A tone colours only the value;
 * `to` makes the tile a link to the screen with the details.
 */
export function StatTile({
  label,
  value,
  tone,
  hint,
  to,
  search,
}: {
  label: ReactNode
  value: ReactNode
  tone?: StatTone
  hint?: ReactNode
  to?: string
  search?: Record<string, unknown>
}) {
  const body = (
    <>
      <div className="stat-label">{label}</div>
      <div className={`stat-value ${tone ?? ''}`}>{typeof value === 'number' ? <AnimatedNumber value={value} /> : value}</div>
      {hint ? <div className="stat-hint">{hint}</div> : null}
    </>
  )
  if (to) {
    return (
      <Link to={to} search={search} className="stat-card linked">
        {body}
      </Link>
    )
  }
  return <div className="stat-card">{body}</div>
}

export type BadgeTone = 'neutral' | 'accent' | 'ok' | 'warn' | 'danger' | 'info' | 'violet'

/** A short label in sentence case; the tone carries meaning, not decoration. */
export function Badge({
  tone = 'neutral',
  mono,
  title,
  children,
}: {
  tone?: BadgeTone
  mono?: boolean
  title?: string
  children: ReactNode
}) {
  return (
    <span className={`badge ${tone} ${mono ? 'mono' : ''}`} title={title}>
      {children}
    </span>
  )
}

export type DotTone = 'neutral' | 'ok' | 'warn' | 'danger' | 'info'

/** A static status dot. */
export function StatusDot({ tone = 'neutral', title }: { tone?: DotTone; title?: string }) {
  return <span className={`status-dot ${tone}`} title={title} aria-hidden={title ? undefined : true} />
}

/** The one animated dot: marks work that is running right now. */
export function LiveDot({ title = 'Running' }: { title?: string }) {
  return <span className="status-dot live" title={title} role="status" aria-label={title} />
}

/** A key as printed on the keyboard: letters in capitals (G, K), named keys as is (Ctrl, Esc). */
export function Kbd({ children }: { children: ReactNode }) {
  return <kbd>{typeof children === 'string' && children.length === 1 ? children.toUpperCase() : children}</kbd>
}

export function StatusChip({ status }: { status: number }) {
  const tone = status >= 500 ? 'danger' : status >= 400 ? 'warn' : status >= 300 ? 'info' : 'ok'
  return (
    <Badge tone={tone} mono>
      {status}
    </Badge>
  )
}

export function MethodBadge({ method }: { method: string }) {
  return <span className={`method ${method.toUpperCase()}`}>{method.toUpperCase()}</span>
}

export function EmptyState({
  title,
  description,
  icon = 'telescope',
  action,
}: {
  title: string
  description?: string
  icon?: IconName
  action?: ReactNode
}) {
  return (
    <div className="empty">
      <div className="empty-icon">
        <Icon name={icon} />
      </div>
      <div className="empty-title">{title}</div>
      {description ? <div className="empty-sub">{description}</div> : null}
      {action}
    </div>
  )
}

export function Spinner({ large }: { large?: boolean }) {
  return <span className={`spinner ${large ? 'lg' : ''}`} aria-label="Loading" />
}

export function LoadingBlock({ label = 'Loading…' }: { label?: string }) {
  return (
    <div className="skeleton-rows" role="status" aria-label={label}>
      <div className="skeleton-row heading" />
      {[92, 74, 86, 63, 78].map((width, index) => (
        <div
          key={index}
          className="skeleton-row"
          style={{ width: `${width}%`, animationDelay: `${index * 90}ms` }}
        />
      ))}
    </div>
  )
}

export function ErrorBox({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div className="error-box" role="alert">
      <Icon name="alert" />
      <span className="error-box-text">{message}</span>
      {onRetry ? (
        <Button size="sm" variant="ghost" icon="refresh" onClick={onRetry}>
          Retry
        </Button>
      ) : null}
    </div>
  )
}

export function Tabs<T extends string>({
  tabs,
  active,
  onChange,
}: {
  tabs: { id: T; label: string; count?: number }[]
  active: T
  onChange: (id: T) => void
}) {
  return (
    <div className="tabs" role="tablist">
      {tabs.map((tab) => (
        <button
          key={tab.id}
          role="tab"
          aria-selected={active === tab.id}
          className={`tab ${active === tab.id ? 'active' : ''}`}
          onClick={() => onChange(tab.id)}
        >
          {tab.label}
          {tab.count !== undefined ? <span className="count">{tab.count}</span> : null}
        </button>
      ))}
    </div>
  )
}

export function CodeBlock({ code, label, maxHeight }: { code: string; label?: string; maxHeight?: number }) {
  return (
    <div className="code-block">
      <div className="code-block-bar">
        <span className="code-block-label">{label ?? 'Code'}</span>
        <CopyButton value={code} />
      </div>
      <pre className="json" style={maxHeight ? { maxHeight } : undefined}>
        {code}
      </pre>
    </div>
  )
}

export function JsonView({ value, maxHeight, label }: { value: unknown; maxHeight?: number; label?: string }) {
  return <CodeBlock code={prettyJSON(value)} label={label ?? 'JSON'} maxHeight={maxHeight} />
}

export function CopyButton({ value, label }: { value: string; label?: string }) {
  const { push } = useToast()
  return (
    <button
      type="button"
      className="btn ghost sm"
      title="Copy to clipboard"
      onClick={() => {
        void navigator.clipboard
          .writeText(value)
          .then(() => push('Copied to clipboard', 'success'))
          .catch(() => push('Clipboard unavailable', 'error'))
      }}
    >
      <Icon name="copy" />
      {label ?? null}
    </button>
  )
}

// Open overlays, newest last: Escape closes only the top one.
const escapeStack: { current: () => void }[] = []

if (typeof document !== 'undefined') {
  document.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape' || escapeStack.length === 0) return
    event.preventDefault()
    escapeStack[escapeStack.length - 1].current()
  })
}

/** Closes an open overlay on Escape when it is the top-most one. */
export function useEscape(open: boolean, onClose: () => void) {
  const handler = useRef(onClose)
  handler.current = onClose
  useEffect(() => {
    if (!open) return
    const entry = { current: () => handler.current() }
    escapeStack.push(entry)
    return () => {
      escapeStack.splice(escapeStack.indexOf(entry), 1)
    }
  }, [open])
}

export function Modal({
  open,
  title,
  description,
  actions,
  size,
  onClose,
  children,
}: {
  open: boolean
  title: ReactNode
  description?: ReactNode
  actions?: ReactNode
  /** lg: 640 px instead of 440 px. */
  size?: 'lg'
  onClose: () => void
  children: ReactNode
}) {
  useEscape(open, onClose)
  if (!open) return null
  return (
    <div className="overlay center" onClick={onClose}>
      <div className={`modal ${size ?? ''}`} role="dialog" aria-modal="true" onClick={(event) => event.stopPropagation()}>
        <h2 className="modal-title">{title}</h2>
        {description ? <p className="modal-sub">{description}</p> : null}
        <div className="modal-body">{children}</div>
        {actions ? <div className="modal-actions">{actions}</div> : null}
      </div>
    </div>
  )
}

export function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel = 'Confirm',
  danger,
  busy,
  onConfirm,
  onCancel,
}: {
  open: boolean
  title: string
  description?: string
  confirmLabel?: string
  danger?: boolean
  busy?: boolean
  onConfirm: () => void
  onCancel: () => void
}) {
  useEscape(open && !busy, onCancel)
  if (!open) return null
  return (
    <div className="overlay center" onClick={onCancel}>
      <div className="modal" role="alertdialog" aria-modal="true" onClick={(event) => event.stopPropagation()}>
        <h2 className="modal-title">{title}</h2>
        {description ? <p className="modal-sub">{description}</p> : null}
        <div className="modal-actions">
          <Button variant="ghost" onClick={onCancel} disabled={busy}>
            Cancel
          </Button>
          <Button variant={danger ? 'danger' : 'primary'} onClick={onConfirm} disabled={busy}>
            {busy ? <Spinner /> : null}
            {confirmLabel}
          </Button>
        </div>
      </div>
    </div>
  )
}

export function Drawer({
  open,
  title,
  subtitle,
  size = 'lg',
  onClose,
  actions,
  children,
}: {
  open: boolean
  title: ReactNode
  subtitle?: ReactNode
  /** md 560 px, lg 720 px (default), fit: as wide as the content. */
  size?: 'md' | 'lg' | 'fit'
  onClose: () => void
  actions?: ReactNode
  children: ReactNode
}) {
  useEscape(open, onClose)
  if (!open) return null
  return (
    <div className="overlay" onClick={onClose}>
      <aside className={`drawer ${size}`} role="dialog" aria-modal="true" onClick={(event) => event.stopPropagation()}>
        <header className="drawer-header">
          <div className="drawer-heading">
            <div className="drawer-title">{title}</div>
            {subtitle ? <div className="card-sub">{subtitle}</div> : null}
          </div>
          <div className="spacer" />
          {actions}
          <button type="button" className="icon-btn" onClick={onClose} aria-label="Close">
            <Icon name="x" />
          </button>
        </header>
        <div className="drawer-body">{children}</div>
      </aside>
    </div>
  )
}

export function RefreshToggle({ enabled, onChange }: { enabled: boolean; onChange: (value: boolean) => void }) {
  return (
    <label className="checkbox">
      <input type="checkbox" checked={enabled} onChange={(event) => onChange(event.target.checked)} />
      Live
    </label>
  )
}
