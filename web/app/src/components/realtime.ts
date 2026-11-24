import { useEffect, useSyncExternalStore } from 'react'
import type { QueryKey } from '@tanstack/react-query'
import { ApiError, apiStream } from '../api/client'
import { useSession } from '../auth/session'
import { queryClient } from '../query'
import { feedKey } from './notifications'

/**
 * Live updates: one Server-Sent Events stream per browser (GET
 * /api/v1/realtime/stream) says when the account's notifications or
 * announcements changed, and the matching queries refetch. Events carry no
 * data, so a missed one costs nothing but the slow fallback poll.
 *
 * One tab holds the stream (Web Locks); it relays events and its status to
 * the others over a BroadcastChannel, and when it closes another tab takes
 * over. Tabs acting as someone else (X-Acting-*) stay out and keep polling.
 */

export type RealtimeStatus = 'off' | 'connecting' | 'live' | 'offline'

/** What each event refetches. "resync" (and a reconnect) refetch them all. */
const invalidates: Record<string, QueryKey[]> = {
  notifications: [feedKey],
  announcements: [['announcements', 'live']],
  // A payment was submitted, approved, rejected or withdrawn: the
  // organization's plan and payments, and the console's approval list.
  billing: [['subscription-status'], ['billing'], ['payments'], ['console', 'approvals']],
}

const STREAM_PATH = '/realtime/stream'
const MIN_BACKOFF = 1_000
const MAX_BACKOFF = 30_000

// ── Status store (useSyncExternalStore, like appearance.ts) ──────────────────

let status: RealtimeStatus = 'off'
const listeners = new Set<() => void>()

function setStatus(next: RealtimeStatus): void {
  if (status === next) return
  status = next
  for (const listener of listeners) listener()
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

/** The stream's state; polling slows down while it is 'live'. */
export function useRealtimeStatus(): RealtimeStatus {
  return useSyncExternalStore(subscribe, () => status, () => 'off')
}

/** A query's poll interval: slow while live updates arrive, normal otherwise. */
export function usePollInterval(normal: number, whileLive: number): number {
  return useRealtimeStatus() === 'live' ? whileLive : normal
}

function apply(kind: string): void {
  const keys = kind === 'resync' ? Object.values(invalidates).flat() : invalidates[kind]
  for (const queryKey of keys ?? []) void queryClient.invalidateQueries({ queryKey })
}

// ── The stream ───────────────────────────────────────────────────────────────

type Message = { type: 'event'; kind: string } | { type: 'status'; status: RealtimeStatus } | { type: 'hello' }

/**
 * Mounted once in the signed-in shell. Renders nothing.
 */
export function RealtimeConnector() {
  const session = useSession()
  const userId = session?.user.id
  const acting = session?.acting != null

  useEffect(() => {
    if (!userId || acting || typeof BroadcastChannel === 'undefined') {
      setStatus('off')
      return
    }
    const stop = new AbortController()
    // Per user: two tabs signed in as different people never share a stream.
    const channel = new BroadcastChannel(`ims.app.realtime.${userId}`)
    let leader = false
    const post = (message: Message) => channel.postMessage(message)

    channel.onmessage = ({ data }: MessageEvent<Message>) => {
      if (data.type === 'event') apply(data.kind)
      else if (data.type === 'status' && !leader) setStatus(data.status)
      else if (data.type === 'hello' && leader) post({ type: 'status', status })
    }

    const lead = async () => {
      leader = true
      const report = (next: RealtimeStatus) => {
        setStatus(next)
        post({ type: 'status', status: next })
      }
      const emit = (kind: string) => {
        apply(kind)
        post({ type: 'event', kind })
      }
      await runStream(stop.signal, emit, report)
    }

    setStatus('connecting')
    if (navigator.locks) {
      // The lock is held while lead() runs, so exactly one tab streams; the
      // others wait in line and take over when it closes.
      navigator.locks.request(`ims.app.realtime.${userId}`, { signal: stop.signal }, lead).catch(() => {})
      post({ type: 'hello' })
    } else {
      void lead() // no Web Locks: every tab streams for itself
    }

    return () => {
      stop.abort()
      channel.close()
      setStatus('off')
    }
  }, [userId, acting])

  return null
}

/** Keeps a stream open until signal aborts, reconnecting with backoff. */
async function runStream(signal: AbortSignal, emit: (kind: string) => void, report: (status: RealtimeStatus) => void) {
  let backoff = MIN_BACKOFF
  while (!signal.aborted) {
    report('connecting')
    let wait = 0
    try {
      const response = await apiStream(STREAM_PATH, signal)
      report('live')
      backoff = MIN_BACKOFF
      // Refetch once on every connect, the first included: a change made
      // while the page was loading (or while we were away) sent its event
      // before this stream existed.
      emit('resync')
      await readEvents(response, signal, emit)
      // The server asked for a reconnect (or closed): reconnect at once.
    } catch (error) {
      if (signal.aborted) return
      if (error instanceof ApiError && (error.status === 401 || error.status === 404)) {
        // Signed out (the API client handles it) or live updates turned off.
        report('off')
        return
      }
      wait = error instanceof ApiError && error.status === 429 ? MAX_BACKOFF : backoff
      backoff = Math.min(backoff * 2, MAX_BACKOFF)
      report('offline')
    }
    if (wait > 0) await sleep(wait + Math.random() * 500, signal)
  }
}

/**
 * Reads SSE frames until the stream ends, a "reconnect" event arrives or no
 * byte (not even a ping) arrives for two heartbeats.
 */
async function readEvents(response: Response, signal: AbortSignal, emit: (kind: string) => void): Promise<void> {
  const reader = response.body!.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let event = ''
  let silenceLimit = 60_000
  let silence: ReturnType<typeof setTimeout> | undefined
  const resetSilence = () => {
    clearTimeout(silence)
    silence = setTimeout(() => void reader.cancel(), silenceLimit)
  }
  const onAbort = () => void reader.cancel()
  signal.addEventListener('abort', onAbort)
  resetSilence()
  try {
    for (;;) {
      const { value, done } = await reader.read()
      if (done) return
      resetSilence()
      buffer += decoder.decode(value, { stream: true })
      let newline: number
      while ((newline = buffer.indexOf('\n')) >= 0) {
        const line = buffer.slice(0, newline).replace(/\r$/, '')
        buffer = buffer.slice(newline + 1)
        if (line === '') {
          // End of one event.
          if (event === 'reconnect') return
          if (event && event !== 'ready') emit(event)
          event = ''
        } else if (line.startsWith('event:')) {
          event = line.slice(6).trim()
        } else if (line.startsWith('data:') && event === 'ready') {
          const heartbeat = Number(JSON.parse(line.slice(5)).heartbeat)
          if (heartbeat > 0) silenceLimit = heartbeat * 2_000
          resetSilence()
        }
        // ": ping" comments and "retry:" only keep the silence timer happy.
      }
    }
  } finally {
    clearTimeout(silence)
    signal.removeEventListener('abort', onAbort)
  }
}

function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const timer = setTimeout(resolve, ms)
    signal.addEventListener('abort', () => {
      clearTimeout(timer)
      resolve()
    })
  })
}
