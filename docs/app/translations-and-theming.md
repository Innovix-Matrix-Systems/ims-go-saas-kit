---
title: Translations and theming
description: How the app speaks six languages (including right to left), how to add one, and how the theme, accent and density work on design tokens.
layers: [app, api]
---

The app ships in **six languages** (English, Bangla, German, Arabic, Spanish and French, Arabic
right to left), in light and dark, with five accent colours and two densities. All of it rests on
two small systems: typed message catalogs and a set of design tokens. How languages work across the
whole kit (lookup order, e-mails, adding one) is in [Languages](../guides/languages.md).

![The organization home in Bangla](../images/app-org-dashboard-bn.png)

## Translations

### How text works

Every visible string is a key in a catalog, one folder per screen area:

```json title="web/app/src/i18n/catalogs/members/en.json"
"members.invitedToast": "Invitation sent to {email}.",
"members.count": {"one": "{count} member", "other": "{count} members"}
```

```tsx
const t = useT()
push(t('members.invitedToast', { email }), 'success')
t('members.count', { count: members.length })
```

- `catalogs/<namespace>/en.json` is the **reference**. `npm run i18n:types` generates
  `messages.gen.ts`, so a key that doesn't exist, or a missing `{placeholder}`, is a type error.
- A value that depends on a number is **plural forms** selected by `{count}` (`Intl.PluralRules`);
  `{count}` is shown in the language's digits. Never build "1 files": give `one` and `other` (and
  whatever the language needs; `npm run i18n:check` lists them).
- **English is bundled; every other language loads when chosen**, as one chunk, and the screen
  switches only once it has arrived.
- The language follows the account (signed in), then this browser's pick, then the organization's
  language, then the browser. It sets `<html lang dir>`, and every API call sends it as
  `Accept-Language`.
- **Activity log** actions have labels in the `activity` namespace (`activity.label.<action>`); an
  action without one is spelled from its code.

### The server speaks the same language

API messages come back translated too: validation errors, problem titles, the notifications the
system writes. E-mails use the **recipient's** language. See [Languages](../guides/languages.md).

Dates, numbers and money are formatted with `Intl` for the chosen language and its digits, so a
Bangla user sees Bangla numerals and month names: use `useMoney()` and the helpers in `src/format.ts`
rather than formatting by hand.

![Billing in Bangla](../images/app-billing-bn.png)

### Adding a language

1. Add its descriptor to `src/i18n/languages.json` and to the API's list (see
   [Languages](../guides/languages.md#adding-a-language)); the app reads the offered list from
   `GET /api/v1/languages`.
2. Add `catalogs/<namespace>/<code>.json` for every namespace: `node scripts/i18n.mjs missing <code>`
   lists the English text and the plural forms.
3. `npm --prefix web/app run i18n:check` fails until every key, placeholder and plural form is there.

### Right to left

Stylesheets use logical properties only (`margin-inline-start`, `padding-inline-end`,
`inset-inline-start`, `text-align: start/end`); `npm run check:logical` fails on `left`/`right`.
Write new rules the same way, use the `end` class for numbers in tables and `align-end` for popovers
on the end edge. Inputs for e-mail, URL, password, phone and numbers, `.mono`, code and charts stay
left to right.

### Changing the wording

Put only the keys you change in `src/i18n/overrides/<namespace>/<code>.json`; they are merged last,
so kit upgrades don't conflict.

## Theming

### Tokens, not colours

Components never use a raw colour. They use **tokens** defined once in
`web/app/src/styles/tokens.css`:

| Tokens | For |
| --- | --- |
| `--surface-0` … `--surface-2`, `--surface-inset` | page, cards, raised and sunken areas |
| `--text-1` … `--text-3` | primary, secondary and muted text |
| `--border`, `--border-strong` | dividers and inputs |
| `--accent`, `--accent-fg` | the accent and text on it |
| `--ok-fg`, `--warn-fg`, `--danger-fg`, `--info-fg`, `--violet-fg` | status colours |
| `--series-1` … `--series-6` | chart series, in order |
| `--fs-xs` … `--fs-xl`, `--space-1` … `--space-6`, `--radius` | type scale, spacing, shape |

Three attributes on `<html>` select the values, set before the first paint so nothing flashes:

```html
<html data-theme="dark" data-accent="teal" data-density="compact">
```

- **Theme**: `light` or `dark`; "System" follows the operating system and resolves to one of
  them.
- **Accent**: `indigo` (default), `blue`, `teal`, `violet`, `slate`. Each preset has a light and a
  dark value.
- **Density**: `comfortable` (default) or `compact`, which tightens rows and padding.

The user's choices are stored per browser (`src/components/appearance.ts`).

### Rebranding

To give the app your brand:

1. Change the accent presets in `tokens.css` (or add one, and list it in `accents` in
   `appearance.ts`). Set the default in `appearance.ts`.
2. Adjust surfaces and text in both theme blocks if you want a different neutral.
3. Replace `web/app/public/favicon.svg` and set `'app.name'` in every catalog.
4. Run the style check.

```bash
npm --prefix web/app run check:style
```

The check fails when any text and surface pair, the accent, or a status colour on its background
falls below **WCAG AA contrast (4.5:1)** in either theme with any accent preset, when a style uses
a size outside the type scale, or when a raw colour appears outside `tokens.css`. CI runs it.

### Charts

Charts (`src/components/charts.tsx`) take their colours from `--series-1` … `--series-6` and
their text and grid from the text and border tokens, so they follow the theme without extra code.
Use `KpiCard`, `TrendChart`, `DonutChart` and `RankedBarChart` rather than raw Recharts so every
chart in the app looks the same.
