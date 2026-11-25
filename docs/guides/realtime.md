---
title: Live updates (SSE)
description: How the app stays current — polling as the floor, one Server-Sent Events stream on top — what the stream sends, how to subscribe from any client, and how to give your own feature live updates.
layers: [api, app]
---

The notification bell, the announcement banners and the billing screens update as soon as
something changes, without a reload. Two mechanisms work together:

- **Polling is the floor.** Every screen that shows changing data refetches it on a timer, so it
  is never wrong for long, whatever the network, proxy or server does.
- **One Server-Sent Events stream is the accelerator.** It says *what kind* of thing changed for the
  signed-in account (`notifications`, `announcements`, `billing`) and the app refetches that data
  at once through the normal API. While the stream is up, the timers slow down.

```text
any process (API, worker, scheduler)          ims serve (every instance)               browser
service write ── pg_notify('ims_realtime') ──► pubsub.Listener ─► pubsub.Hub ─► GET /api/v1/realtime/stream
                                                (one LISTEN connection)       event: notifications
                                                                               → refetch the feed
```

Events carry **no data**. Permissions, tenancy, branch scope and read state stay in the endpoints
the app already calls, so there is no second code path to keep in sync and nothing sensitive on
the wire. A missed event costs one poll; an event sent to someone it did not concern costs one
refetch that finds nothing new.

## How the app stays current

| What | Polls (normal → while live) | Refetched at once by |
|---|---|---|
| Notification bell and feed | 60 s → 5 min | `notifications` |
| Announcement banners | 5 min → 15 min | `announcements` |
| Plan, payments, payment approvals | when the screen opens | `billing` |
| Jobs tray and Jobs page | 3 s while a job runs, else 60 s | (no event: progress polls) |

Polls pause while the tab is in the background (`refetchIntervalInBackground: false`). On every
(re)connect the app refetches everything once, because a change made while it was away sent its
event before the stream existed.

The interval for a query comes from one hook, so a screen slows its poll while live updates
arrive:

```ts title="web/app/src/components/NotificationBell.tsx"
const feed = useQuery({
  queryKey: [...feedKey, 'bell'], // starts with feedKey, so a 'notifications' event refetches it
  queryFn: () => fetchFeed({ limit: 8 }),
  refetchInterval: usePollInterval(60_000, 5 * 60_000), // normal, while live
  refetchIntervalInBackground: false,
})
```

**Who gets the stream.** A member's own session. Staff **impersonating** an organization or
**logged in as** a user keep polling: the stream belongs to the account that holds the token, not
to the person being acted as.

## The stream

`GET /api/v1/realtime/stream` with the usual bearer header holds a `text/event-stream` open:

| Frame | Meaning |
|---|---|
| `event: ready` | Connected; `data: {"heartbeat": 25}` (seconds) |
| `event: notifications` | Refetch the notification feed and unread count |
| `event: announcements` | Refetch `GET /api/v1/announcements/live` |
| `event: billing` | Refetch the organization's plan and payments (staff: the payment approvals) |
| `event: resync` | Refetch everything (the server may have missed events) |
| `: ping` | Every `REALTIME_HEARTBEAT` while idle, so proxies keep it open |
| `event: reconnect` | The server is closing the stream (after `REALTIME_MAX_STREAM`, or shutdown): open a new one |

| Answer | When |
|---|---|
| `200` | the stream; it stays open |
| `401` | no or invalid token |
| `404` | `REALTIME_ENABLED=false`: poll only, don't retry |
| `429` | more than `REALTIME_MAX_STREAMS_PER_USER` streams for this account on this server: back off |
| `503` | the server can't subscribe right now: retry with backoff |

## Subscribing from your own client

Any client can use the stream: a mobile app, another frontend, a script. Try it with curl (`-N`
turns off curl's buffering):

```bash
TOKEN=$(curl -s localhost:8080/api/v1/login -H 'Content-Type: application/json' \
  -d '{"email":"superadmin@ims.com","password":"123456","device":"cli"}' | jq -r .data.token)

curl -N localhost:8080/api/v1/realtime/stream -H "Authorization: Bearer $TOKEN"
# event: ready
# data: {"heartbeat":25}
#
# : ping
# event: notifications      ← mark a notification read in the app to see this
```

In a browser, use `fetch` and read the body. `EventSource` cannot send an `Authorization`
header, and the token must never go in a URL (it would land in proxy and access logs). A minimal
reader:

```ts
async function listen(token: string, onEvent: (kind: string) => void, signal: AbortSignal) {
  const response = await fetch('/api/v1/realtime/stream', {
    headers: { Authorization: `Bearer ${token}`, Accept: 'text/event-stream' },
    signal,
  })
  if (!response.ok) throw new Error(`stream: ${response.status}`) // 404: live updates are off
  const reader = response.body!.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let event = ''
  for (;;) {
    const { value, done } = await reader.read()
    if (done) return // closed: reconnect
    buffer += decoder.decode(value, { stream: true })
    let newline: number
    while ((newline = buffer.indexOf('\n')) >= 0) {
      const line = buffer.slice(0, newline).replace(/\r$/, '')
      buffer = buffer.slice(newline + 1)
      if (line === '') {
        if (event === 'reconnect') return
        if (event && event !== 'ready') onEvent(event) // notifications, announcements, billing, resync
        event = ''
      } else if (line.startsWith('event:')) {
        event = line.slice(6).trim()
      }
    }
  }
}
```

Whatever the client, follow the same rules the app does:

1. **Treat an event as "refetch".** Call the endpoint you already use for that data; never expect
   the data in the event.
2. **Refetch everything once on every connect**, and on `resync`.
3. **Reconnect** when the stream ends or sends `reconnect`: at once after a clean close, with
   backoff (1 s doubling to 30 s, plus jitter) after an error, and 30 s after a 429. Stop on 401
   (sign in again) and 404 (live updates off).
4. **Watch for silence.** If no byte arrives for two heartbeats (pings count), the connection is
   dead even if the socket isn't: close it and reconnect.
5. **Keep a slow poll** as the fallback. Mobile apps that go to the background should close the
   stream and refetch when they come back.

## How the app's client works

`web/app/src/components/realtime.ts` does all of the above once for the whole app.
`<RealtimeConnector />` is mounted in the signed-in shell and renders nothing:

- **One stream per browser, per user.** Tabs elect a leader with the Web Locks API; only the
  leader opens the stream and relays each event and its status to the other tabs over a
  `BroadcastChannel`. When the leader tab closes, the next one in line takes over. Without Web
  Locks, every tab streams for itself.
- **Events invalidate queries.** A table maps each kind to the TanStack Query keys it refetches;
  `resync` refetches them all:

  ```ts title="web/app/src/components/realtime.ts"
  const invalidates: Record<string, QueryKey[]> = {
    notifications: [feedKey],
    announcements: [['announcements', 'live']],
    billing: [['subscription-status'], ['billing'], ['payments'], ['console', 'approvals']],
  }
  ```

- **Status** (`off`, `connecting`, `live`, `offline`) is shared by `useRealtimeStatus()`;
  `usePollInterval(normal, whileLive)` reads it.

## Adding live updates to your feature

Say the products module from the [tutorial](../tutorial/first-module.mdx) should refresh the
Products list for everyone in the organization when someone adds or changes a product. Four
steps: name a kind, publish it from the service, wire the publisher, refetch it in the app.

### 1. Name the kind

One short lowercase name per thing the client refetches. Keep it next to the service:

```go title="internal/modules/product/realtime.go"
package product

import (
	"context"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/pubsub"
)

// RealtimeKind is the live-update event the app answers by refetching the
// products list (docs/guides/realtime.md).
const RealtimeKind = "products"

// WithPublisher tells open streams when products change. Without one, the
// app finds changes by polling.
func (s *Service) WithPublisher(publisher pubsub.Publisher) *Service {
	s.publisher = publisher
	return s
}

// publish never fails the write it reports: polling still finds the change.
func (s *Service) publish(ctx context.Context, tenantID int64) {
	if s.publisher == nil {
		return
	}
	if err := s.publisher.Publish(ctx, pubsub.ToTenant(RealtimeKind, tenantID)); err != nil {
		slog.WarnContext(ctx, "products: live update not published", "tenant_id", tenantID, "error", err)
	}
}
```

Add `publisher pubsub.Publisher` to the `Service` struct (and `"log/slog"` to the imports).

### 2. Publish after the write

```go title="internal/modules/product/service.go" ins={7}
func (s *Service) Create(ctx context.Context, scope tenancy.Scope, actorID int64, input CreateInput) (*Product, error) {
	// … validation and the insert …
	product, err := s.repo.Create(ctx, input)
	if err != nil {
		return nil, err
	}
	s.publish(ctx, product.TenantID)
	return product, nil
}
```

Do the same in `Update` and `Delete`. The rules:

- **Publish after the write succeeds**, never before. `NOTIFY` is delivered on commit, so a
  publisher built on a transaction's queries sends nothing if it rolls back.
- **Publishing never fails the write**: log and move on.
- **Choose the audience** the change is for, and prefer a slightly wider one over re-implementing a
  read's matching rules. An event is a nudge, not a delivery:

| Audience | Event |
|---|---|
| named users (their other tabs and devices) | `pubsub.ToUsers(kind, id1, id2, …)` |
| every member of one organization | `pubsub.ToTenant(kind, tenantID)` |
| staff accounts (no organization) | `pubsub.ToTenant(kind, 0)` |
| an organization **and** staff | `pubsub.Event{Kind: kind, TenantIDs: []int64{id}, Platform: true}` |
| every organization | `pubsub.Event{Kind: kind, AllTenants: true}` |

- **Keep the payload small.** It must fit in Postgres's 8 KB `NOTIFY` limit; `Publish` returns
  `pubsub.ErrTooLarge` beyond it. Name the organization instead of listing thousands of users
  (notifications switch over at 500).
- **Changes on a clock** (something starting at 9:00) have no write to publish from: publish from
  a scheduled task, as `announcements.publish-transitions` does every minute.

### 3. Wire the publisher

`internal/app/platform.go` builds one publisher for the whole process: `pg_notify` through the
database, or `pubsub.Discard` when `REALTIME_ENABLED=false`. Hand it to your service where you
build it:

```go title="internal/app/wiring.go" ins={2}
product.NewService(product.NewRepository(queries)).WithActivity(platform.Activity).
	WithPublisher(platform.Realtime),
```

Any process can publish (API, worker, scheduler); every `ims serve` instance hears it.

### 4. Refetch in the app

Map the kind to the query keys it should refetch, so the stream (and `resync`) reaches your
screen:

```ts title="web/app/src/components/realtime.ts" ins={4}
const invalidates: Record<string, QueryKey[]> = {
  notifications: [feedKey],
  announcements: [['announcements', 'live']],
  products: [['products']], // every query whose key starts with 'products'
  billing: [/* … */],
}
```

The key is a prefix: `['products']` refetches `['products', query, page]` and any other products
query. If the screen should also poll (a list people leave open), give its query a poll interval
that slows down while live:

```ts
refetchInterval: usePollInterval(60_000, 10 * 60_000),
```

Open the Products screen in two browsers signed in as two members of the same organization, add
a product in one, and it appears in the other within a second.

### 5. Test who hears it

`internal/modules/realtime/integration_test.go` runs the real listener and HTTP server against a
test database, opens streams for members of two organizations and asserts who hears what. Copy
its `live`, `stream`, `expect` and `waitListening` helpers into your module's integration test:

```go
app := testkit.New(t)
server := live(t, app) // the listener and a real HTTP server, as `ims serve` runs them
member := app.User(t, testkit.Role("Admin"))
outsider := app.User(t, testkit.Role("Admin"), testkit.InTenant(app.Tenant(t, "Other Org")))

memberEvents := stream(t, server, member.Token)
outsiderEvents := stream(t, server, outsider.Token)
waitListening(t, app, member.ID, memberEvents)
waitListening(t, app, outsider.ID, outsiderEvents)

app.As(member).Post("/api/v1/product", `{"name":"Pen","sku":"PEN-1","price":1250,"stock":0}`).
	Status(t, http.StatusCreated)
expect(t, memberEvents, product.RealtimeKind) // the organization heard it
// the other organization did not: its next event is a probe
```

## What publishes today

| Change | Process | Who hears it | Kind |
|---|---|---|---|
| Notification fan-out finished (unread counts written) | worker | the addressed users (their organization beyond 500) | `notifications` |
| Notification created with no fan-out, updated or deleted | API | its organization (staff for tenantless ones) | `notifications` |
| Mark read / mark all read | API / worker | that user (their other tabs and devices) | `notifications` |
| Announcement created, updated or deleted while live | API | its organizations (before and after an edit) | `announcements` |
| Announcement starts or ends on schedule | scheduler | its organizations | `announcements` |
| Payment submitted, approved, rejected or withdrawn | API | the organization and staff | `billing` |
| The listener reconnected to Postgres | API | everyone | `resync` |

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
(`httptest.NewServer(app.Router)`), opens streams for members of two organizations, makes changes
through the services and asserts who hears what. `internal/platform/pubsub` has unit tests for
matching, the per-user limit, dropping on a full buffer and shutdown.
