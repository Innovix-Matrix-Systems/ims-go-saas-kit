import { useState } from 'react'
import { Area, AreaChart, ResponsiveContainer, Tooltip, YAxis } from 'recharts'
import { pointerIndex } from './TrafficChart'

/**
 * A one-series trend line (Recharts): a 2 px line over a faint area, a
 * recessive baseline, and a text readout of the point under the pointer.
 * The tile's title names the series, so there is no legend; the value text
 * stays in text colours. Colours come from the theme tokens in overview.css.
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
  const max = Math.max(...points.map((point) => point.value), 0)
  const point = hover === null ? null : points[hover]

  return (
    <div className="sparkline">
      <div className="sparkline-plot" role="img" aria-label={label}>
        <ResponsiveContainer width="100%" height={44}>
          <AreaChart
            data={points}
            margin={{ top: 3, right: 0, bottom: 0, left: 0 }}
            onMouseMove={(state) => setHover(pointerIndex(state))}
            onMouseLeave={() => setHover(null)}
          >
            {/* A flat series sits on the baseline instead of mid-height. */}
            <YAxis hide domain={[0, max === 0 ? 1 : max]} />
            <Tooltip content={() => null} isAnimationActive={false} />
            <Area
              type="linear"
              dataKey="value"
              className="sparkline-series"
              strokeWidth={2}
              dot={false}
              activeDot={false}
              isAnimationActive={false}
            />
          </AreaChart>
        </ResponsiveContainer>
      </div>
      <div className="sparkline-readout" aria-live="polite">
        {point ? `${new Date(point.at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })} · ${format(point.value)}` : ' '}
      </div>
    </div>
  )
}
