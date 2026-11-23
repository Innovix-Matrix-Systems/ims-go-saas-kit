import { useEffect, useRef, useState, type MouseEvent } from 'react'
import type { DevTrafficMinute } from '../api/types'
import { formatDuration } from '../format'

/**
 * Requests per minute as stacked bars (hand-drawn SVG): 2xx/3xx in the
 * accent, 4xx amber, 5xx red, with a 2 px gap between bars, a recessive
 * grid, time labels, a legend and a hover readout for the minute under the
 * pointer. Empty minutes are gaps, not zero-height bars.
 */
export function TrafficChart({ slots, end }: { slots: (DevTrafficMinute | null)[]; end: Date }) {
  const [hover, setHover] = useState<number | null>(null)
  // Drawn at the element's own width, so text and bars are never stretched.
  const box = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(1000)
  useEffect(() => {
    const element = box.current
    if (!element) return
    const observer = new ResizeObserver(([entry]) => setWidth(Math.max(320, Math.round(entry.contentRect.width))))
    observer.observe(element)
    return () => observer.disconnect()
  }, [])
  const height = 200
  const top = 8
  const bottom = 22
  const left = 34
  const plot = height - top - bottom
  const count = slots.length
  const step = (width - left) / Math.max(count, 1)
  const max = Math.max(1, ...slots.map((slot) => slot?.requests ?? 0))
  const scale = niceMax(max)
  const y = (value: number) => top + plot - (value / scale) * plot
  const minuteAt = (index: number) => new Date(end.getTime() - (count - 1 - index) * 60_000)
  const label = (date: Date) => date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })

  const onMove = (event: MouseEvent<SVGRectElement>) => {
    const box = event.currentTarget.getBoundingClientRect()
    const ratio = (event.clientX - box.left) / box.width
    setHover(Math.min(count - 1, Math.max(0, Math.floor(ratio * count))))
  }
  const point = hover === null ? null : slots[hover]

  return (
    <div className="traffic-chart" ref={box}>
      <div className="traffic-legend" aria-hidden="true">
        <span><i className="swatch ok-requests" /> 2xx and 3xx</span>
        <span><i className="swatch client" /> 4xx</span>
        <span><i className="swatch server" /> 5xx</span>
        <span className="traffic-readout">
          {hover !== null
            ? `${label(minuteAt(hover))} · ${point ? `${point.requests} requests · ${point.client_errors} 4xx · ${point.server_errors} 5xx · p95 ${formatDuration(point.p95_ms)}` : 'no requests'}`
            : ' '}
        </span>
      </div>
      <svg viewBox={`0 0 ${width} ${height}`} role="img" aria-label="Requests per minute, last hour">
        {[0, 0.5, 1].map((share) => (
          <g key={share}>
            <line className="chart-grid" x1={left} x2={width} y1={y(scale * share)} y2={y(scale * share)} />
            <text className="chart-axis" x={left - 6} y={y(scale * share) + 4} textAnchor="end">
              {Math.round(scale * share)}
            </text>
          </g>
        ))}
        {slots.map((slot, index) => {
          if (!slot || slot.requests === 0) return null
          const x = left + index * step + 1
          const w = Math.max(step - 2, 1)
          const errors = slot.client_errors + slot.server_errors
          const okHeight = ((slot.requests - errors) / scale) * plot
          const clientHeight = (slot.client_errors / scale) * plot
          const serverHeight = (slot.server_errors / scale) * plot
          const base = top + plot
          return (
            <g key={index} className={hover === index ? 'bar active' : 'bar'}>
              <rect className="ok-requests" x={x} width={w} y={base - okHeight} height={okHeight} />
              <rect className="client" x={x} width={w} y={base - okHeight - clientHeight} height={clientHeight} />
              <rect className="server" x={x} width={w} y={base - okHeight - clientHeight - serverHeight} height={serverHeight} />
            </g>
          )
        })}
        {[0, 15, 30, 45, 59].map((index) =>
          index < count ? (
            <text key={index} className="chart-axis" x={left + index * step + step / 2} y={height - 6} textAnchor="middle">
              {index === count - 1 ? 'now' : label(minuteAt(index))}
            </text>
          ) : null,
        )}
        <rect className="chart-hit" x={left} y={top} width={width - left} height={plot} onMouseMove={onMove} onMouseLeave={() => setHover(null)} />
      </svg>
    </div>
  )
}

/** A round axis maximum: 1, 2, 5 × 10^n at or above value. */
function niceMax(value: number): number {
  const power = 10 ** Math.floor(Math.log10(value))
  for (const factor of [1, 2, 5, 10]) {
    if (factor * power >= value) return factor * power
  }
  return 10 * power
}
