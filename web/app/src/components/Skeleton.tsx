import type { CSSProperties } from 'react'
import { useT } from '../i18n'

// Skeletons stand in for content while it loads, in the shape it will have,
// so a page never sits blank and nothing jumps when the data arrives.

/** One shimmering block. */
export function Skeleton({ width, height = 14, radius, style }: { width?: number | string; height?: number; radius?: number; style?: CSSProperties }) {
  return <span className="skeleton" style={{ display: 'block', width: width ?? '100%', height, borderRadius: radius, ...style }} aria-hidden="true" />
}

/** Widths that look like real text rather than one bar repeated. */
const widths = [72, 54, 88, 63, 80, 47, 69, 58]

/** Marks a region as loading for assistive technology. */
function Loading({ className, children }: { className?: string; children: React.ReactNode }) {
  const t = useT()
  return (
    <div className={className} role="status" aria-busy="true" aria-label={t('common.loading')}>
      {children}
    </div>
  )
}

/** A table: a header and rows of cells; the first column has an avatar line. */
export function TableSkeleton({ rows = 6, columns = 4, avatar = true }: { rows?: number; columns?: number; avatar?: boolean }) {
  return (
    <Loading className="table-wrap">
      <table className="table skeleton-table">
        <thead>
          <tr>
            {Array.from({ length: columns }, (_, column) => (
              <th key={column}>
                <Skeleton width={column === 0 ? 90 : 60} height={10} />
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {Array.from({ length: rows }, (_, row) => (
            <tr key={row}>
              {Array.from({ length: columns }, (_, column) => (
                <td key={column}>
                  {column === 0 && avatar ? (
                    <span className="skeleton-person">
                      <Skeleton width={28} height={28} radius={999} />
                      <span className="skeleton-lines">
                        <Skeleton width={`${widths[row % widths.length]}%`} height={12} />
                        <Skeleton width={`${widths[(row + 3) % widths.length] - 20}%`} height={10} />
                      </span>
                    </span>
                  ) : (
                    <Skeleton width={`${widths[(row + column) % widths.length]}%`} height={12} />
                  )}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </Loading>
  )
}

/** A list of rows: a title and a line under it, an optional leading circle. */
export function ListSkeleton({ rows = 5, avatar = false }: { rows?: number; avatar?: boolean }) {
  return (
    <Loading className="skeleton-list">
      {Array.from({ length: rows }, (_, row) => (
        <div key={row} className="skeleton-list-row">
          {avatar ? <Skeleton width={28} height={28} radius={999} /> : null}
          <span className="skeleton-lines">
            <Skeleton width={`${widths[row % widths.length] - 20}%`} height={13} />
            <Skeleton width={`${widths[(row + 2) % widths.length]}%`} height={10} />
          </span>
        </div>
      ))}
    </Loading>
  )
}

/** A form: labelled fields and a button. */
export function FormSkeleton({ fields = 3 }: { fields?: number }) {
  return (
    <Loading className="skeleton-form">
      {Array.from({ length: fields }, (_, field) => (
        <span key={field} className="skeleton-field">
          <Skeleton width={90 + (field % 3) * 24} height={10} />
          <Skeleton height={34} radius={8} />
        </span>
      ))}
      <Skeleton width={120} height={32} radius={8} />
    </Loading>
  )
}

/** Settings rows: a description on the left, a control on the right. */
export function SettingsSkeleton({ rows = 4 }: { rows?: number }) {
  return (
    <Loading className="skeleton-list">
      {Array.from({ length: rows }, (_, row) => (
        <div key={row} className="skeleton-list-row">
          <span className="skeleton-lines">
            <Skeleton width={`${widths[row % widths.length]}%`} height={13} />
            <Skeleton width={`${widths[(row + 4) % widths.length] - 25}%`} height={10} />
          </span>
          <Skeleton width={row % 2 ? 36 : 160} height={row % 2 ? 20 : 32} radius={row % 2 ? 999 : 8} />
        </div>
      ))}
    </Loading>
  )
}

/** Stat tiles in a row. */
export function TilesSkeleton({ count = 3 }: { count?: number }) {
  return (
    <Loading className="skeleton-tiles">
      {Array.from({ length: count }, (_, tile) => (
        <div key={tile} className="stat-card">
          <Skeleton width={90} height={10} />
          <Skeleton width={70} height={24} style={{ marginTop: 10 }} />
          <Skeleton width={110} height={10} style={{ marginTop: 8 }} />
        </div>
      ))}
    </Loading>
  )
}

/** Cards in a grid (plans and the like). */
export function CardsSkeleton({ count = 3 }: { count?: number }) {
  return (
    <Loading className="plan-grid">
      {Array.from({ length: count }, (_, card) => (
        <div key={card} className="plan-card">
          <Skeleton width="55%" height={14} />
          <Skeleton width="40%" height={24} />
          {[0, 1, 2, 3].map((line) => (
            <Skeleton key={line} width={`${widths[(card + line) % widths.length]}%`} height={10} />
          ))}
          <Skeleton height={32} radius={8} />
        </div>
      ))}
    </Loading>
  )
}

/** A whole page: header, toolbar and a table card. Shown while a page's code loads. */
export function PageSkeleton() {
  return (
    <div className="skeleton-page">
      <div className="page-header">
        <div className="skeleton-lines">
          <Skeleton width={180} height={22} />
          <Skeleton width={300} height={12} />
        </div>
      </div>
      <div className="toolbar">
        <Skeleton width={280} height={34} radius={8} />
      </div>
      <div className="card flush">
        <TableSkeleton />
      </div>
    </div>
  )
}

/** The signed-in frame before the session has loaded: sidebar, top bar and a page. */
export function ShellSkeleton() {
  return (
    <div className="app">
      <aside className="sidebar" aria-hidden="true">
        <div className="brand">
          <div className="brand-mark">I</div>
          <Skeleton width={130} height={14} />
        </div>
        <div className="nav skeleton-nav">
          {[0, 1, 2, 3, 4, 5, 6, 7].map((item) => (
            <span key={item} className="skeleton-nav-item">
              <Skeleton width={16} height={16} radius={4} />
              <Skeleton width={`${widths[item % widths.length]}%`} height={12} />
            </span>
          ))}
        </div>
      </aside>
      <div className="main">
        <header className="topbar">
          <Skeleton width={120} height={14} />
        </header>
        <main className="page">
          <PageSkeleton />
        </main>
      </div>
    </div>
  )
}
