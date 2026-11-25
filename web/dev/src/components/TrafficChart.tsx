import { useState } from 'react'
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import type { DevTrafficMinute } from '../api/types'
import { formatDuration } from '../format'

/** What Recharts hands a mouse handler: the index under the pointer, if any. */
interface ChartPointer {
  activeTooltipIndex?: number | string | null
}

/** The hovered index from a Recharts mouse event, or null. */
export function pointerIndex(state: ChartPointer | null | undefined): number | null {
  const raw = state?.activeTooltipIndex
  if (raw === null || raw === undefined || raw === '') return null
  const index = Number(raw)
  return Number.isInteger(index) && index >= 0 ? index : null
}

interface TrafficDatum {
  index: number
  ok: number | null
  client: number | null
  server: number | null
}

/**
 * Requests per minute as stacked bars (Recharts): 2xx/3xx in the accent,
 * 4xx amber, 5xx red, a recessive grid, time labels, a legend and a text
 * readout for the minute under the pointer. Empty minutes are gaps, not
 * zero-height bars. Colours come from the theme tokens in overview.css, so
 * they follow light/dark mode and the accent.
 */
export function TrafficChart({ slots, end }: { slots: (DevTrafficMinute | null)[]; end: Date }) {
  const [hover, setHover] = useState<number | null>(null)
  const count = slots.length
  const max = Math.max(1, ...slots.map((slot) => slot?.requests ?? 0))
  const scale = niceMax(max)
  const minuteAt = (index: number) => new Date(end.getTime() - (count - 1 - index) * 60_000)
  const label = (date: Date) => date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })

  const data: TrafficDatum[] = slots.map((slot, index) => {
    if (!slot || slot.requests === 0) return { index, ok: null, client: null, server: null }
    const errors = slot.client_errors + slot.server_errors
    return { index, ok: slot.requests - errors, client: slot.client_errors, server: slot.server_errors }
  })
  const ticks = [0, 15, 30, 45, 59].filter((index) => index < count)
  const point = hover === null ? null : slots[hover]

  return (
    <div className="traffic-chart" role="img" aria-label="Requests per minute, last hour">
      <div className="traffic-legend" aria-hidden="true">
        <span><i className="swatch ok-requests" /> 2xx and 3xx</span>
        <span><i className="swatch client" /> 4xx</span>
        <span><i className="swatch server" /> 5xx</span>
        <span className="traffic-readout">
          {hover !== null
            ? `${label(minuteAt(hover))} · ${point ? `${point.requests} requests · ${point.client_errors} 4xx · ${point.server_errors} 5xx · p95 ${formatDuration(point.p95_ms)}` : 'no requests'}`
            : ' '}
        </span>
      </div>
      <ResponsiveContainer width="100%" height={200}>
        <BarChart
          data={data}
          margin={{ top: 8, right: 14, bottom: 0, left: 0 }}
          barCategoryGap={2}
          onMouseMove={(state) => setHover(pointerIndex(state as ChartPointer))}
          onMouseLeave={() => setHover(null)}
        >
          <CartesianGrid vertical={false} />
          <XAxis
            dataKey="index"
            ticks={ticks}
            interval={0}
            tickFormatter={(index: number) => (index === count - 1 ? 'now' : label(minuteAt(index)))}
            tickLine={false}
            axisLine={false}
          />
          <YAxis
            domain={[0, scale]}
            ticks={[0, scale / 2, scale]}
            // The midpoint of 1, 5, 10… is 0.5, 2.5, 5: show the half, not a rounded 3.
            tickFormatter={(value: number) => (Number.isInteger(value) ? String(value) : value.toFixed(1))}
            tickLine={false}
            axisLine={false}
            width={34}
          />
          {/* The readout is the legend line above, so the tooltip draws only the column highlight. */}
          <Tooltip content={() => null} isAnimationActive={false} />
          <Bar dataKey="ok" stackId="requests" className="ok-requests" isAnimationActive={false} />
          <Bar dataKey="client" stackId="requests" className="client" isAnimationActive={false} />
          <Bar dataKey="server" stackId="requests" className="server" isAnimationActive={false} />
        </BarChart>
      </ResponsiveContainer>
    </div>
  )
}

/** A round axis maximum: 1, 2, 5 × 10^n at or above value. */
export function niceMax(value: number): number {
  const power = 10 ** Math.floor(Math.log10(value))
  for (const factor of [1, 2, 5, 10]) {
    if (factor * power >= value) return factor * power
  }
  return 10 * power
}
