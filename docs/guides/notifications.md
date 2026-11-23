---
title: Notifications
description: Notifications targeted at users, roles, branches, tenants or everyone — in the in-app feed, and by mail, SMS or push when you ask — with per-user preferences and one-click unsubscribe.
---

Notifications are in-app messages — "Your import finished", "Subscription expires in 3 days".
They are stored once and **fanned out** to their audience in the background.

## Creating one

Platform staff create notifications through the internal API (`notification.create`):

```json
POST /api/v1/internal/notification
{
  "event_type": "maintenance.scheduled",
  "priority": "WARNING",
  "title": "Maintenance tonight",
  "body": "The API will be read-only from 01:00 to 01:30.",
  "data": { "window": "01:00-01:30" },
  "group_key": "maintenance-2026-11-21",
  "expires_at": "2026-11-22T00:00:00Z",
  "targets": [{ "type": "TENANT", "id": 7 }, { "type": "ROLE", "id": 3 }],
  "channels": ["mail"]
}
```

| Field | Meaning |
| --- | --- |
| `priority` | `INFO`, `WARNING` or `CRITICAL` |
| `targets` | one or more of `USER`, `ROLE`, `BRANCH`, `TENANT` (with an `id`) or `GLOBAL` |
| `group_key` | collapses repeats of the same event |
| `expires_at` | hidden after this; purged 7 days later by `notifications.purge-expired` |
| `channels` | the feed is always on; add `mail`, `sms` or `push` to deliver it too ([below](#mail-sms-and-push)) |

From code, call the notifications service — billing does this for renewal reminders. Fan-out runs
as a queue job and is idempotent (`INSERT … ON CONFLICT … RETURNING`), so a retried job never
double-counts.

The `notification.enabled` [setting](./settings.md) turns fan-out off per tenant.

## Event types

`event_type` must be registered. A module registers its events once (at startup), saying which
channels they may use and whether users may opt out:

```go
notifications.RegisterEvent("maintenance.scheduled", notifications.Event{
	Description: "Planned maintenance.",
	Channels:    []delivery.Channel{delivery.Mail, delivery.Push},
})
notifications.RegisterEvent("billing.payment_failed", notifications.Event{
	Description: "A payment failed.",
	Channels:    []delivery.Channel{delivery.Mail},
	Mandatory:   true, // users cannot turn it off
})
```

The starter ships `renewal.reminder` (mail and SMS; billing sends it by mail) and
`platform.announcement` (mail) for staff announcements.

## Announcements to organizations

Every notification belongs to one organization, and a `GLOBAL` target reaches platform staff
only. To tell organizations something (planned maintenance, a policy change), staff call
`POST /api/v1/internal/notification/broadcast` (`notification.create`) with a `priority`,
`title`, optional `body` and `channels`, and optional `tenant_ids`: it creates one
`platform.announcement` per organization, addressed to all its members, and answers how many
organizations it reached. Without `tenant_ids` it reaches every active organization.
`GET /api/v1/internal/notification?event_type=platform.announcement` lists what was sent.
An organization-wide notification reaches that organization's members, and staff acting in it;
a staff account's own feed (`/internal/notification/feed`) never shows them.

## Mail, SMS and push

With `channels`, fan-out also hands the notification to the [delivery system](delivery.md)
(template `notification`: the title, the body, an **Open** button for `data.url`, and an
unsubscribe link) for each user it **newly** addresses, so a retried job never sends twice. Per
user and channel:

- a user who turned the channel off for the event type is skipped (unless the event is
  mandatory);
- mail needs a verified e-mail address, SMS a verified phone (`phone_verified_at`);
- SMS needs the tenant's plan to have `sms.notifications`, and is capped at
  `SMS_DAILY_LIMIT_PER_TENANT` per tenant per day (default `500`, `0` = no cap);
- push goes to the log until a push provider is added to the delivery system.

Each message is a row in the delivery log (template `notification`), visible in the developer
dashboard's **Deliveries** page.

### Preferences and unsubscribing

| Endpoint | Does |
| --- | --- |
| `GET /api/v1/notification/preferences` | every event type × channel it may use, with the caller's choice (`mandatory` ones are always on) |
| `PUT /api/v1/notification/preferences` | `{"preferences": [{"event_type": "renewal.reminder", "channel": "sms", "enabled": false}]}` |

Notification mails carry a one-click unsubscribe link (and `List-Unsubscribe` /
`List-Unsubscribe-Post` headers, so mail clients show their own button):
`GET` or `POST /api/v1/notification/unsubscribe?token=…` turns off every extra channel of that
event type for that user. The token is sealed with `APP_KEY`, so it can be neither read nor
forged. The in-app feed keeps the notifications either way.

## Reading the feed

| Endpoint | Does |
| --- | --- |
| `GET /api/v1/notification/feed?limit=20&cursor=…` | the caller's notifications, newest first, with unread count |
| `POST /api/v1/notification/{id}/read` | mark one read |
| `POST /api/v1/notification/read-all` | mark all read (scoped and idempotent) |

Platform accounts use the same endpoints (preferences included) under
`/api/v1/internal/notification/…`. Reads are
**user-scoped** — never branch-scoped — so moving branches doesn't hide notifications. Unread
counters are cached and recomputed from the database on a cache miss.
