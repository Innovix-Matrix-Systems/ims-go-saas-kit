import { useState, type MouseEvent } from 'react'

/**
 * A one-series trend line (hand-drawn SVG, no chart library): a 2 px line
 * over a faint area, recessive baseline, and a hover readout of the point
 * under the pointer. The tile's title names the series, so there is no
 * legend; the value text stays in text colours.
 */
export function Sparkline({
  points,
  label,
  format,
}: {
  points: { at: string; value: number }[]
  /** What the line shows, for screen readers ("requests per minute"). */
  label: string
  format: (value: number) => string
}) {
  const [hover, setHover] = useState<number | null>(null)
  const width = 240
  const height = 44
  const pad = 3
  const max = Math.max(...points.map((point) => point.value), 0)
  const x = (index: number) => (points.length < 2 ? width / 2 : (index / (points.length - 1)) * width)
  const y = (value: number) => (max === 0 ? height - pad : height - pad - (value / max) * (height - pad * 2))
  const line = points.map((point, index) => `${index === 0 ? 'M' : 'L'}${x(index).toFixed(1)},${y(point.value).toFixed(1)}`).join(' ')
  const area = points.length > 0 ? `${line} L${x(points.length - 1).toFixed(1)},${height} L${x(0).toFixed(1)},${height} Z` : ''

  const onMove = (event: MouseEvent<SVGRectElement>) => {
    const box = event.currentTarget.getBoundingClientRect()
    const ratio = (event.clientX - box.left) / box.width
    setHover(Math.min(points.length - 1, Math.max(0, Math.round(ratio * (points.length - 1)))))
  }
  const point = hover === null ? null : points[hover]

  return (
    <div className="sparkline">
      <svg viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none" role="img" aria-label={label}>
        <line className="sparkline-base" x1="0" x2={width} y1={height - 0.5} y2={height - 0.5} />
        {points.length > 0 ? (
          <>
            <path className="sparkline-area" d={area} />
            <path className="sparkline-line" d={line} />
          </>
        ) : null}
        {point && hover !== null ? (
          <line className="sparkline-cursor" x1={x(hover)} x2={x(hover)} y1="0" y2={height} />
        ) : null}
        <rect
          className="sparkline-hit"
          x="0"
          y="0"
          width={width}
          height={height}
          onMouseMove={onMove}
          onMouseLeave={() => setHover(null)}
        />
      </svg>
      <div className="sparkline-readout" aria-live="polite">
        {point ? `${new Date(point.at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })} · ${format(point.value)}` : ' '}
      </div>
    </div>
  )
}
