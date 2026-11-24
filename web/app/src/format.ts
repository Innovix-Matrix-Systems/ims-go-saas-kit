import type { BadgeTone } from './components/ui'

export function formatTime(value: string | number): string {
  const date = new Date(value)
  return date.toLocaleTimeString(undefined, { hour12: false })
}

export function formatDateTime(value: string | number): string {
  const date = new Date(value)
  return date.toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  })
}

export function relativeTime(value: string | number): string {
  const then = new Date(value).getTime()
  const delta = Date.now() - then
  if (Number.isNaN(delta)) return '—'
  const seconds = Math.round(delta / 1000)
  if (seconds < 5) return 'just now'
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.round(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.round(hours / 24)
  return `${days}d ago`
}

/** "in 4m", "in 3h", "in 2d" for a time ahead; relativeTime for one behind. */
export function timeUntil(value: string | number): string {
  const delta = new Date(value).getTime() - Date.now()
  if (Number.isNaN(delta)) return '—'
  if (delta <= 0) return relativeTime(value)
  const seconds = Math.round(delta / 1000)
  if (seconds < 60) return `in ${seconds}s`
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `in ${minutes}m`
  const hours = Math.round(minutes / 60)
  if (hours < 24) return `in ${hours}h`
  return `in ${Math.round(hours / 24)}d`
}

/** A cron expression in words for the common shapes; anything else stays as is. */
export function describeCron(spec: string): string {
  const parts = spec.trim().split(/\s+/)
  if (parts.length !== 5) return spec
  const [minute, hour, day, month, weekday] = parts
  const at = (h: string, m: string) => `${h.padStart(2, '0')}:${m.padStart(2, '0')}`
  const numeric = (value: string) => /^\d+$/.test(value)
  const days = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']
  if (spec.trim() === '* * * * *') return 'Every minute'
  if (/^\*\/\d+$/.test(minute) && hour === '*' && day === '*' && month === '*' && weekday === '*') {
    return `Every ${minute.slice(2)} minutes`
  }
  if (numeric(minute) && hour === '*' && day === '*' && month === '*' && weekday === '*') {
    return `Hourly at :${minute.padStart(2, '0')}`
  }
  if (numeric(minute) && /^\*\/\d+$/.test(hour) && day === '*' && month === '*' && weekday === '*') {
    return `Every ${hour.slice(2)} hours at :${minute.padStart(2, '0')}`
  }
  if (numeric(minute) && numeric(hour) && month === '*') {
    if (day === '*' && weekday === '*') return `Daily at ${at(hour, minute)}`
    if (day === '*' && numeric(weekday)) return `${days[Number(weekday) % 7]}s at ${at(hour, minute)}`
    if (numeric(day) && weekday === '*') return `Monthly on day ${day} at ${at(hour, minute)}`
  }
  return spec
}

export function formatDuration(ms: number | undefined | null): string {
  if (ms === undefined || ms === null || Number.isNaN(ms)) return '—'
  if (ms < 1) return `${Math.round(ms * 1000)}µs`
  if (ms < 1000) return `${ms.toFixed(ms < 10 ? 2 : 1)}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

export function formatBytes(bytes: number | undefined | null): string {
  if (bytes === undefined || bytes === null) return '—'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(2)} MB`
}

export function statusTone(status: number): BadgeTone {
  if (status >= 500) return 'danger'
  if (status >= 400) return 'warn'
  if (status >= 300) return 'info'
  if (status >= 200) return 'ok'
  return 'neutral'
}

// Job states: green only for a job that finished well, the accent for one
// running now, red for one River gave up on.
const stateTones: Record<string, BadgeTone> = {
  available: 'info',
  cancelled: 'neutral',
  completed: 'ok',
  discarded: 'danger',
  pending: 'neutral',
  retryable: 'warn',
  running: 'accent',
  scheduled: 'neutral',
}

export function stateTone(state: string): BadgeTone {
  return stateTones[state] ?? 'neutral'
}

export function levelTone(level: string): BadgeTone {
  switch (level.toUpperCase()) {
    case 'ERROR':
    case 'CRITICAL':
      return 'danger'
    case 'WARN':
    case 'WARNING':
      return 'warn'
    case 'INFO':
      return 'info'
    default:
      return 'neutral'
  }
}

/** Where a setting or flag value comes from, the same colour on every screen. */
export function sourceTone(source: string): BadgeTone {
  switch (source) {
    case 'user':
      return 'accent'
    case 'tenant':
      return 'violet'
    case 'global':
      return 'info'
    default:
      return 'neutral'
  }
}

export function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '?'
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase()
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase()
}

export function prettyJSON(value: unknown): string {
  if (value === undefined || value === null) return '—'
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

export function shortRequestID(value: string): string {
  if (!value) return '—'
  return value.length > 12 ? value.slice(0, 12) : value
}

/** "in 3 days", "in 2 years" or "5 days ago" for dates either side of now. */
export function relativeDate(value: string | number): string {
  const delta = new Date(value).getTime() - Date.now()
  if (Number.isNaN(delta)) return '—'
  const future = delta > 0
  const days = Math.abs(delta) / 86_400_000
  let text: string
  if (days < 1) text = `${Math.max(1, Math.round(Math.abs(delta) / 3_600_000))}h`
  else if (days < 60) text = `${Math.round(days)} day${Math.round(days) === 1 ? '' : 's'}`
  else if (days < 730) text = `${Math.round(days / 30)} months`
  else text = `${Math.round(days / 365)} years`
  return future ? `in ${text}` : `${text} ago`
}

function utcOffset(date: Date): string {
  const minutes = -date.getTimezoneOffset()
  const sign = minutes >= 0 ? '+' : '-'
  const abs = Math.abs(minutes)
  return `${sign}${String(Math.floor(abs / 60)).padStart(2, '0')}:${String(abs % 60).padStart(2, '0')}`
}

function localDay(iso: string | undefined, hours: number, minutes: number, seconds: number, ms: number): string | undefined {
  if (!iso) return undefined
  const [year, month, day] = iso.split('-').map(Number)
  if (!year || !month || !day) return undefined
  const at = new Date(year, month - 1, day, hours, minutes, seconds, ms)
  const time = `${String(hours).padStart(2, '0')}:${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`
  return `${iso}T${time}${ms ? `.${String(ms).padStart(3, '0')}` : ''}${utcOffset(at)}`
}

/**
 * Start of a picked calendar day (YYYY-MM-DD) in the viewer's time zone, as
 * RFC 3339. The API reads a bare date as a UTC day, which is the wrong day
 * for anyone east or west of UTC around midnight.
 */
export function localDayStart(iso: string | undefined): string | undefined {
  return localDay(iso, 0, 0, 0, 0)
}

/** Last millisecond of a picked calendar day in the viewer's time zone (RFC 3339). */
export function localDayEnd(iso: string | undefined): string | undefined {
  return localDay(iso, 23, 59, 59, 999)
}

/** "5 minutes ago" in the given BCP 47 locale. */
export function relativeTimeIn(locale: string, value: string | number): string {
  const seconds = Math.round((new Date(value).getTime() - Date.now()) / 1000)
  if (Number.isNaN(seconds)) return '—'
  const format = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' })
  const steps: [Intl.RelativeTimeFormatUnit, number][] = [
    ['second', 60],
    ['minute', 60],
    ['hour', 24],
    ['day', 30],
    ['month', 12],
    ['year', Infinity],
  ]
  let amount = seconds
  for (const [unit, size] of steps) {
    if (Math.abs(amount) < size) return format.format(Math.round(amount), unit)
    amount /= size
  }
  return format.format(Math.round(amount), 'year')
}
