import { useQuery } from '@tanstack/react-query'
import { api, clearToken, getActing, getToken, setActing, setToken, type Acting, type LoginResult, type User } from '../api/client'
import { queryClient } from '../query'

// The signed-in session: who the user is and what they may do. The token is in
// api/client; the profile and permissions are cached by TanStack Query.

export interface Permission {
  id: number
  name: string
}

export interface Session {
  user: User
  permissions: Set<string>
  /** Super-Admins pass every permission check. */
  superAdmin: boolean
  /** The account has no organization (staff), and nobody is acting as a user. */
  isPlatformUser: boolean
  /** Staff impersonating an organization or logged in as a user. */
  acting: Acting | null
}

const DEVICE = deviceName()

function deviceName(): string {
  if (typeof navigator === 'undefined') return 'Web'
  const ua = navigator.userAgent
  const browser = /Edg\//.test(ua) ? 'Edge' : /Firefox\//.test(ua) ? 'Firefox' : /Chrome\//.test(ua) ? 'Chrome' : /Safari\//.test(ua) ? 'Safari' : 'Browser'
  const os = /Windows/.test(ua) ? 'Windows' : /Mac OS X/.test(ua) ? 'macOS' : /Android/.test(ua) ? 'Android' : /iPhone|iPad/.test(ua) ? 'iOS' : /Linux/.test(ua) ? 'Linux' : ''
  return `Web · ${browser}${os ? ` on ${os}` : ''}`
}

/** A sign-in that still needs a second factor. */
export interface PendingSecondFactor {
  email: string
  method: 'totp' | 'email'
  remember: boolean
}

/** A sign-in whose token only reaches the two-factor setup routes. */
export interface SetupOnlySession {
  token: string
  remember: boolean
}

export type SignInOutcome =
  | { kind: 'signed-in' }
  | { kind: 'second-factor'; pending: PendingSecondFactor }
  | { kind: 'two-factor-setup'; setup: SetupOnlySession }

let setupOnly: SetupOnlySession | null = null

export function getSetupOnlySession(): SetupOnlySession | null {
  return setupOnly
}

function finish(result: LoginResult, email: string, remember: boolean): SignInOutcome {
  if (result.status === 'OTP_REQUIRED') {
    return { kind: 'second-factor', pending: { email, method: result.method ?? 'totp', remember } }
  }
  if (!result.token) throw new Error('The server did not return a session token.')
  if (result.status === 'TWO_FACTOR_SETUP_REQUIRED') {
    setupOnly = { token: result.token, remember }
    return { kind: 'two-factor-setup', setup: setupOnly }
  }
  setupOnly = null
  setToken(result.token, remember)
  queryClient.clear()
  return { kind: 'signed-in' }
}

export async function signIn(email: string, password: string, remember: boolean): Promise<SignInOutcome> {
  const result = await api<LoginResult>('/login', {
    method: 'POST',
    body: { email, password, device: DEVICE },
    quiet: true,
    token: null,
  })
  return finish(result, email, remember)
}

export async function verifySecondFactor(pending: PendingSecondFactor, code: string): Promise<SignInOutcome> {
  const result = await api<LoginResult>('/verify-otp', {
    method: 'POST',
    body: { email: pending.email, otp: code.trim(), device: DEVICE },
    quiet: true,
    token: null,
  })
  return finish(result, pending.email, pending.remember)
}

/** Revokes the token on the server (best effort) and clears everything local. */
export async function signOut(): Promise<void> {
  const token = getToken() ?? setupOnly?.token ?? null
  if (token) {
    try {
      // The API signs out the tokens of this device name, so send the one
      // used at sign-in.
      await api('/logout', { method: 'POST', body: { device: DEVICE }, quiet: true, token })
    } catch {
      /* offline or already revoked: the local session is cleared anyway */
    }
  }
  forgetSession()
}

let sessionEnded = false

/**
 * Clears the local session without calling the server. `ended` marks it as
 * ended by the server (a 401), so the sign-in page can say so once.
 */
export function forgetSession(ended = false): void {
  setupOnly = null
  clearToken()
  queryClient.clear()
  if (ended) sessionEnded = true
}

/** True once after the server ended the session; resets when read. */
export function takeSessionEndedNotice(): boolean {
  const value = sessionEnded
  sessionEnded = false
  return value
}

export function isSignedIn(): boolean {
  return Boolean(getToken())
}

interface ProfilePermissions {
  super_admin: boolean
  permissions: string[]
  acting_as: boolean
}

// Both calls carry the acting headers, so logging in as a user loads that
// user's profile and permissions and the app renders as they see it.
async function loadSession(): Promise<Session> {
  const [user, granted] = await Promise.all([
    api<User>('/user/profile'),
    api<ProfilePermissions>('/user/profile/permissions').catch(() => ({ super_admin: false, permissions: [], acting_as: false })),
  ])
  return {
    user,
    permissions: new Set(granted.permissions),
    superAdmin: granted.super_admin,
    isPlatformUser: user.tenant_id == null,
    acting: getActing(),
  }
}

/** Starts impersonating an organization or logging in as a user, or stops (null). */
export async function changeActing(acting: Acting | null): Promise<void> {
  setActing(acting)
  // Everything cached belongs to whoever the app was acting as before.
  queryClient.clear()
  await queryClient.fetchQuery(sessionQuery)
}

export const sessionQuery = {
  queryKey: ['session'] as const,
  queryFn: loadSession,
  staleTime: 60_000,
}

/** The current session; only use inside the signed-in area. */
export function useSession(): Session | undefined {
  return useQuery(sessionQuery).data
}

/** Whether the session holds a permission (UI hint only; the API enforces). */
export function can(session: Session | undefined, permission: string): boolean {
  return Boolean(session && (session.superAdmin || session.permissions.has(permission)))
}
