import type { LoginResult, Me } from './types'

const TOKEN_KEY = 'ims.dev.token'
const ME_KEY = 'ims.dev.me'

/** RFC 9457 problem body as rendered by the API. */
export interface Problem {
  title?: string
  detail?: string
  status?: number
  error_code?: string
  errors?: Record<string, string[]>
  meta?: Record<string, unknown>
  retry_after?: number
  trace_id?: string
}

export class ApiError extends Error {
  status: number
  code?: string
  /** Field errors from a 422 (dot-path field -> messages). */
  errors?: Record<string, string[]>
  /** Extension data, e.g. {feature_key, limit, current} on a 402. */
  meta?: Record<string, unknown>
  /** Seconds to wait before retrying a 429. */
  retryAfter?: number
  traceId?: string

  constructor(message: string, status: number, problem?: Problem | null) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = problem?.error_code
    this.errors = problem?.errors
    this.meta = problem?.meta
    this.retryAfter = problem?.retry_after
    this.traceId = problem?.trace_id
  }
}

function problemError(status: number, payload: unknown): ApiError {
  const problem = payload && typeof payload === 'object' ? (payload as Problem) : null
  const detail = problem?.detail ?? problem?.title ?? `Request failed (${status})`
  return new ApiError(detail, status, problem)
}

export function getToken(): string | null {
  try {
    return localStorage.getItem(TOKEN_KEY)
  } catch {
    return null
  }
}

export function setToken(token: string | null): void {
  try {
    if (token) localStorage.setItem(TOKEN_KEY, token)
    else localStorage.removeItem(TOKEN_KEY)
  } catch {
    /* storage disabled */
  }
}

export function getCachedMe(): Me | null {
  try {
    const raw = localStorage.getItem(ME_KEY)
    return raw ? (JSON.parse(raw) as Me) : null
  } catch {
    return null
  }
}

export function setCachedMe(me: Me | null): void {
  try {
    if (me) localStorage.setItem(ME_KEY, JSON.stringify(me))
    else localStorage.removeItem(ME_KEY)
  } catch {
    /* storage disabled */
  }
}

export function clearSession(): void {
  setToken(null)
  setCachedMe(null)
}

/** Signs out: revokes the token on the server, then clears the session. */
export async function logoutRedirect(): Promise<void> {
  const token = getToken()
  if (token) {
    try {
      await fetch(new URL('/api/v1/logout', window.location.origin).toString(), {
        method: 'POST',
        headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
        body: JSON.stringify({ device: 'dev-dashboard' }),
      })
    } catch {
      /* offline: the local session is still cleared */
    }
  }
  clearSession()
  window.location.assign('/dev/login')
}

type QueryValue = string | number | boolean | undefined | null

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PATCH' | 'PUT' | 'DELETE'
  body?: unknown
  query?: Record<string, QueryValue>
  signal?: AbortSignal
  /** Keep the raw {data, links, meta} envelope (cursor lists). */
  unwrap?: boolean
}

export async function api<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const url = new URL(path, window.location.origin)
  if (options.query) {
    for (const [key, value] of Object.entries(options.query)) {
      if (value === undefined || value === null || value === '') continue
      url.searchParams.set(key, String(value))
    }
  }
  const headers = new Headers()
  if (options.body !== undefined) headers.set('Content-Type', 'application/json')
  const token = getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)

  const response = await fetch(url.toString(), {
    method: options.method ?? 'GET',
    headers,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
    signal: options.signal,
  })

  const isJSON = response.headers.get('content-type')?.includes('json') ?? false
  const payload = isJSON ? await response.json().catch(() => null) : null

  if (!response.ok) {
    if (response.status === 401) {
      clearSession()
      if (!window.location.pathname.endsWith('/login')) window.location.assign('/dev/login')
    }
    throw problemError(response.status, payload)
  }

  if (payload && typeof payload === 'object' && 'data' in payload) {
    if (options.unwrap === false) return payload as T
    return (payload as { data: T }).data
  }
  return payload as T
}

export async function apiBlob(path: string, signal?: AbortSignal): Promise<Blob> {
  const response = await fetch(new URL(path, window.location.origin).toString(), {
    headers: getToken() ? { Authorization: `Bearer ${getToken()}` } : undefined,
    signal,
  })
  if (!response.ok) {
    if (response.status === 401) {
      clearSession()
      if (!window.location.pathname.endsWith('/login')) window.location.assign('/dev/login')
    }
    const payload = await response.json().catch(() => null)
    throw problemError(response.status, payload)
  }
  return response.blob()
}

export async function login(email: string, password: string): Promise<LoginResult> {
  const response = await fetch('/api/v1/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password, device: 'dev-dashboard' }),
  })
  const payload = await response.json().catch(() => null)
  if (!response.ok) {
    throw problemError(response.status, payload)
  }
  return (payload as { data: LoginResult }).data
}

/** Completes an OTP_REQUIRED sign-in with a code (or a recovery code). */
export async function verifyOTP(email: string, otp: string): Promise<LoginResult> {
  const response = await fetch('/api/v1/verify-otp', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, otp, device: 'dev-dashboard' }),
  })
  const payload = await response.json().catch(() => null)
  if (!response.ok) {
    throw problemError(response.status, payload)
  }
  return (payload as { data: LoginResult }).data
}

export function fetchMe(): Promise<Me> {
  return api<Me>('/dev/api/me')
}

/**
 * Writes sample logs/exceptions (POST /dev/api/simulate-events) and waits
 * for the telemetry collector's batch flush (750 ms), so a refetch right
 * after sees them.
 */
export async function writeSampleEvents(body: { logs: number; level: 'warn' | 'error'; exception: boolean }): Promise<void> {
  await api('/dev/api/simulate-events', { method: 'POST', body })
  await new Promise((resolve) => setTimeout(resolve, 1000))
}
