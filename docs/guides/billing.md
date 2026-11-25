---
title: Billing and plans
description: Packages with features and limits, subscriptions, plan changes with prorated upgrades, renewals, account credit and refunds, payments with bKash, cash and bank transfer, staff assignment and overrides.
layers: [database, api, app]
---

Billing answers one question for the rest of the code: **may this tenant do this?** Packages
(plans) carry features and limits, a tenant subscribes to a package, and resource services ask
the billing **guard** before they create anything.

```text
package ──has──► package_features (feature_key, is_enabled, limit_value)
   ▲
   │ package_id
subscription (tenant_id, status, expires_at, overrides) ──► payments ──► approval requests
                                                              billing_credits (account credit)
```

Everything is stored in Postgres (`packages`, `package_features`, `subscriptions`, `payments`,
`payment_approval_requests`, `billing_credits`). Billing **management** — editing plans, approving payments — belongs
in your product's admin app, which calls the endpoints below; the developer dashboard only shows
each tenant's plan and usage for debugging.

## Features and limits

The catalog of features is in Go — `internal/modules/billing/domain/features.go`:

| Kind | Features |
| --- | --- |
| **Limits** (numbers) | `users.max`, `branches.max`, `storage.mb` |
| **Switches** (booleans) | `patients.manage`, `appointments.manage`, `invoices.manage`, `reports.basic`, `reports.advanced`, `reports.export`, `sms.notifications`, `api.access`, `audit.log`, `custom.branding` |

A limit of `-1` means unlimited. Resolution: the subscription's **overrides** → the package's
features → denied.

## Enforcing a limit

Resource services depend on a one-method interface, not on billing:

```go
type LimitChecker interface {
	// QuotaFor reads the plan limit (before any lock) and returns a check
	// that only compares numbers.
	QuotaFor(ctx context.Context, tenantID int64, feature string) (func(current int64) error, error)
}
```

`users.max` and `branches.max` are enforced by identity and branches under a per-tenant advisory
lock (see [Database → Quotas](./database.md#quotas-withtenantlock)); `storage.mb` by the files
service on uploads. A refusal is a **402**:

```json
{ "error_code": "USAGE_LIMIT_EXCEEDED", "status": 402,
  "meta": { "feature_key": "users.max", "limit": 5, "current": 5 } }
```

…or `SUBSCRIPTION_REQUIRED` when the tenant has no active plan. For a boolean feature, call
`guard.Require(ctx, tenantID, domain.FeatureReportsExport)`.

## Subscriptions

Statuses: `trial`, `pending`, `scheduled`, `active`, `past_due`, `cancelled`, `expired`. Only
**active** and **trial** grant access, plus a **scheduled** one once its `starts_at` has passed (the
hourly task then makes it active).

| Action | How |
| --- | --- |
| price a change first | `GET /api/v1/billing/change-preview?package_id=…` (`billing.manage`): see [Changing plans](#changing-plans) |
| tenant subscribes, renews or changes plan | `POST /api/v1/billing/subscribe` `{package_id, payment_method, payment_data}` (`billing.manage`) |
| stop the renewal, or undo it | `POST /api/v1/billing/subscription/cancel` and `…/keep` (`billing.manage`) |
| account credit | `GET /api/v1/billing/credits` (`billing.view`) |
| tenant payment history | `GET /api/v1/billing/payments` (`billing.view`) |
| see the current plan | `GET /api/v1/billing/subscription/status` |
| withdraw a checkout nobody confirmed yet | `POST /api/v1/billing/payments/{id}/cancel` (`billing.manage`): the payment and its pending subscription are cancelled, the approval request closed as withdrawn |
| download an invoice | `GET /api/v1/billing/payments/{id}/invoice.pdf` (`billing.view`): a one-page PDF under the platform's name; `…/invoice` is the same as a printable HTML page |
| see the use of each plan limit | `GET /api/v1/billing/usage` (`billing.view`): `users.max`, `branches.max` and `storage.mb` with the limit (-1 unlimited, 0 not in the plan) and what is used, counted as the limit checks count |
| staff assigns a plan | `POST /api/v1/billing/internal/tenants/{tenant_id}/subscription` |
| staff cancels | `POST /api/v1/billing/internal/subscriptions/{id}/cancel` — immediate |
| staff refund | `POST /api/v1/billing/internal/payments/{id}/refund` (`payment.refund`) |
| staff credit | `GET`/`POST /api/v1/billing/internal/tenants/{tenant_id}/credits` (`payment.view.all` / `payment.refund`) |
| expiry, scheduled plans start | the `billing.expire-subscriptions` task, hourly |
| renewal reminders | `billing.send-renewal-reminders`, daily at 08:00, at 7/3/1 days (not for a plan that will not renew or whose next period is paid) |

**Subscribe** is idempotent per tenant + package + method: while a checkout is pending, a retry
or double click returns the same checkout instead of charging twice (a per-tenant lock serialises
it). **Only one checkout waits at a time**: another plan or method answers 409 with
`meta.reason: CHECKOUT_PENDING` and the waiting `payment_id`; withdraw it first. Activating a
subscription deactivates the previous one in the same transaction.

### Changing plans

Every change is priced by one pure function (`domain.QuoteChange`,
`internal/modules/billing/domain/proration.go`), shown by `GET /billing/change-preview` and frozen
on the payment at checkout (`payments.kind`, `lines`, `gross`), so a slow bank-transfer approval
never changes the amount. The rules follow what Stripe, Paddle and Claude do:

| Change | When | What is paid now |
| --- | --- | --- |
| **New plan** (none, a trial of another plan, a free plan) | when paid | the full price |
| **Upgrade**: costs more per day, or a longer cycle (monthly → yearly) | when paid; a fresh period from that day | price − **unused credit** of the current plan |
| **Renewal** of the same plan (or of the plan chosen for the renewal) | when the current period ends (`scheduled`) | the full price; no days are lost |
| **Downgrade**: costs less per day | when the current period ends | nothing: the plan is remembered (`next_package_id`) and paid at the renewal |
| **Free plan** while on a paid one | when the current period ends (`scheduled`) | nothing |

- **Unused credit** = what was actually paid for the current period (completed payments' `gross`
  less refunds; trials, free plans and staff comps give 0) × unused days ÷ period days. Unused
  days are whole days rounded up, in the customer's favour; money rounds half up. Credit above the
  new price becomes **account credit**.
- **Account credit** pays first: it is reserved at checkout (a negative `billing_credits` row) and
  given back if the payment is withdrawn, rejected or fails. When it covers everything, no method
  is needed: the plan starts at once with a `credit` payment of 0.
- A paid renewal starts when the current period ends and keeps the current **overrides**; any other
  change starts with none (the approval screen says how many are dropped).
- A hidden (inactive) plan can only be renewed by an organization already on it.
- **Stop the renewal** (`…/subscription/cancel`): the plan runs to `expires_at` and ends; a period
  paid in advance becomes account credit. `…/subscription/keep` undoes it, and also undoes a chosen
  downgrade or a scheduled free plan.

### Account credit and refunds

Nothing is refunded automatically. Staff refund a completed payment with
`POST /billing/internal/payments/{id}/refund` `{amount, reason, to, end_subscription}`: `to: credit`
adds it to the account credit, `to: original` refunds through the gateway (bKash) or records that
cash or bank money was paid back by hand. A payment becomes `refunded` when fully refunded; invoices
show the refunded amount. `POST /billing/internal/tenants/{id}/credits` `{amount, reason}` adds
credit (goodwill) or takes it back (negative, never below zero). Every change is in the
organization's activity log.

When approving a bank or cash payment, staff can give the **amount received**: more than the
invoice becomes account credit, less is refused (reject it, or ask for the rest). An approval whose
credited plan is no longer the one in force shows `quote_stale`.

### Staff assignment

Support, sales deals and complimentary accounts use the assign endpoint (`subscription.manage`):

```json
POST /api/v1/billing/internal/tenants/7/subscription
{ "package_id": 2, "payment_method": "cash", "amount": 150000 }
```

It ends the current subscription and starts an **active** one in one transaction. With
`payment_method` (`cash` or `bank_transfer`) it records a completed payment — `amount` defaults
to the package price; without it the plan is free of charge. Hidden (inactive) packages can be
assigned. With `"mode": "change"` staff apply the organization's own rules instead (unused credit,
account credit, downgrades and renewals at the end of the period), and the payment, which defaults
to the amount due, is recorded as received.

### Overrides

Per-tenant exceptions live on the subscription (`subscription.override`):

```json
POST /api/v1/billing/internal/subscriptions/12/overrides
{ "overrides": { "users.max": 25, "reports.export": true, "branches.max": null } }
```

Values merge in; `null` removes an override so the feature falls back to the plan. A new
subscription starts with none.

## Payments

An **orchestrator** picks a driver per method:

| Method | Flow |
| --- | --- |
| `bkash` | tokenized checkout; the token is cached 55 minutes and dropped on 401/403 |
| `cash`, `bank_transfer` | a pending payment + a **staff approval request** |
| `nagad` | stub, ready for a real driver |

- **Callbacks** (`GET /api/v1/billing/{gateway}/callback`) are public and only a *hint*: bKash
  is always asked for the real status. Only a status bKash reports as failed or cancelled fails a
  payment; a gateway error is a retryable 503 that changes nothing. Completion and activation are
  one transaction.
- **Free packages** start at once when an organization subscribes to one (no payment method,
  no approval), the way staff assign a plan.
- **Feature catalog**: `GET /api/v1/packages/features` (`package.view.all`) lists every feature
  with `is_limit`, for package editors.
- **Bank details** for transfers are the global setting `billing.payment_instructions` (group
  **Billing**): bank, account name and number. Clients show it at checkout; members read it from
  `GET /api/v1/settings?group=billing`.
- **Approvals**: `GET …/payment-approvals?status=pending` lists them with `details` (organization,
  plan, amount, currency, invoice) so a reviewer needs no second lookup;
  `POST …/payment-approvals/{id}/approve|reject` update the payment and the
  subscription atomically.
- **Invoices** are `INV-XXXX-timestamp` from `crypto/rand`; cash and bank payments get
  unguessable `CASH-…` / `BANK-…` references. The PDF (`codeberg.org/go-pdf/fpdf`, pure Go) uses
  the standard PDF fonts, so amounts show the currency code and characters outside Western
  European scripts print as `?`; embed a TrueType font in `service/invoice_pdf.go` to change that.
- **Who hears about a payment** (`internal/app/billing_notices.go`, billing's `PaymentNotifier`):

  | When | Who | How |
  | --- | --- | --- |
  | a bank-transfer or cash checkout is submitted | staff holding `payment_approval.review` (Super-Admins included) | in-app "Payment to review" (opens the approvals page) + mail |
  | the same | the organization's members holding `billing.manage`, and the submitter | in-app "Payment submitted" + mail |
  | it is approved, or a gateway payment completes | the same members | in-app "Payment received" + the `payment-receipt` mail with the invoice PDF attached |
  | it is rejected | the same members | in-app "Payment not confirmed" with the reviewer's note + mail |

  Every change also sends a `billing` live update, so the organization's billing page and the
  staff approval list refresh on their own (`docs/guides/realtime.md`). A notice that cannot go
  out is logged; it never fails the payment.
- Payment statuses only move along allowed transitions (pending → any, failed → completed,
  completed → refunded).

## Packages

`ims db:seed` creates a hidden **Unlimited** package (every feature on, every limit `-1`) and
subscribes the default tenant to it when that tenant has no active plan, so a fresh install can
add users right away; it is never listed publicly and seeding again changes nothing.

`GET /api/v1/public/packages` lists active packages for your pricing page (no auth). Staff manage
packages with `/api/v1/packages` (`package.*`); updating a package **replaces** its feature set
in one transaction. Prices are integers in the minor units of the default currency, and each
payment keeps the currency it was made in; see [Money and currency](currencies.md).

## In the app

Organizations see their plan, usage, account credit and payments under **Billing** (`/org/billing`) and check out from there: choosing a plan shows the priced change (lines, total, when it starts) before paying, **Renew** appears in the last half of the period and **Don't renew** stops the renewal; a 402 from any action explains the limit and links to it. Staff manage the plan catalog under **Plans** and decide bank and cash payments under **Payment approvals**. See [Organization workspace](../app/organization-workspace.md#plan-limits) and [Staff console](../app/staff-console.md#payments-and-plans).
