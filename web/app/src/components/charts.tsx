import { useEffect, useId, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent, type ReactNode } from 'react'
import { Link } from '@tanstack/react-router'
import { intlLocale, useLocale, useT } from '../i18n'
import { Icon } from './ui'

// Small hand-drawn SVG charts for the dashboards. One series each, in the
// accent hue (the series is the point; a ranked list is one hue too), with a
// recessive grid, a crosshair readout on hover and arrow keys, and a hidden
// table for screen readers.

export interface ChartPoint {
  date: string
  value: number
}

/** Width of an element, followed as it resizes. */
function useWidth<T extends HTMLElement>(fallback = 600) {
  const ref = useRef<T>(null)
  const [width, setWidth] = useState(fallback)
  useEffect(() => {
    const element = ref.current
    if (!element) return
    const observer = new ResizeObserver(([entry]) => setWidth(Math.max(160, Math.round(entry.contentRect.width))))
    observer.observe(element)
    return () => observer.disconnect()
  }, [])
  return [ref, width] as const
}

/** A round axis maximum: 1, 2, 2.5, 5 × 10^n at or above value. */
export function niceMax(value: number): number {
  if (value <= 0) return 1
  const power = 10 ** Math.floor(Math.log10(value))
  for (const factor of [1, 2, 2.5, 5, 10]) {
    if (factor * power >= value) return factor * power
  }
  return 10 * power
}

/**
 * A smooth path through the points that never overshoots them (monotone
 * cubic interpolation), so a flat stretch stays flat and zero stays zero.
 */
function smoothPath(points: [number, number][]): string {
  const n = points.length
  if (n === 0) return ''
  if (n === 1) return `M${points[0][0]},${points[0][1]}`
  const dx: number[] = []
  const slope: number[] = []
  for (let i = 0; i < n - 1; i++) {
    dx.push(points[i + 1][0] - points[i][0])
    slope.push((points[i + 1][1] - points[i][1]) / (dx[i] || 1))
  }
  const tangent: number[] = [slope[0]]
  for (let i = 1; i < n - 1; i++) {
    tangent.push(slope[i - 1] * slope[i] <= 0 ? 0 : (slope[i - 1] + slope[i]) / 2)
  }
  tangent.push(slope[n - 2])
  for (let i = 0; i < n - 1; i++) {
    if (slope[i] === 0) {
      tangent[i] = 0
      tangent[i + 1] = 0
      continue
    }
    const a = tangent[i] / slope[i]
    const b = tangent[i + 1] / slope[i]
    const h = a * a + b * b
    if (h > 9) {
      const t = 3 / Math.sqrt(h)
      tangent[i] = t * a * slope[i]
      tangent[i + 1] = t * b * slope[i]
    }
  }
  let d = `M${points[0][0]},${points[0][1]}`
  for (let i = 0; i < n - 1; i++) {
    const [x0, y0] = points[i]
    const [x1, y1] = points[i + 1]
    const h = dx[i] / 3
    d += ` C${x0 + h},${y0 + tangent[i] * h} ${x1 - h},${y1 - tangent[i + 1] * h} ${x1},${y1}`
  }
  return d
}

function useDayFormat() {
  const locale = intlLocale(useLocale())
  return useMemo(() => {
    const short = new Intl.DateTimeFormat(locale, { month: 'short', day: 'numeric', timeZone: 'UTC' })
    const long = new Intl.DateTimeFormat(locale, { weekday: 'short', month: 'short', day: 'numeric', year: 'numeric', timeZone: 'UTC' })
    // Dates are calendar days ("2026-03-05"), read at UTC midnight so no
    // time zone moves them.
    const parse = (date: string) => new Date(`${date}T00:00:00Z`)
    return { short: (date: string) => short.format(parse(date)), long: (date: string) => long.format(parse(date)) }
  }, [locale])
}

/**
 * A value per day as a smooth line over a soft area, with a crosshair and a
 * readout of the day under the pointer (or the arrow keys).
 */
export function AreaChart({
  points,
  label,
  format = (value) => String(value),
  height = 220,
}: {
  points: ChartPoint[]
  /** What the series is: names the chart for screen readers and the readout. */
  label: string
  format?: (value: number) => string
  height?: number
}) {
  const t = useT()
  const days = useDayFormat()
  const gradient = useId()
  const [box, width] = useWidth<HTMLDivElement>()
  const [hover, setHover] = useState<number | null>(null)
  const top = 12
  const bottom = 26
  const left = 44
  const right = 8
  const plot = height - top - bottom
  const count = points.length
  const max = niceMax(Math.max(0, ...points.map((point) => point.value)))
  const x = (index: number) => left + (count <= 1 ? 0 : (index / (count - 1)) * (width - left - right))
  const y = (value: number) => top + plot - (value / max) * plot
  const coords = points.map((point, index) => [x(index), y(point.value)] as [number, number])
  const line = smoothPath(coords)
  const area = count > 1 ? `${line} L${x(count - 1)},${top + plot} L${x(0)},${top + plot} Z` : ''
  const ticks = [0, 0.5, 1]
  // A few evenly spaced day labels, the last always today.
  const labelEvery = Math.max(1, Math.ceil(count / Math.max(2, Math.floor((width - left) / 90))))
  const dayLabels = points.map((_, index) => index).filter((index) => index === count - 1 || (index % labelEvery === 0 && count - 1 - index >= labelEvery / 2))

  const pick = (event: PointerEvent<SVGRectElement>) => {
    const rect = event.currentTarget.getBoundingClientRect()
    const ratio = (event.clientX - rect.left) / rect.width
    setHover(Math.min(count - 1, Math.max(0, Math.round(ratio * (count - 1)))))
  }
  const keys = (event: KeyboardEvent<SVGSVGElement>) => {
    if (event.key === 'ArrowRight' || event.key === 'ArrowLeft') {
      event.preventDefault()
      const step = event.key === 'ArrowRight' ? 1 : -1
      setHover((current) => Math.min(count - 1, Math.max(0, (current ?? (step > 0 ? -1 : count)) + step)))
    } else if (event.key === 'Escape') {
      setHover(null)
    }
  }
  const active = hover === null ? null : points[hover]
  const readoutLeft = hover === null ? 0 : Math.min(Math.max(x(hover), 70), width - 70)

  return (
    <div className="chart" ref={box}>
      <svg
        className="chart-svg"
        viewBox={`0 0 ${width} ${height}`}
        width={width}
        height={height}
        role="img"
        aria-label={t('chart.summary', { label, days: count })}
        tabIndex={0}
        onKeyDown={keys}
        onBlur={() => setHover(null)}
      >
        <defs>
          <linearGradient id={gradient} x1="0" x2="0" y1="0" y2="1">
            <stop offset="0%" className="chart-area-top" />
            <stop offset="100%" className="chart-area-bottom" />
          </linearGradient>
        </defs>
        {ticks.map((share) => (
          <g key={share}>
            <line className={share === 0 ? 'chart-baseline' : 'chart-grid'} x1={left} x2={width - right} y1={y(max * share)} y2={y(max * share)} />
            <text className="chart-axis" x={left - 8} y={y(max * share) + 4} textAnchor="end">
              {format(max * share)}
            </text>
          </g>
        ))}
        {dayLabels.map((index) => (
          <text key={index} className="chart-axis" x={x(index)} y={height - 6} textAnchor={index === 0 ? 'start' : index === count - 1 ? 'end' : 'middle'}>
            {days.short(points[index].date)}
          </text>
        ))}
        {area ? <path className="chart-area" d={area} fill={`url(#${gradient})`} /> : null}
        <path className="chart-line" d={line} pathLength={1} />
        {hover !== null ? (
          <g className="chart-cursor">
            <line x1={x(hover)} x2={x(hover)} y1={top} y2={top + plot} />
            <circle cx={x(hover)} cy={y(points[hover].value)} r={5} />
          </g>
        ) : null}
        <rect className="chart-hit" x={left} y={0} width={Math.max(0, width - left - right)} height={top + plot} onPointerMove={pick} onPointerLeave={() => setHover(null)} />
      </svg>
      {active ? (
        <div className="chart-readout" style={{ left: readoutLeft }} role="status">
          <span className="chart-readout-date">{days.long(active.date)}</span>
          <span className="chart-readout-value">
            <i className="chart-swatch" aria-hidden="true" />
            {label} <strong>{format(active.value)}</strong>
          </span>
        </div>
      ) : null}
      <table className="sr-only">
        <caption>{label}</caption>
        <tbody>
          {points.map((point) => (
            <tr key={point.date}>
              <th scope="row">{days.long(point.date)}</th>
              <td>{format(point.value)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

/** A tiny trend line for a stat card; decoration beside the number it summarizes. */
export function Sparkline({ points, height = 36 }: { points: ChartPoint[]; height?: number }) {
  const [box, width] = useWidth<HTMLDivElement>(120)
  if (points.length < 2) return <div className="sparkline" ref={box} />
  const max = Math.max(1, ...points.map((point) => point.value))
  const pad = 3
  const coords = points.map(
    (point, index) => [(index / (points.length - 1)) * width, pad + (height - pad * 2) * (1 - point.value / max)] as [number, number],
  )
  const line = smoothPath(coords)
  return (
    <div className="sparkline" ref={box} aria-hidden="true">
      <svg viewBox={`0 0 ${width} ${height}`} width={width} height={height}>
        <path className="sparkline-area" d={`${line} L${width},${height} L0,${height} Z`} />
        <path className="sparkline-line" d={line} />
      </svg>
    </div>
  )
}

/** "+12%" / "−5%" against the previous period, or "new" from nothing. */
export function Delta({ current, previous, invert }: { current: number; previous: number; invert?: boolean }) {
  const t = useT()
  const locale = intlLocale(useLocale())
  if (current === previous) {
    return <span className="delta flat">{t('dashboard.noChange')}</span>
  }
  const up = current > previous
  const good = invert ? !up : up
  const text =
    previous === 0
      ? t('dashboard.new')
      : new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 0, signDisplay: 'always' }).format((current - previous) / previous)
  return (
    <span className={`delta ${good ? 'good' : 'bad'}`} title={t('dashboard.vsPrevious')}>
      <Icon name="chevron" className={up ? 'delta-up' : 'delta-down'} />
      {text}
    </span>
  )
}

/** A headline number with its change and a sparkline; a link when `to` is set. */
export function KpiCard({
  label,
  value,
  sub,
  delta,
  points,
  to,
  search,
}: {
  label: string
  value: ReactNode
  sub?: ReactNode
  delta?: ReactNode
  points?: ChartPoint[]
  to?: string
  search?: Record<string, unknown>
}) {
  const body = (
    <>
      <div className="kpi-head">
        <span className="kpi-label">{label}</span>
        {to ? <Icon name="chevron" className="kpi-go" /> : null}
      </div>
      <div className="kpi-value">{value}</div>
      <div className="kpi-foot">
        {delta}
        {sub ? <span className="kpi-sub">{sub}</span> : null}
      </div>
      {points ? <Sparkline points={points} /> : null}
    </>
  )
  return to ? (
    <Link to={to} search={search} className="kpi-card linked">
      {body}
    </Link>
  ) : (
    <div className="kpi-card">{body}</div>
  )
}

export interface BarItem {
  key: string
  label: ReactNode
  value: number
  to?: string
  search?: Record<string, unknown>
}

/** Ranked horizontal bars on one baseline, largest first, one hue. */
export function BarList({ items, format = (value) => String(value), empty }: { items: BarItem[]; format?: (value: number) => string; empty?: ReactNode }) {
  if (items.length === 0) return <div className="small muted bar-list-empty">{empty}</div>
  const max = Math.max(1, ...items.map((item) => item.value))
  return (
    <ul className="bar-list">
      {items.map((item) => {
        const row = (
          <>
            <span className="bar-list-track" aria-hidden="true">
              <span className="bar-list-fill" style={{ width: `${Math.max(2, (item.value / max) * 100)}%` }} />
            </span>
            <span className="bar-list-label">{item.label}</span>
            <span className="bar-list-value">{format(item.value)}</span>
          </>
        )
        return (
          <li key={item.key}>
            {item.to ? (
              <Link to={item.to} search={item.search} className="bar-list-row linked">
                {row}
              </Link>
            ) : (
              <div className="bar-list-row">{row}</div>
            )}
          </li>
        )
      })}
    </ul>
  )
}

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
