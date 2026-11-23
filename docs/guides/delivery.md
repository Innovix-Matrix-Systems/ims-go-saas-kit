---
title: Delivery (mail, SMS, push)
description: One way to reach a person — templates in plain text or HTML, English and Bangla, mail, SMS and push channels, the queue, retries, the delivery log and how to add your own template or channel.
---

Every feature that contacts a person — one-time codes, password resets, e-mail verification and
whatever your product adds — goes through one service, `delivery.Service`. Mail is one channel
of it; SMS is another; push is ready for a provider. You write a template once, pick the
channels, and the delivery system renders it in the recipient's language, queues one job per
channel, retries each on its own and records the outcome.

```go
err := platform.Delivery.Send(ctx, delivery.Message{
	To:       delivery.Recipient{UserID: user.ID, TenantID: user.TenantID, Name: user.Name, Email: user.Email, Phone: user.Phone},
	Template: "order-shipped",
	Data:     map[string]any{"order": order.Number, "url": trackingURL},
	Channels: []delivery.Channel{delivery.Mail, delivery.SMS},
})
```

## What happens on Send

1. **Render to validate.** Every channel is rendered once, so a missing template, file or data
   key is an error here, not a failed job later.
2. **Log.** One row per channel in the delivery log (`deliveries`): template, channel, a
   **masked** address (`j***@acme.test`, `+88017******78`) and `queued`. A channel the recipient
   has no address for is logged as `skipped` and nothing is queued.
3. **Queue.** One `delivery.send` job per channel. A slow SMS provider never delays the mail, and
   each channel retries on its own (the `default` queue's retries).
4. **Send.** The job renders again (a template fix applies to retries), sends through the
   channel's driver and marks the row `sent` (with the provider's message id) or `failed` (with
   the reason).

The log never stores a body or the template data, so one-time codes and reset links never reach
the database, the dashboard's queue panel (payloads are hidden) or the logs (drivers log bodies
only with `APP_DEBUG=true` outside production). `DELIVERY_RETENTION_DAYS` (default `30`) prunes
it daily; `ims delivery:prune` does it by hand.

`SendNow` delivers before returning, for the rare caller that must know the result
(`ims delivery:test` uses it).

## Templates

A template is a folder. Each file is optional except what its channels need:

```text
templates/order-shipped/
├── subject.txt    mail subject (one line)
├── mail.txt       plain-text mail body: enough on its own
├── mail.html      optional HTML body, wrapped in the shared layout
├── sms.txt        SMS body (keep it under 320 characters)
├── push.json      {"title": "…", "body": "…"}
└── sample.json    data for the dashboard preview
```

- **Plain text works alone.** With only `subject.txt` and `mail.txt` the mail is plain text.
  Add `mail.html` and it becomes multipart (text + HTML), wrapped in the layout.
- **Languages.** A locale variant sits next to its file: `mail.bn.txt` is used for Bangla,
  `mail.txt` for everything else. The locale is the recipient's `Locale`, then the request's
  `Accept-Language`, then `APP_FALLBACK_LOCALE`.
- **What a template sees:** `.To` (the recipient: `.To.Name`, `.To.Email`…), `.Data` (your data)
  and `.Brand` (`.Brand.Name`, `.Brand.Color`…). A missing data key is an error, so a typo fails
  at `Send` instead of mailing `<no value>`.
- **Escaping.** HTML files use `html/template`: data is escaped. Text files use `text/template`.
- **`{{duration .Data.minutes}}`** spells a number of minutes in the file's language:
  `90` → `1 hour 30 minutes` / `১ ঘণ্টা ৩০ মিনিট`.

**Raw messages** need no template: give `Subject` and `Text` (and optionally `HTML`, wrapped in
the layout) instead of `Template`. The log shows them as `raw`.

**Built-in templates:** `otp-code` (mail and SMS), `password-reset`, `email-verify`, in English
and Bangla. They live in `internal/platform/delivery/templates/`.

### Adding a template

Put the folder in your module and register it once at startup:

```go
//go:embed templates
var templates embed.FS

sub, _ := fs.Sub(templates, "templates")
if err := platform.Delivery.Templates().Register(sub); err != nil { … }
```

Registering a folder with the name of a built-in replaces it, so you can restyle the OTP mail
without touching the starter's code. A `_layout/mail.html` folder replaces the layout.

## Branding

The mail layout shows a name, a logo, an accent colour and a footer, from the **Branding**
settings group:

| Setting | Default |
| --- | --- |
| `branding.name` | empty: `system.name` |
| `branding.logo_url` | empty: the name as text |
| `branding.color` | `#2563eb` (must be `#rrggbb`) |
| `branding.footer` | empty: `© <name>` |

The platform values apply to everyone. A tenant's own values apply only when its plan has the
`custom.branding` feature, so white-labelled mail is a plan upgrade.

## Channels and drivers

| Channel | Address | Drivers |
| --- | --- | --- |
| `mail` | `Recipient.Email` | `MAIL_DRIVER=log` (default) or `smtp` |
| `sms` | `Recipient.Phone` (E.164) | `SMS_DRIVER=log` (default) or `twilio` |
| `push` | `Recipient.UserID` | log only, until a provider is added |

Mail extras: `CC`, `BCC`, `ReplyTo` and `Attachments` — files on a storage disk
(`{Disk, Path, Name, ContentType}`), read when the mail is sent and capped by
`MAIL_MAX_ATTACHMENT_MB` (default `10`). Subjects and names are RFC 2047 encoded, so Bangla
arrives intact.

```bash
# SMTP (Mailpit locally: MAIL_HOST=127.0.0.1 MAIL_PORT=1025)
MAIL_DRIVER=smtp
MAIL_HOST=smtp.example.com
MAIL_PORT=587
MAIL_ENCRYPTION=starttls        # tls | starttls | "" (plain)
MAIL_USERNAME=…
MAIL_PASSWORD=…
MAIL_FROM_ADDRESS=hello@example.com
MAIL_FROM_NAME="Acme"

# Twilio
SMS_DRIVER=twilio
TWILIO_ACCOUNT_SID=AC…
TWILIO_AUTH_TOKEN=…
TWILIO_FROM=+15005550006        # a Twilio number or a messaging service SID
```

### Adding a channel driver

A driver is a `delivery.Sender`:

```go
type Sender interface {
	Send(ctx context.Context, message delivery.Message, content delivery.Content) (providerID string, err error)
	Name() string
}
```

Set it in `internal/app/delivery.go` with `WithSender(delivery.Push, fcmSender{…})`. Callers do
not change: a message that asked for `push` simply starts arriving on phones.

## Trying it

```bash
ims delivery:test --to=you@example.com                                    # a plain test mail
ims delivery:test --to=you@example.com --template=password-reset --locale=bn
ims delivery:test --channel=sms --to=+8801712345678 --template=otp-code
```

The developer dashboard has a **Deliveries** page: the log with filters (channel, status,
template) and a **Templates** tab that renders every template for each channel and language with
its `sample.json`, without sending anything. `delivery:test` and `delivery:prune` are also in the
Commands panel.

## Account mails

`authn` sends one-time codes and recovery links through the delivery system: the OTP mail uses
`otp-code`, the reset link `password-reset`, the verification link `email-verify`, each in the
user's language. `authn` depends on a small `authn.Deliverer` interface; the adapter lives in
`internal/app/delivery.go`, because the delivery package depends on the queue, which depends on
tenancy and so on `authn`.

The old `mail.send` job and `mail.Enqueue` still work for code written before the delivery
system; new code should call `delivery.Service`.
