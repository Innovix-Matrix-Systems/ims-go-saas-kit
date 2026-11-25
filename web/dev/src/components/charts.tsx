import { useState, type ReactNode } from 'react'
import { Bar, BarChart, CartesianGrid, Cell, Pie, PieChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { pointerIndex } from './TrafficChart'

// The dashboard's chart kit, drawn with Recharts. Colours are CSS classes
// on the design tokens (tone-*), so every chart follows light/dark mode and
// the accent with no JavaScript; the figures are always in text too (a
// legend, a readout or a tooltip), so nothing is told by colour alone.

/** A colour: a categorical series (parts of a whole) or a meaning. */
export type Tone =
  | 'series-1'
  | 'series-2'
  | 'series-3'
  | 'series-4'
  | 'series-5'
  | 'series-6'
  | 'accent'
  | 'ok'
  | 'warn'
  | 'danger'
  | 'muted'

/** The categorical palette in order, for slices with no meaning of their own. */
export const seriesTones: Tone[] = ['series-1', 'series-2', 'series-3', 'series-4', 'series-5', 'series-6']

const plain = (value: number) => value.toLocaleString()

/** What a Recharts tooltip renderer receives. */
interface TipProps {
  active?: boolean
  payload?: ReadonlyArray<{ payload?: unknown }>
}

/** A tooltip body on the dashboard surface. */
function ChartTip({ title, rows }: { title?: ReactNode; rows: { tone: Tone; label: ReactNode; value: ReactNode }[] }) {
  return (
    <div className="chart-tip">
      {title ? <div className="chart-tip-title">{title}</div> : null}
      {rows.map((row, index) => (
        <div key={index} className="chart-tip-row">
          <i className={`swatch tone-${row.tone}`} aria-hidden="true" />
          <span>{row.label}</span>
          <strong>{row.value}</strong>
        </div>
      ))}
    </div>
  )
}

export interface Slice {
  key: string
  label: string
  value: number
  tone?: Tone
}

/**
 * Parts of a whole as a donut (a pie with the total in the middle) and a
 * legend with each part's figure and share. More than six parts fold the
 * smallest into "Other" so every colour stays distinct.
 */
export function DonutChart({
  slices,
  label,
  format = plain,
  totalLabel = 'total',
  onSelect,
}: {
  slices: Slice[]
  /** What the chart shows, for screen readers. */
  label: string
  format?: (value: number) => string
  totalLabel?: string
  /** Makes legend rows buttons (for example, to filter a list). */
  onSelect?: (key: string) => void
}) {
  const parts = fold(slices.filter((slice) => slice.value > 0))
  const total = parts.reduce((sum, slice) => sum + slice.value, 0)
  const share = (value: number) => (total > 0 ? `${Math.round((value / total) * 100)}%` : '0%')
  const tooltip = ({ active, payload }: TipProps) => {
    if (!active || !payload?.length) return null
    const slice = payload[0].payload as Slice & { tone: Tone }
    return <ChartTip rows={[{ tone: slice.tone, label: slice.label, value: `${format(slice.value)} · ${share(slice.value)}` }]} />
  }

  return (
    <div className="donut" role="figure" aria-label={label}>
      <div className="donut-plot">
        <ResponsiveContainer width="100%" height="100%">
          <PieChart>
            {total === 0 ? (
              <Pie data={[{ value: 1 }]} dataKey="value" innerRadius="70%" outerRadius="100%" isAnimationActive={false} stroke="none">
                <Cell className="tone-empty" />
              </Pie>
            ) : (
              <Pie
                data={parts}
                dataKey="value"
                nameKey="label"
                innerRadius="70%"
                outerRadius="100%"
                paddingAngle={parts.length > 1 ? 1.5 : 0}
                startAngle={90}
                endAngle={-270}
                stroke="none"
                isAnimationActive={false}
              >
                {parts.map((slice) => (
                  <Cell key={slice.key} className={`tone-${slice.tone}`} />
                ))}
              </Pie>
            )}
            {total > 0 ? <Tooltip content={(props) => tooltip(props as TipProps)} isAnimationActive={false} /> : null}
          </PieChart>
        </ResponsiveContainer>
        <div className="donut-total" aria-hidden="true">
          <strong>{format(total)}</strong>
          <span>{totalLabel}</span>
        </div>
      </div>
      <ul className="chart-legend">
        {parts.length === 0 ? <li className="dim small">Nothing in this window.</li> : null}
        {parts.map((slice) => {
          const body = (
            <>
              <i className={`swatch tone-${slice.tone}`} aria-hidden="true" />
              <span className="chart-legend-label">{slice.label}</span>
              <span className="chart-legend-value">{format(slice.value)}</span>
              <span className="chart-legend-share">{share(slice.value)}</span>
            </>
          )
          return (
            <li key={slice.key}>
              {onSelect && slice.key !== 'other' ? (
                <button type="button" className="chart-legend-row" onClick={() => onSelect(slice.key)}>
                  {body}
                </button>
              ) : (
                <div className="chart-legend-row">{body}</div>
              )}
            </li>
          )
        })}
      </ul>
    </div>
  )
}

/** Keeps the five largest parts and folds the rest into "Other"; gives every part a tone. */
function fold(slices: Slice[]): (Slice & { tone: Tone })[] {
  const sorted = [...slices].sort((a, b) => b.value - a.value)
  const kept = sorted.length > 6 ? sorted.slice(0, 5) : sorted
  const rest = sorted.length > 6 ? sorted.slice(5) : []
  const out = kept.map((slice, index) => ({ ...slice, tone: slice.tone ?? seriesTones[index] }))
  if (rest.length > 0) {
    out.push({ key: 'other', label: `Other (${rest.length})`, value: rest.reduce((sum, s) => sum + s.value, 0), tone: 'muted' })
  }
  return out
}

export interface BarPart {
  key: string
  label: string
  tone: Tone
}

export interface BarRow {
  key: string
  /** The axis label (shortened when long). */
  label: string
  /** The full label, shown in the tooltip. */
  title?: string
  values: Record<string, number>
}

/**
 * One horizontal bar per row, stacked from parts (for example successful
 * and failed requests per route), with the row's label on the left and a
 * tooltip with every part.
 */
export function HBarChart({
  rows,
  parts,
  label,
  format = plain,
  labelWidth = 220,
}: {
  rows: BarRow[]
  parts: BarPart[]
  label: string
  format?: (value: number) => string
  labelWidth?: number
}) {
  if (rows.length === 0) return <div className="chart-empty">Nothing in this window.</div>
  const data = rows.map((row) => ({ ...row.values, __label: row.label, __title: row.title ?? row.label }))
  const tooltip = ({ active, payload }: TipProps) => {
    if (!active || !payload?.length) return null
    const datum = payload[0].payload as Record<string, number | string>
    return (
      <ChartTip
        title={<span className="mono">{String(datum.__title)}</span>}
        rows={parts
          .filter((part) => Number(datum[part.key] ?? 0) > 0 || parts.length === 1)
          .map((part) => ({ tone: part.tone, label: part.label, value: format(Number(datum[part.key] ?? 0)) }))}
      />
    )
  }
  const chars = Math.floor(labelWidth / 7)
  return (
    <div className="hbar-chart" role="figure" aria-label={label}>
      {parts.length > 1 ? <PartLegend parts={parts} /> : null}
      <ResponsiveContainer width="100%" height={rows.length * 30 + 24}>
        <BarChart data={data} layout="vertical" margin={{ top: 0, right: 12, bottom: 0, left: 0 }} barCategoryGap={6}>
          <CartesianGrid horizontal={false} />
          <XAxis type="number" tickFormatter={(value: number) => format(value)} tickLine={false} axisLine={false} allowDecimals={false} />
          <YAxis
            type="category"
            dataKey="__label"
            width={labelWidth}
            tickLine={false}
            axisLine={false}
            interval={0}
            tickFormatter={(value: string) => (value.length > chars ? `${value.slice(0, chars - 1)}…` : value)}
          />
          <Tooltip content={(props) => tooltip(props as TipProps)} isAnimationActive={false} />
          {parts.map((part) => (
            <Bar key={part.key} dataKey={part.key} stackId="row" className={`tone-${part.tone}`} isAnimationActive={false} />
          ))}
        </BarChart>
      </ResponsiveContainer>
    </div>
  )
}

export interface TimePoint {
  /** ISO time of the bucket. */
  at: string
  values: Record<string, number>
}

/**
 * Stacked bars over time (one bar per hour or day) with a legend and a
 * readout of the bucket under the pointer, like the Overview traffic chart.
 */
export function TimeBars({
  points,
  parts,
  label,
  tick,
  title,
  format = plain,
  height = 180,
}: {
  points: TimePoint[]
  parts: BarPart[]
  label: string
  /** The axis label of a bucket. */
  tick: (at: string) => string
  /** The readout's heading for a bucket. */
  title: (at: string) => string
  format?: (value: number) => string
  height?: number
}) {
  const [hover, setHover] = useState<number | null>(null)
  const data = points.map((point, index) => {
    const row: Record<string, number | null> = { __index: index }
    for (const part of parts) {
      const value = point.values[part.key] ?? 0
      row[part.key] = value > 0 ? value : null
    }
    return row
  })
  const step = Math.max(1, Math.ceil(points.length / 6))
  const ticks = points.map((_, index) => index).filter((index) => index % step === 0 || index === points.length - 1)
  const point = hover === null ? null : points[hover]
  const readout = point
    ? `${title(point.at)} · ${parts.map((part) => `${format(point.values[part.key] ?? 0)} ${part.label.toLowerCase()}`).join(' · ')}`
    : ' '

  return (
    <div className="time-bars" role="figure" aria-label={label}>
      <div className="chart-head">
        <PartLegend parts={parts} />
        <span className="chart-readout">{readout}</span>
      </div>
      <ResponsiveContainer width="100%" height={height}>
        <BarChart
          data={data}
          margin={{ top: 8, right: 14, bottom: 0, left: 0 }}
          barCategoryGap={2}
          onMouseMove={(state) => setHover(pointerIndex(state))}
          onMouseLeave={() => setHover(null)}
        >
          <CartesianGrid vertical={false} />
          <XAxis
            dataKey="__index"
            ticks={ticks}
            interval={0}
            tickFormatter={(index: number) => (points[index] ? tick(points[index].at) : '')}
            tickLine={false}
            axisLine={false}
          />
          <YAxis tickLine={false} axisLine={false} width={34} allowDecimals={false} tickFormatter={(value: number) => format(value)} />
          <Tooltip content={() => null} isAnimationActive={false} />
          {parts.map((part) => (
            <Bar key={part.key} dataKey={part.key} stackId="time" className={`tone-${part.tone}`} maxBarSize={44} isAnimationActive={false} />
          ))}
        </BarChart>
      </ResponsiveContainer>
    </div>
  )
}

function PartLegend({ parts }: { parts: BarPart[] }) {
  return (
    <span className="chart-parts" aria-hidden="true">
      {parts.map((part) => (
        <span key={part.key}>
          <i className={`swatch tone-${part.tone}`} /> {part.label}
        </span>
      ))}
    </span>
  )
}
