import type { ReactNode } from 'react'
import { useT } from '../i18n'

// Meters are not charts: they stay out of the charting library so the top
// bar's plan peek does not load it on every page.

export interface MeterItem {
  key: string
  label: ReactNode
  used: number
  /** -1 unlimited. */
  limit: number
  format?: (value: number) => string
}

/** Use against a limit, one row each: name, bar and figure, bars aligned. */
export function MeterList({ items }: { items: MeterItem[] }) {
  const t = useT()
  return (
    <ul className="meter-list">
      {items.map((item) => {
        const format = item.format ?? ((value: number) => String(value))
        const unlimited = item.limit < 0
        const share = unlimited || item.limit === 0 ? 0 : Math.min(1, item.used / item.limit)
        const tone = share >= 1 ? 'danger' : share >= 0.8 ? 'warn' : ''
        return (
          <li key={item.key}>
            <span className="meter-list-label">{item.label}</span>
            <span className="meter-list-figure">
              {format(item.used)}
              <span className="muted"> / {unlimited ? t('dashboard.unlimited') : format(item.limit)}</span>
            </span>
            <span className="meter" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(share * 100)}>
              <span className={`meter-fill ${tone}`} style={{ width: unlimited ? '0%' : `${Math.max(share > 0 ? 3 : 0, share * 100)}%` }} />
            </span>
          </li>
        )
      })}
    </ul>
  )
}
