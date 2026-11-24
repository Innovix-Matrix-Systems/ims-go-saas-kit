---
title: Webhooks
description: Send events to your customers' systems — subscribe a URL, Standard Webhooks signatures, retries and auto-disable, the delivery log, SSRF protection and how to add your own events.
---

A tenant subscribes a URL to events; every matching event is POSTed to it, **signed**, **retried**
for about a day and **logged**. The events come from the [activity log](activity-log.md), so any
module that records activity can be a webhook source with one line.

## Subscribing

`webhook.manage` (in the tenant Admin role), and the plan's `api.access`:

```http
POST /api/v1/webhook  { "url": "https://crm.acme.test/hooks/ims", "events": ["user.created"], "description": "CRM sync" }
→ 201 { "id": 4, "secret": "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw", "is_active": true, … }
```

The secret is shown **once**. `"events": ["*"]` subscribes to every event.
`GET /api/v1/webhook/events` lists what can be subscribed to.

| Endpoint | Does |
| --- | --- |
| `GET /api/v1/webhook`, `GET/PATCH/DELETE /api/v1/webhook/{id}` | list, show, change (`url`, `events`, `description`, `is_active`), delete |
| `POST /api/v1/webhook/{id}/rotate-secret` | a new secret (shown once); the old one also signs for 24 h |
| `POST /api/v1/webhook/{id}/test` | send a `webhook.test` event now and return the answer |
| `GET /api/v1/webhook/{id}/deliveries` | the delivery log: status, attempts, the endpoint's status and first 4 KB of its answer |
| `POST /api/v1/webhook/deliveries/{id}/redeliver` | send one again now (same event id) |

## What the receiver gets

```http
POST /hooks/ims HTTP/1.1
Content-Type: application/json
User-Agent: ims-go-admin-starter-Webhooks/0.1.0
webhook-id: evt_9f2c1a7b4e6d4f0a8c3b2d1e0f9a8b7c
webhook-timestamp: 1795340000
webhook-signature: v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE=

{"id":"evt_9f2c…","type":"user.created","timestamp":"2026-11-22T10:00:00Z","tenant_id":7,
 "data":{"subject":"user","subject_id":42,"actor_id":3,"changes":{"name":"Nadia Islam","email":"nadia@acme.test"}}}
```

The signature follows [Standard Webhooks](https://www.standardwebhooks.com): base64 of
HMAC-SHA256 over `id.timestamp.body`, keyed with the base64 part of the secret. Use one of its
libraries, or:

```go
// Go receiver.
func verify(secret string, r *http.Request, body []byte) bool {
	key, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(r.Header.Get("webhook-id") + "." + r.Header.Get("webhook-timestamp") + "." + string(body)))
	want := "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
	for _, signature := range strings.Fields(r.Header.Get("webhook-signature")) {
		if hmac.Equal([]byte(signature), []byte(want)) {
			return true
		}
	}
	return false
}
```

```js
// Node receiver (raw body, before JSON parsing).
const crypto = require('node:crypto')
function verify(secret, headers, rawBody) {
  const key = Buffer.from(secret.replace(/^whsec_/, ''), 'base64')
  const signed = `${headers['webhook-id']}.${headers['webhook-timestamp']}.${rawBody}`
  const want = 'v1,' + crypto.createHmac('sha256', key).update(signed).digest('base64')
  return headers['webhook-signature'].split(' ').some((s) => s.length === want.length &&
    crypto.timingSafeEqual(Buffer.from(s), Buffer.from(want)))
}
```

Reject requests whose `webhook-timestamp` is more than a few minutes old (replays). During a
secret rotation the header carries two signatures; accept either. Delivery order is **not**
guaranteed: dedupe on `id` and order by `timestamp`.

## Retries and failures

A 2xx answer within `WEBHOOK_TIMEOUT` (default `10s`) is a success. Anything else is retried
after 1 m, 5 m, 30 m, 2 h, 5 h, 10 h and 10 h: eight attempts over about a day, then the delivery
is **abandoned**. After `WEBHOOK_DISABLE_AFTER` (default `5`) abandoned deliveries in a row the
endpoint is **disabled** and the person who created it is notified (in the feed and by mail).
Fix the receiver, then `PATCH … {"is_active": true}`: the failure count starts over. Redirects are
not followed.

Delivery logs are kept for `WEBHOOK_RETENTION_DAYS` (default `30`; the `webhook.prune` task).

## SSRF protection

Outside local development, endpoints must use **https** and may not point at private, loopback,
link-local (cloud metadata) or unique-local addresses, including IPv6 addresses that carry one of
those through a NAT64 gateway or a 6to4 relay. The check runs on the address the client actually
connects to, so a hostname that resolves to an internal address later (DNS rebinding) is refused
too. `WEBHOOK_ALLOW_PRIVATE=true` (the default in local development) lifts it so you can
test against `http://localhost`.

## Adding an event

An activity action becomes deliverable once registered. In the module that records it:

```go
webhooks.RegisterEvent("invoice.paid", "An invoice was paid.")
// … and where the change happens, as for any activity row:
s.activity.Record(ctx, activity.Entry{TenantID: t, Action: "invoice.paid", Subject: "invoice", SubjectID: id, Changes: …})
```

The payload's `data.changes` are the activity entry's changes, already redacted (secrets never
leave). The starter ships `user.created` and `branch.updated` as examples, plus `webhook.test`.
Sign-ins, impersonation and dashboard actions are never deliverable: only registered actions are.

Secrets are encrypted with `APP_KEY` (see [deploy](../operations/deploy.md)).
