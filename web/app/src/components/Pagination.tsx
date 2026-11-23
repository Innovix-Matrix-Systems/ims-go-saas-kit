import { useState } from 'react'
import { useT } from '../i18n'
import { Select } from './inputs'
import { Icon } from './ui'

export const perPageOptions = [10, 20, 50, 100] as const

/** Page numbers to show: the ends, the current page and its neighbours, gaps as null. */
function pageList(page: number, last: number): (number | null)[] {
  const pages = new Set([1, last, page - 1, page, page + 1].filter((n) => n >= 1 && n <= last))
  const sorted = [...pages].sort((a, b) => a - b)
  const out: (number | null)[] = []
  sorted.forEach((n, i) => {
    if (i > 0 && n - sorted[i - 1] > 1) out.push(null)
    out.push(n)
  })
  return out
}

/**
 * The footer of every paginated list: how many rows and which ones are shown,
 * how many per page, and the pages. Shown even when everything fits on one page.
 */
export function Pagination({
  page,
  perPage,
  total,
  onPage,
  onPerPage,
}: {
  page: number
  perPage: number
  total: number
  onPage: (page: number) => void
  onPerPage?: (perPage: number) => void
}) {
  const t = useT()
  const last = Math.max(1, Math.ceil(total / perPage))
  const from = total === 0 ? 0 : (page - 1) * perPage + 1
  const to = Math.min(total, page * perPage)
  return (
    <nav className="pagination" aria-label={t('pagination.label')}>
      <span className="small muted">{t('pagination.showing', { from, to, total })}</span>
      <span className="spacer" />
      {onPerPage ? (
        <span className="pagination-size">
          <span className="small muted">{t('pagination.perPage')}</span>
          <Select
            size="sm"
            value={String(perPage)}
            ariaLabel={t('pagination.perPage')}
            onChange={(value) => onPerPage(Number(value))}
            options={perPageOptions.map((n) => ({ value: String(n), label: String(n) }))}
          />
        </span>
      ) : null}
      <span className="pagination-pages">
        <button type="button" className="page-btn flip" aria-label={t('members.previous')} disabled={page <= 1} onClick={() => onPage(page - 1)}>
          <Icon name="chevron" />
        </button>
        {pageList(page, last).map((n, i) =>
          n === null ? (
            <span key={`gap-${i}`} className="page-gap">
              …
            </span>
          ) : (
            <button key={n} type="button" className={`page-btn ${n === page ? 'active' : ''}`} aria-current={n === page ? 'page' : undefined} onClick={() => onPage(n)}>
              {n}
            </button>
          ),
        )}
        <button type="button" className="page-btn" aria-label={t('members.next')} disabled={page >= last} onClick={() => onPage(page + 1)}>
          <Icon name="chevron" />
        </button>
      </span>
    </nav>
  )
}

/** Client-side paging for lists the API returns whole (roles, webhooks). */
export function pageSlice<T>(items: T[], page: number, perPage: number): T[] {
  return items.slice((page - 1) * perPage, page * perPage)
}

/**
 * Cursor paging for feeds the API pages with next_cursor (activity,
 * notifications): it remembers the cursor of every page visited so Previous
 * goes back. `reset` starts over (a filter changed).
 */
export function useCursorPages(initialPerPage = 20) {
  const [cursors, setCursors] = useState<(string | undefined)[]>([undefined])
  const [perPage, setPerPageState] = useState(initialPerPage)
  const index = cursors.length - 1
  return {
    cursor: cursors[index],
    index,
    perPage,
    next: (cursor: string | null | undefined) => {
      if (cursor) setCursors((current) => [...current, cursor])
    },
    previous: () => setCursors((current) => (current.length > 1 ? current.slice(0, -1) : current)),
    reset: () => setCursors([undefined]),
    setPerPage: (size: number) => {
      setPerPageState(size)
      setCursors([undefined])
    },
  }
}

/** The footer of a cursor-paged feed: rows shown, rows per page, previous and next. */
export function CursorPagination({
  index,
  perPage,
  shown,
  total,
  hasNext,
  onPrevious,
  onNext,
  onPerPage,
}: {
  index: number
  perPage: number
  shown: number
  /** How many match in all, when the API says. */
  total?: number
  hasNext: boolean
  onPrevious: () => void
  onNext: () => void
  onPerPage?: (perPage: number) => void
}) {
  const t = useT()
  const from = shown === 0 ? 0 : index * perPage + 1
  const to = index * perPage + shown
  return (
    <nav className="pagination" aria-label={t('pagination.label')}>
      <span className="small muted">
        {total != null ? t('pagination.showing', { from, to, total }) : t('pagination.page', { page: index + 1 })}
      </span>
      <span className="spacer" />
      {onPerPage ? (
        <span className="pagination-size">
          <span className="small muted">{t('pagination.perPage')}</span>
          <Select
            size="sm"
            value={String(perPage)}
            ariaLabel={t('pagination.perPage')}
            onChange={(value) => onPerPage(Number(value))}
            options={perPageOptions.map((n) => ({ value: String(n), label: String(n) }))}
          />
        </span>
      ) : null}
      <span className="pagination-pages">
        <button type="button" className="page-btn flip" aria-label={t('members.previous')} disabled={index === 0} onClick={onPrevious}>
          <Icon name="chevron" />
        </button>
        <span className="page-btn active" aria-current="page">
          {index + 1}
        </span>
        <button type="button" className="page-btn" aria-label={t('members.next')} disabled={!hasNext} onClick={onNext}>
          <Icon name="chevron" />
        </button>
      </span>
    </nav>
  )
}
