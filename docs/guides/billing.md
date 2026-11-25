---
title: Billing and plans
description: Packages with features and limits, subscriptions, payments with bKash, cash and bank transfer, staff assignment and overrides.
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
```

Everything is stored in Postgres (`packages`, `package_features`, `subscriptions`, `payments`,
`payment_approval_requests`). Billing **management** — editing plans, approving payments — belongs
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

Statuses: `trial`, `pending`, `active`, `past_due`, `cancelled`, `expired`. Only **active** and
**trial** grant access.

| Action | How |
| --- | --- |
| tenant subscribes | `POST /api/v1/billing/subscribe` `{package_id, payment_method, payment_data}` (`billing.manage`) |
| tenant payment history | `GET /api/v1/billing/payments` (`billing.view`) |
| see the current plan | `GET /api/v1/billing/subscription/status` |
| withdraw a checkout nobody confirmed yet | `POST /api/v1/billing/payments/{id}/cancel` (`billing.manage`): the payment and its pending subscription are cancelled, the approval request closed as withdrawn |
| download an invoice | `GET /api/v1/billing/payments/{id}/invoice.pdf` (`billing.view`): a one-page PDF under the platform's name; `…/invoice` is the same as a printable HTML page |
| see the use of each plan limit | `GET /api/v1/billing/usage` (`billing.view`): `users.max`, `branches.max` and `storage.mb` with the limit (-1 unlimited, 0 not in the plan) and what is used, counted as the limit checks count |
| staff assigns a plan | `POST /api/v1/billing/internal/tenants/{tenant_id}/subscription` |
| staff cancels | `POST /api/v1/billing/internal/subscriptions/{id}/cancel` — immediate |
| expiry | the `billing.expire-subscriptions` task, every 6 hours |
| renewal reminders | `billing.send-renewal-reminders`, daily at 08:00, at 7/3/1 days |

**Subscribe** is idempotent per tenant + package + method: while a checkout is pending, a retry
or double click returns the same checkout instead of charging twice (a per-tenant lock serialises
it). Activating a subscription deactivates the previous one in the same transaction.

### Staff assignment

Support, sales deals and complimentary accounts use the assign endpoint (`subscription.manage`):

```json
POST /api/v1/billing/internal/tenants/7/subscription
{ "package_id": 2, "payment_method": "cash", "amount": 150000 }
```

It ends the current subscription and starts an **active** one in one transaction. With
`payment_method` (`cash` or `bank_transfer`) it records a completed payment — `amount` defaults
to the package price; without it the plan is free of charge. Hidden (inactive) packages can be
assigned.

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

Organizations see their plan, usage and payments under **Billing** (`/org/billing`) and check out from there; a 402 from any action explains the limit and links to it. Staff manage the plan catalog under **Plans** and decide bank and cash payments under **Payment approvals**. See [Organization workspace](../app/organization-workspace.md#plan-limits) and [Staff console](../app/staff-console.md#payments-and-plans).
