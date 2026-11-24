import { getLocale } from '../i18n'
import type { components } from './schema'

export type Schemas = components['schemas']
export type User = Schemas['User']
export type LoginResult = Schemas['LoginEnvelope']['data']

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
  /** Field errors from a 422 (field -> messages). */
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

  /** The first message for a form field, if the API rejected it. */
  field(name: string): string | undefined {
    return this.errors?.[name]?.[0]
  }
}

function problemError(status: number, payload: unknown): ApiError {
  const problem = payload && typeof payload === 'object' ? (payload as Problem) : null
  const detail = problem?.detail ?? problem?.title ?? `Request failed (${status})`
  return new ApiError(detail, status, problem)
}

// ── Token storage ────────────────────────────────────────────────────────────
// The token lives in sessionStorage (this tab, gone when it closes) unless the
// user chose "keep me signed in", which uses localStorage. Never in a URL.

const TOKEN_KEY = 'ims.app.token'

function stores(): Storage[] {
  const out: Storage[] = []
  try {
    out.push(window.sessionStorage)
  } catch {
    /* storage disabled */
  }
  try {
    out.push(window.localStorage)
  } catch {
    /* storage disabled */
  }
  return out
}

export function getToken(): string | null {
  for (const store of stores()) {
    const value = store.getItem(TOKEN_KEY)
    if (value) return value
  }
  return null
}

export function setToken(token: string, remember: boolean): void {
  clearToken()
  try {
    ;(remember ? window.localStorage : window.sessionStorage).setItem(TOKEN_KEY, token)
  } catch {
    /* storage disabled: the session lasts until reload */
  }
}

export function clearToken(): void {
  for (const store of stores()) store.removeItem(TOKEN_KEY)
  setActing(null)
}

/** Whether the current token was stored with "keep me signed in". */
export function isRemembered(): boolean {
  try {
    return Boolean(window.localStorage.getItem(TOKEN_KEY))
  } catch {
    return false
  }
}

// ── App-wide events ──────────────────────────────────────────────────────────
// Some answers change what the whole app shows, not just one screen. The
// client reports them here; the router listens and navigates.

export type ApiEvent =
  | { type: 'unauthenticated' }
  | { type: 'two-factor-setup-required' }
  | { type: 'tenant-suspended' }
  | { type: 'maintenance'; message?: string }

const eventListeners = new Set<(event: ApiEvent) => void>()

export function onApiEvent(listener: (event: ApiEvent) => void): () => void {
  eventListeners.add(listener)
  return () => eventListeners.delete(listener)
}

function emit(event: ApiEvent): void {
  for (const listener of eventListeners) listener(event)
}

function report(error: ApiError): void {
  if (error.status === 401) emit({ type: 'unauthenticated' })
  else if (error.code === 'TWO_FACTOR_SETUP_REQUIRED') emit({ type: 'two-factor-setup-required' })
  else if (error.code === 'TENANT_SUSPENDED') emit({ type: 'tenant-suspended' })
  else if (error.status === 503 && error.code === 'MAINTENANCE_MODE') emit({ type: 'maintenance', message: error.message })
}

// ── Acting as an organization or a user (staff only) ────────────────────────
// Impersonating an organization sends X-Acting-Tenant: staff keep their own
// account and permissions inside it. Logging in as a user sends
// X-Acting-User: every request runs as that user. Kept per tab (sessionStorage)
// and never across a sign-out.

export type Acting =
  | { kind: 'tenant'; tenantId: number; name: string }
  | { kind: 'user'; userId: number; name: string; email: string; tenantName?: string }

const ACTING_KEY = 'ims.app.acting'

export function getActing(): Acting | null {
  try {
    const raw = window.sessionStorage.getItem(ACTING_KEY)
    return raw ? (JSON.parse(raw) as Acting) : null
  } catch {
    return null
  }
}

export function setActing(acting: Acting | null): void {
  try {
    if (acting) window.sessionStorage.setItem(ACTING_KEY, JSON.stringify(acting))
    else window.sessionStorage.removeItem(ACTING_KEY)
  } catch {
    // Storage may be unavailable; acting then lasts for this page only.
  }
}

// ── Requests ─────────────────────────────────────────────────────────────────

type QueryValue = string | number | boolean | undefined | null

export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PATCH' | 'PUT' | 'DELETE'
  body?: unknown
  query?: Record<string, QueryValue>
  signal?: AbortSignal
  /** Keep the raw {data, links, meta} envelope (paginated and cursor lists). */
  envelope?: boolean
  /** Send an Idempotency-Key so a retried create or payment runs once. */
  idempotent?: boolean
  /** Do not raise app-wide events (sign-in screens handle 401/403 themselves). */
  quiet?: boolean
  /** Use this token instead of the stored one (e.g. a setup-only token). */
  token?: string | null
  /** Leave out the acting headers (console screens that work as staff). */
  asStaff?: boolean
  /** Read one organization from the console (X-Acting-Tenant for this call only). */
  actingTenant?: number
}

function buildURL(path: string, query?: Record<string, QueryValue>): string {
  const url = new URL(path.startsWith('/api/') ? path : `/api/v1${path}`, window.location.origin)
  if (query) {
    for (const [key, value] of Object.entries(query)) {
      if (value === undefined || value === null || value === '') continue
      url.searchParams.set(key, String(value))
    }
  }
  return url.toString()
}

function headersFor(options: RequestOptions): Headers {
  const headers = new Headers({ Accept: 'application/json', 'Accept-Language': getLocale() })
  if (options.body !== undefined && !(options.body instanceof FormData)) headers.set('Content-Type', 'application/json')
  const token = options.token === undefined ? getToken() : options.token
  if (token) headers.set('Authorization', `Bearer ${token}`)
  if (options.idempotent) headers.set('Idempotency-Key', crypto.randomUUID())
  if (options.actingTenant) {
    headers.set('X-Acting-Tenant', String(options.actingTenant))
    return headers
  }
  const acting = options.asStaff || options.token === null ? null : getActing()
  if (acting?.kind === 'tenant') headers.set('X-Acting-Tenant', String(acting.tenantId))
  if (acting?.kind === 'user') headers.set('X-Acting-User', String(acting.userId))
  return headers
}

/**
 * Calls the API under /api/v1 (pass "/user/profile", not the full path) and
 * returns `data` from the envelope. Errors are ApiError with the problem fields.
 */
export async function api<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const method = options.method ?? 'GET'
  // Most write endpoints decode a JSON body and refuse an empty one, even when
  // there is nothing to send, so writes always carry one.
  if (options.body === undefined && method !== 'GET' && method !== 'DELETE') options = { ...options, body: {} }
  const body =
    options.body === undefined ? undefined : options.body instanceof FormData ? options.body : JSON.stringify(options.body)
  let response: Response
  try {
    response = await fetch(buildURL(path, options.query), {
      method,
      headers: headersFor(options),
      body,
      signal: options.signal,
      credentials: 'same-origin',
      referrerPolicy: 'no-referrer',
    })
  } catch (cause) {
    if (cause instanceof DOMException && cause.name === 'AbortError') throw cause
    throw new ApiError('The server could not be reached. Check your connection and try again.', 0)
  }

  if (response.status === 204) return undefined as T
  const isJSON = response.headers.get('content-type')?.includes('json') ?? false
  const payload = isJSON ? await response.json().catch(() => null) : null

  if (!response.ok) {
    const error = problemError(response.status, payload)
    if (!options.quiet) report(error)
    throw error
  }

  if (payload && typeof payload === 'object' && 'data' in payload && !options.envelope) {
    return (payload as { data: T }).data
  }
  return payload as T
}

/**
 * Opens a Server-Sent Events stream with the bearer header (EventSource cannot
 * send one) and returns the open response for the caller to read. Errors are
 * ApiError, reported like any other call (a 401 signs out).
 */
export async function apiStream(path: string, signal: AbortSignal): Promise<Response> {
  const headers = headersFor({ asStaff: true })
  headers.set('Accept', 'text/event-stream')
  let response: Response
  try {
    response = await fetch(buildURL(path), { headers, signal, cache: 'no-store', referrerPolicy: 'no-referrer' })
  } catch (cause) {
    if (cause instanceof DOMException && cause.name === 'AbortError') throw cause
    throw new ApiError('The server could not be reached.', 0)
  }
  if (!response.ok || !response.body) {
    const payload = await response.json().catch(() => null)
    const error = problemError(response.status, payload)
    report(error)
    throw error
  }
  return response
}

/** Downloads a file the API serves behind authentication. */
export async function apiBlob(path: string, signal?: AbortSignal): Promise<Blob> {
  const response = await fetch(buildURL(path), { headers: headersFor({}), signal, referrerPolicy: 'no-referrer' })
  if (!response.ok) {
    const payload = await response.json().catch(() => null)
    const error = problemError(response.status, payload)
    report(error)
    throw error
  }
  return response.blob()
}

/** Hands a downloaded blob to the browser as a file named `name`. */
export function saveBlob(blob: Blob, name: string): void {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = name
  link.click()
  setTimeout(() => URL.revokeObjectURL(url), 10_000)
}
