import { Link, useNavigate } from '@tanstack/react-router'
import { useEffect, useId, useMemo, useState, type ReactNode } from 'react'
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { intlLocale, useLocale, useT } from '../i18n'
import { Icon } from './ui'

// The dashboards' charts, drawn with Recharts. Colours come from the theme
// tokens (the accent for one series, --series-1… for parts of a whole), so
// they follow light and dark mode and the chosen accent. Every chart has a
// readout on hover or focus, and the figures are also in text (a legend, or
// a hidden table) so nothing is told by colour alone.

/** What a tooltip renderer gets: whether it shows, and the hovered datum. */
interface TipProps {
  active?: boolean
  payload?: ReadonlyArray<{ payload?: unknown }>
}

export interface ChartPoint {
  date: string
  value: number
}

/** Theme colours read from CSS, read again when the theme or accent changes. */
function useThemeColors() {
  const read = () => {
    const style = getComputedStyle(document.documentElement)
    const value = (name: string) => style.getPropertyValue(name).trim()
    return {
      accent: value('--accent'),
      grid: value('--border'),
      axis: value('--text-3'),
      surface: value('--surface-1'),
      other: value('--text-3'),
      series: [1, 2, 3, 4, 5, 6].map((n) => value(`--series-${n}`)),
    }
  }
  const [colors, setColors] = useState(read)
  useEffect(() => {
    const update = () => setColors(read())
    const observer = new MutationObserver(update)
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme', 'data-accent', 'class', 'style'] })
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    media.addEventListener('change', update)
    return () => {
      observer.disconnect()
      media.removeEventListener('change', update)
    }
  }, [])
  return colors
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

function Readout({ title, rows }: { title?: ReactNode; rows: { color: string; label: ReactNode; value: ReactNode }[] }) {
  return (
    <div className="chart-readout">
      {title ? <span className="chart-readout-date">{title}</span> : null}
      {rows.map((row, index) => (
        <span key={index} className="chart-readout-value">
          <i className="chart-swatch" style={{ background: row.color }} aria-hidden="true" />
          {row.label} <strong>{row.value}</strong>
        </span>
      ))}
    </div>
  )
}

/** A value per day as a smooth line over a soft area, with a crosshair readout. */
export function TrendChart({
  points,
  label,
  format = (value) => String(value),
  height = 240,
}: {
  points: ChartPoint[]
  /** What the series is: names the chart and the readout. */
  label: string
  format?: (value: number) => string
  height?: number
}) {
  const t = useT()
  const days = useDayFormat()
  const colors = useThemeColors()
  const gradient = `trend-${useId().replace(/:/g, '')}`
  const tooltip = ({ active, payload }: TipProps) => {
    if (!active || !payload?.length) return null
    const point = payload[0].payload as ChartPoint
    return <Readout title={days.long(point.date)} rows={[{ color: colors.accent, label, value: format(point.value) }]} />
  }
  return (
    <div className="chart" role="figure" aria-label={t('chart.summary', { label, count: points.length })}>
      <ResponsiveContainer width="100%" height={height}>
        <AreaChart data={points} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
          <defs>
            <linearGradient id={gradient} x1="0" x2="0" y1="0" y2="1">
              <stop offset="0%" stopColor={colors.accent} stopOpacity={0.28} />
              <stop offset="100%" stopColor={colors.accent} stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid vertical={false} stroke={colors.grid} strokeDasharray="3 4" />
          <XAxis
            dataKey="date"
            tickFormatter={days.short}
            tick={{ fill: colors.axis, fontSize: 11 }}
            tickLine={false}
            axisLine={{ stroke: colors.grid }}
            minTickGap={28}
          />
          <YAxis tickFormatter={format} tick={{ fill: colors.axis, fontSize: 11 }} tickLine={false} axisLine={false} width={48} allowDecimals={false} />
          <Tooltip content={(props) => tooltip(props as TipProps)} cursor={{ stroke: colors.axis, strokeDasharray: '3 3' }} />
          <Area
            type="monotone"
            dataKey="value"
            name={label}
            stroke={colors.accent}
            strokeWidth={2}
            fill={`url(#${gradient})`}
            activeDot={{ r: 5, stroke: colors.surface, strokeWidth: 2, fill: colors.accent }}
            animationDuration={700}
          />
        </AreaChart>
      </ResponsiveContainer>
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
export function Sparkline({ points, height = 40 }: { points: ChartPoint[]; height?: number }) {
  const colors = useThemeColors()
  const gradient = `spark-${useId().replace(/:/g, '')}`
  if (points.length < 2) return <div className="sparkline" />
  return (
    <div className="sparkline" aria-hidden="true">
      <ResponsiveContainer width="100%" height={height}>
        <AreaChart data={points} margin={{ top: 4, right: 0, bottom: 0, left: 0 }}>
          <defs>
            <linearGradient id={gradient} x1="0" x2="0" y1="0" y2="1">
              <stop offset="0%" stopColor={colors.accent} stopOpacity={0.25} />
              <stop offset="100%" stopColor={colors.accent} stopOpacity={0} />
            </linearGradient>
          </defs>
          <Area type="monotone" dataKey="value" stroke={colors.accent} strokeWidth={1.5} fill={`url(#${gradient})`} isAnimationActive={false} dot={false} />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  )
}

export interface Slice {
  key: string
  label: string
  value: number
  to?: string
  search?: Record<string, unknown>
}

/**
 * Parts of a whole as a donut, the total in the middle and a legend with
 * each part's figure and share. More than five parts fold the smallest into
 * "Other" so every colour stays distinct.
 */
export function DonutChart({
  slices,
  format = (value) => String(value),
  totalLabel,
  totalValue,
  empty,
  height = 200,
}: {
  slices: Slice[]
  format?: (value: number) => string
  totalLabel: string
  /** The figure in the middle; the sum when not given. */
  totalValue?: string
  empty?: ReactNode
  height?: number
}) {
  const t = useT()
  const colors = useThemeColors()
  const navigate = useNavigate()
  const locale = intlLocale(useLocale())
  const percent = new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 0 })
  const sorted = [...slices].filter((slice) => slice.value > 0).sort((a, b) => b.value - a.value)
  const shown = sorted.length > 5 ? [...sorted.slice(0, 4), { key: 'other', label: t('chart.other'), value: sorted.slice(4).reduce((sum, s) => sum + s.value, 0) }] : sorted
  const total = shown.reduce((sum, slice) => sum + slice.value, 0)
  const color = (index: number, key: string) => (key === 'other' ? colors.other : colors.series[index % colors.series.length])
  const [active, setActive] = useState<number | null>(null)

  if (total === 0) return <div className="small muted bar-list-empty">{empty}</div>

  const tooltip = ({ active: on, payload }: TipProps) => {
    if (!on || !payload?.length) return null
    const slice = payload[0].payload as Slice
    const index = shown.findIndex((s) => s.key === slice.key)
    return <Readout rows={[{ color: color(index, slice.key), label: slice.label, value: `${format(slice.value)} · ${percent.format(slice.value / total)}` }]} />
  }

  return (
    <div className="donut-box">
      <div className="donut">
        <div className="donut-chart" style={{ height }}>
          <ResponsiveContainer width="100%" height="100%">
            <PieChart>
              <Tooltip content={(props) => tooltip(props as TipProps)} />
              <Pie
                data={shown}
                dataKey="value"
                nameKey="label"
                innerRadius="64%"
                outerRadius="96%"
                paddingAngle={shown.length > 1 ? 2 : 0}
                cornerRadius={4}
                stroke={colors.surface}
                strokeWidth={2}
                animationDuration={700}
                onMouseEnter={(_, index) => setActive(index)}
                onMouseLeave={() => setActive(null)}
                onClick={(_, index) => {
                  const slice = shown[index] as Slice
                  if (slice?.to) void navigate({ to: slice.to, search: slice.search })
                }}
              >
                {shown.map((slice, index) => (
                  <Cell
                    key={slice.key}
                    fill={color(index, slice.key)}
                    opacity={active === null || active === index ? 1 : 0.45}
                    cursor={'to' in slice && slice.to ? 'pointer' : undefined}
                  />
                ))}
              </Pie>
            </PieChart>
          </ResponsiveContainer>
          <div className="donut-center" aria-hidden="true">
            <span className="donut-total">{totalValue ?? format(total)}</span>
            <span className="donut-total-label">{totalLabel}</span>
          </div>
        </div>
        <ul className="donut-legend">
          {shown.map((slice, index) => {
            const body = (
              <>
                <i className="chart-swatch" style={{ background: color(index, slice.key) }} aria-hidden="true" />
                <span className="donut-legend-label">{slice.label}</span>
                <span className="donut-legend-value">{format(slice.value)}</span>
                <span className="donut-legend-share">{percent.format(slice.value / total)}</span>
              </>
            )
            return (
              <li key={slice.key} onMouseEnter={() => setActive(index)} onMouseLeave={() => setActive(null)} className={active === index ? 'active' : ''}>
                {'to' in slice && slice.to ? (
                  <Link to={slice.to} search={slice.search} className="donut-legend-row">
                    {body}
                  </Link>
                ) : (
                  <span className="donut-legend-row">{body}</span>
                )}
              </li>
            )
          })}
        </ul>
      </div>
    </div>
  )
}

export interface BarItem {
  key: string
  label: string
  value: number
  to?: string
  search?: Record<string, unknown>
}

/** Ranked horizontal bars, largest first, one hue; a bar with `to` opens it. */
export function RankedBarChart({ items, format = (value) => String(value), label, empty }: { items: BarItem[]; format?: (value: number) => string; label: string; empty?: ReactNode }) {
  const colors = useThemeColors()
  const navigate = useNavigate()
  if (items.length === 0) return <div className="small muted bar-list-empty">{empty}</div>
  const data = [...items].sort((a, b) => b.value - a.value)
  const height = data.length * 36 + 8
  const tooltip = ({ active, payload }: TipProps) => {
    if (!active || !payload?.length) return null
    const item = payload[0].payload as BarItem
    return <Readout rows={[{ color: colors.accent, label: item.label, value: format(item.value) }]} />
  }
  return (
    <div className="ranked-chart" role="figure" aria-label={label}>
      <ResponsiveContainer width="100%" height={height}>
        <BarChart data={data} layout="vertical" margin={{ top: 0, right: 40, bottom: 0, left: 0 }} barCategoryGap={8}>
          <XAxis type="number" hide domain={[0, 'dataMax']} />
          <YAxis
            type="category"
            dataKey="label"
            width={150}
            tick={{ fill: colors.axis, fontSize: 12 }}
            tickLine={false}
            axisLine={false}
            tickFormatter={(value: string) => (value.length > 22 ? `${value.slice(0, 21)}…` : value)}
          />
          <Tooltip content={(props) => tooltip(props as TipProps)} cursor={{ fill: colors.grid, opacity: 0.4 }} />
          <Bar
            dataKey="value"
            name={label}
            fill={colors.accent}
            radius={[0, 4, 4, 0]}
            maxBarSize={22}
            animationDuration={600}
            label={{ position: 'right', fill: colors.axis, fontSize: 12, formatter: (value: unknown) => format(Number(value)) }}
            onClick={(item: { payload?: BarItem }) => {
              if (item.payload?.to) void navigate({ to: item.payload.to, search: item.payload.search })
            }}
            cursor={data.some((item) => item.to) ? 'pointer' : undefined}
          />
        </BarChart>
      </ResponsiveContainer>
      <table className="sr-only">
        <caption>{label}</caption>
        <tbody>
          {data.map((item) => (
            <tr key={item.key}>
              <th scope="row">{item.label}</th>
              <td>{format(item.value)}</td>
            </tr>
          ))}
        </tbody>
      </table>
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
