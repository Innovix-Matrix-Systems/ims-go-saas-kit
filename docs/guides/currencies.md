---
title: Money and currency
description: Currencies are data, Akaunting style. Amounts are integers in minor units; a currency's row says how to write them; changing the default changes the label, never the numbers.
layers: [api, app]
---

Money in the kit follows one rule: **an amount is an integer in its currency's minor units** (cents,
paisa) and a currency is a row that says how to write it. Changing the currency changes the symbol
and format; amounts are never converted. There are no exchange rates, no prices in several
currencies and no tax, so nothing depends on a rate being right.

## Currencies

Each currency is a row in `currencies`, managed by staff under **Console → Currencies** (add,
edit with a live preview, enable or disable, and **Make default** from the currency's row or the
top of its drawer):

| Field | Example | Meaning |
| --- | --- | --- |
| `code` | `BDT` | three capital letters (ISO 4217); set once |
| `name` | Bangladeshi Taka | shown in lists |
| `symbol` | `৳` | the sign written with the amount |
| `symbol_first` | yes | `৳1,234.50`, or `1.234,50 €` when no |
| `precision` | `2` | decimals: an amount of `123450` is `1,234.50` |
| `decimal_mark`, `thousands_separator` | `.` and `,` | `€` uses `,` and `.` |
| `is_enabled`, `is_default` | | offered to clients; the one in use |

A new install has **USD** (the default), **BDT** and **EUR**. An upgraded install with payments
keeps its money: the currency its payments use becomes the default.

**The default currency** is the currency of plan prices and of new payments. Plans have no
currency of their own: switching the default from BDT to USD turns a plan priced `৳300.00` into
`$300.00`, the same number with a new symbol, so re-enter the prices if you really change
currency. Each payment stores the code it was made in, so old payments, invoices and receipts keep
their own currency, and dashboard revenue stays grouped per currency.

Guards that keep stored amounts meaningful:

- A currency's **decimals are locked once it has payments**: going from 2 to 0 would make every
  stored amount a hundred times bigger.
- The default can't be disabled or deleted; a currency with payments can't be deleted (disable it).
- The decimal mark and the thousands separator must differ, or `1,234` could be read two ways.
- bKash and Nagad settle in taka, so they refuse a payment while the default isn't BDT.

Every change (added, updated, default changed, deleted) is recorded in the system audit log
(**Console → Audit log**).

## How amounts are written

The currency's row decides the symbol, its side, the decimals and the separators. Only the digits
follow the reader's language: a Bangla reader sees `৳১,২৩৪.৫০`, an English one `৳1,234.50`.
Numbers stay left to right in Arabic. The same rule runs on both sides, so the API and the app write
an amount identically:

| Where | How |
| --- | --- |
| Go | `money.Of(minor, currency).Format(lang)` (`internal/platform/money`); `.Code()` gives `BDT 1,234.50` for the PDF invoice, whose fonts can't draw every symbol; `.Decimal()` gives `1234.50` for gateway payloads |
| Notifications | `notifications.Money(amount)` as a message param: each reader gets their digits |
| E-mails | format with `i18n.LanguageFor(delivery.LocaleFor(ctx, to))` where the message is built |
| App | `useMoney()(minor, code)` (no code: the default currency), `formatMoney` in `src/money.ts`; `{ compact: true }` for chart axes |
| App inputs | `MoneyInput` (symbol, decimals, left to right) with `toMinor`/`fromMinor`: a typo is refused, never rounded |

## API

- `GET /api/v1/public/currencies` (no sign-in; cached on the server, never in the browser, so a
  change by staff shows at once): the enabled currencies, the
  default first, and `default`. The app loads it at start, with the language, and reads it again
  after a minute or when the tab is focused again.
- Staff: `GET/POST /api/v1/currencies`, `PATCH`/`DELETE /api/v1/currencies/{id}`,
  `POST /api/v1/currencies/{id}/default` (`currency.view.all`, `currency.create`,
  `currency.update`, `currency.delete`; system permissions).
- Every amount in the API is an integer next to a `currency` code; a plan's `price` is in the
  default currency.

## Money in your own module

Store an amount as `bigint` minor units and keep the currency with it when it can differ (a
`currency text` column on the row, or one per document); otherwise it is the default currency.

```go
format := currencies.Format(ctx, row.Currency)        // *currencies.Service
text := money.Of(row.Total, format).Format(i18n.LanguageFor(code))
```

```tsx
const money = useMoney()
<td className="end">{money(order.total, order.currency)}</td>
<MoneyInput currency={useDefaultCurrency()} value={price} onChange={setPrice} />
// on submit: const minor = toMinor(price, currency); if (minor === null) show an error
```

Never divide by 100 or use floats: the currency's `precision` says where the decimal point is.
