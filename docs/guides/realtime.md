---
title: Live updates (SSE)
description: How the app learns that notifications or announcements changed without polling — the stream, the Postgres bus behind it, publishing from a module, and running it behind nginx or a pooler.
---

The app's notification bell and announcement banners update as soon as something changes,
instead of on a timer. One Server-Sent Events stream per browser says *what kind* of thing
changed for the signed-in account; the app refetches it through the normal API.

```text
any process (API, worker, scheduler)          ims serve (every instance)               browser
service write ── pg_notify('ims_realtime') ──► pubsub.Listener ─► pubsub.Hub ─► GET /api/v1/realtime/stream
                                                (one LISTEN connection)       event: notifications
                                                                               → refetch the feed
```

## What the client sees

`GET /api/v1/realtime/stream` with the usual bearer header holds a `text/event-stream` open:

| Frame | Meaning |
|---|---|
| `event: ready` | Connected; `data: {"heartbeat": 25}` |
| `event: notifications` | Refetch the notification feed and unread count |
| `event: announcements` | Refetch `GET /api/v1/announcements/live` |
| `event: resync` | Refetch everything (the server may have missed events) |
| `: ping` | Every `REALTIME_HEARTBEAT` while idle, so proxies keep it open |
| `event: reconnect` | The server is closing the stream (after `REALTIME_MAX_STREAM`, or shutdown): open a new one |

Events carry **no data**. Permissions, tenancy, branch scope and read state stay in the endpoints
the app already calls, so there is no second code path to keep in sync and nothing sensitive on
the wire. A missed event costs one fallback poll; an event sent to someone it did not concern
costs one refetch that finds nothing new.

Browsers open the stream with `fetch` and read the body (`EventSource` cannot send an
`Authorization` header, and the token never goes in a URL). The app's client is
`web/app/src/components/realtime.ts`: one tab per user holds the stream (Web Locks) and relays to
the others (`BroadcastChannel`), it reconnects with backoff, refetches everything after a gap, and
the bell and banner keep a slow poll as a fallback. Tabs acting as another organization or user
keep polling.

## Publishing from a module

A module tells streams about a change through a `pubsub.Publisher`, wired like any optional
collaborator:

```go
// platform.go
notificationsService := notifications.NewService(...).WithPublisher(realtimePublisher)

// the module
const RealtimeKind = "notifications"

func (s *Service) MarkRead(ctx context.Context, ...) (err error) {
	// ... the write ...
	s.publish(ctx, pubsub.ToUsers(RealtimeKind, userID))
	return nil
}
```

- **Address who should refetch:** `pubsub.ToUsers(kind, ids...)` for named users,
  `pubsub.ToTenant(kind, tenantID)` for a tenant's members (`0` = tenantless platform accounts),
  `Event{AllTenants: true}` for every organization. Prefer a slightly wider audience over
  re-implementing a read's matching rules: an event is a nudge.
- **Publish after the write succeeds**, never before. `NOTIFY` is delivered on commit, so a
  publisher built on a transaction's queries sends nothing if it rolls back.
- **Publishing never fails the write**: log and move on, polling still finds the change. A
  payload over the 8 KB `NOTIFY` limit returns `pubsub.ErrTooLarge`; name the tenant instead of
  listing thousands of users (notifications switch over at 500).
- **Kinds are short lowercase names** (`notifications`), one per thing the client refetches. Add the
  kind to `invalidates` in `web/app/src/components/realtime.ts` so the app knows what to refetch.
- **Changes on a clock** (an announcement starting) have no write to hang a publish on: a
  scheduled task publishes them (`announcements.publish-transitions`, every minute).

Where notifications and announcements publish:

| Change | Process | Who hears it |
|---|---|---|
| Notification fan-out finished (unread counts written) | worker | the addressed users (their tenant beyond 500) |
| Notification created with no fan-out, updated or deleted | API | its tenant (platform accounts for tenantless ones) |
| Mark read / mark all read | API / worker | that user (their other tabs and devices) |
| Announcement created, updated or deleted while live | API | its organizations (before and after an edit) |
| Announcement starts or ends on schedule | scheduler | its organizations |

## Configuration

| Variable | Default | |
|---|---|---|
| `REALTIME_ENABLED` | `true` | Off: the stream answers 404, nothing is published, the app polls as before |
| `REALTIME_HEARTBEAT` | `25s` | Idle comment interval; keep it under every proxy's idle timeout |
| `REALTIME_MAX_STREAM` | `30m` | A stream's life; the client reconnects, which re-checks its token |
| `REALTIME_MAX_STREAMS_PER_USER` | `5` | Per user, per server process (429 beyond) |
| `REALTIME_LISTEN_DSN` | `DATABASE_DSN` | The `LISTEN` connection; see below |

The stream is exempt from `HTTP_REQUEST_TIMEOUT` and lifts `HTTP_WRITE_TIMEOUT` for itself only
(`http.ResponseController`), so ordinary requests keep their limits. The request's trace span and
access-log line cover the stream's life; telemetry capture skips streams.

## Running it

**Several servers.** Each `ims serve` holds one `LISTEN` connection and its own streams; any
process publishes to all of them. Nothing else to set up, and no Redis.

**PgBouncer / connection poolers.** `LISTEN` needs a real session. Through PgBouncer in
**transaction** mode it silently receives nothing. Point `REALTIME_LISTEN_DSN` at Postgres
directly (or at a session-mode pool); publishing (`pg_notify`) works through any pooler.

**nginx.** Turn off buffering for the stream and keep the upstream open longer than the heartbeat:

```nginx
location /api/v1/realtime/stream {
    proxy_pass http://ims;
    proxy_http_version 1.1;
    proxy_set_header Connection '';
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 1h;
}
```

The server also sends `X-Accel-Buffering: no`, but the `proxy_buffering off` directive is the
reliable switch. CDNs that cut idle connections (Cloudflare after ~100 s) are covered by the 25 s
heartbeat. Behind HTTP/1.1 a browser allows about six connections per site; the app's one stream
per browser keeps well under it.

**Shutdown and restarts.** On `SIGTERM` the listener closes every stream (`event: reconnect`),
so the server's drain does not wait on them, and clients reconnect to another instance. After the
listener reconnects to Postgres it sends `resync`, because notifications sent meanwhile are lost.

## Testing

`internal/modules/realtime/integration_test.go` runs the listener and a real HTTP server
(`httptest.NewServer(app.Router)`), opens streams for members of two tenants, makes changes
through the services and asserts who hears what. `internal/platform/pubsub` has unit tests for
matching, the per-user limit, dropping on a full buffer and shutdown.
