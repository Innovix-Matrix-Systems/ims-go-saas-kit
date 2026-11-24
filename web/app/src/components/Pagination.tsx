import { useState } from 'react'
import { useT } from '../i18n'
import { Select } from './inputs'
import { Icon } from './ui'

export const perPageOptions = [10, 20, 50, 100] as const

/**
 * Page numbers to show: a window of five around the current page, with the
 * first and last pages and a gap (null) where pages are left out:
 * 1 2 3 4 5 … 77, 1 … 38 39 40 41 42 … 77.
 */
function pageList(page: number, last: number): (number | null)[] {
  const size = 5
  let start = Math.max(1, page - Math.floor(size / 2))
  const end = Math.min(last, start + size - 1)
  start = Math.max(1, end - size + 1)
  const out: (number | null)[] = []
  if (start > 1) out.push(1)
  if (start > 2) out.push(null)
  for (let n = start; n <= end; n++) out.push(n)
  if (end < last - 1) out.push(null)
  if (end < last) out.push(last)
  return out
}

function PerPage({ perPage, onPerPage }: { perPage: number; onPerPage: (perPage: number) => void }) {
  const t = useT()
  return (
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
  )
}

/**
 * The footer of every page-numbered list: which rows are shown, how many per
 * page, and first / previous / pages / next / last. Shown even when
 * everything fits on one page.
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
      {onPerPage ? <PerPage perPage={perPage} onPerPage={onPerPage} /> : null}
      <span className="pagination-pages">
        <button type="button" className="page-btn arrow" title={t('pagination.first')} aria-label={t('pagination.first')} disabled={page <= 1} onClick={() => onPage(1)}>
          <Icon name="first" />
        </button>
        <button type="button" className="page-btn arrow flip" title={t('members.previous')} aria-label={t('members.previous')} disabled={page <= 1} onClick={() => onPage(page - 1)}>
          <Icon name="chevron" />
        </button>
        {pageList(page, last).map((n, i) =>
          n === null ? (
            <span key={`gap-${i}`} className="page-gap" aria-hidden="true">
              …
            </span>
          ) : (
            <button key={n} type="button" className={`page-btn ${n === page ? 'active' : ''}`} aria-current={n === page ? 'page' : undefined} onClick={() => onPage(n)}>
              {n}
            </button>
          ),
        )}
        <button type="button" className="page-btn arrow" title={t('members.next')} aria-label={t('members.next')} disabled={page >= last} onClick={() => onPage(page + 1)}>
          <Icon name="chevron" />
        </button>
        <button type="button" className="page-btn arrow" title={t('pagination.last')} aria-label={t('pagination.last')} disabled={page >= last} onClick={() => onPage(last)}>
          <Icon name="last" />
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

/**
 * The footer of a cursor-paged feed (activity, notifications): rows shown,
 * rows per page, and only previous and next, since its pages have no numbers.
 */
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
      {onPerPage ? <PerPage perPage={perPage} onPerPage={onPerPage} /> : null}
      <span className="pagination-pages">
        <button type="button" className="page-btn labelled" disabled={index === 0} onClick={onPrevious}>
          <Icon name="chevron" className="flip-x" />
          {t('members.previous')}
        </button>
        <button type="button" className="page-btn labelled" disabled={!hasNext} onClick={onNext}>
          {t('members.next')}
          <Icon name="chevron" />
        </button>
      </span>
    </nav>
  )
}
