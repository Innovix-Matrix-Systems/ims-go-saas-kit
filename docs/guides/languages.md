---
title: Languages
description: Six languages built in (English, Bangla, German, Arabic, Spanish, French), right to left, plurals, an organization language, and how to add a language or translate a module's text.
layers: [api, app, dashboard]
---

The kit is not tied to any language. A language is a **descriptor** (its code, name, direction and
digits) plus **catalog files**; nothing else in the code names one. English is the reference text
and the fallback, and checks fail the build while any other language is incomplete.

| Code | Language | Direction | Digits |
| --- | --- | --- | --- |
| `en` | English (reference) | left to right | 0–9 |
| `bn` | বাংলা (Bangla) | left to right | ০–৯ |
| `de` | Deutsch (German) | left to right | 0–9 |
| `ar` | العربية (Arabic) | **right to left** | 0–9 (Western) |
| `es` | Español (Spanish) | left to right | 0–9 |
| `fr` | Français (French) | left to right | 0–9 |

Everything a person reads is translated: the client app, API messages (validation errors, problem
titles), the notifications the system writes, e-mails and SMS, and the OAuth sign-in pages. The
developer dashboard, CLI output and these docs stay in English.

## Which language someone gets

The most specific choice wins, as with [settings](settings.md) and [feature flags](feature-flags.md):

| For | Order |
| --- | --- |
| The screen | the account's language → this browser's pick (signed out) → the organization's language → the browser's languages → `APP_LOCALE` |
| API answers | `Accept-Language` (when it names a supported language) → the account's → the organization's → `APP_LOCALE` |
| E-mails, SMS and notifications | the account's language → the organization's → the request's language → `APP_FALLBACK_LOCALE` |

- **The account's language** is the profile's *Language* (`users.locale`). Picking a language in the
  top-bar menu while signed in saves it there, so other devices and e-mails follow.
- **The organization's language** is the `general.default_locale` setting (organization settings →
  General → Language). Sign-up stores the language the owner signed up in. It is used for members who
  haven't chosen one and for people being invited.
- The app sends its language as `Accept-Language` on every call, so API messages match the screen.

The menu (a globe and the current code in the top bar and on the signed-out screens) lists every
offered language in its own name; `GET /api/v1/languages` (public) is where clients read the list.

## Configuration

| Variable | Meaning |
| --- | --- |
| `APP_SUPPORTED_LOCALES` | the languages offered, comma-separated; **empty offers every registered language**. A code that isn't registered stops the start. |
| `APP_LOCALE` | the default language at the end of the lookup order (must be offered) |
| `APP_FALLBACK_LOCALE` | the language of e-mails when nothing else decides |

## Where the text lives

**API.** A module owns its text: `<package>/locales/<namespace>/<code>.json`, embedded and
registered in order in `internal/app/i18n.go`:

```go
func translationBundles() []i18n.Bundle {
	return []i18n.Bundle{
		i18n.Platform(),           // common, http-statuses, validation
		oauth.Translations(),      // oauth
		notifications.Translations(),
		noticeTranslations(),      // the app's notices
		i18n.Overrides(overrides), // last: internal/app/locales
	}
}
```

A key belongs to its namespace (`oauth.signin_title` lives in `oauth/`) and a namespace to one
module: registering a key outside your namespaces, or a namespace someone else owns, stops the start.
Placeholders are go-i18n template fields (`{{.Attribute}}`), and handlers and services translate with
`i18n.T(ctx, key, params)`; jobs and e-mails with `i18n.Tr(code, key, params)`. `Tr` and the
`locale` validation rule read the process translator, which the application sets once at start-up
(`i18n.SetDefault`), like `slog.Default`; wired code (middleware, the delivery `LocaleSource`, the
languages endpoint) is handed the `*i18n.Translator` instead.

**E-mails.** A template folder lives with the code that sends it (`internal/authn/templates/`,
`internal/modules/billing/templates/`, …) and has a variant per language next to each file:
`mail.txt` is English, `mail.de.txt` German. Files made only of data (`{{.Data.title}}`) need no
variant. The mail layout sets `lang` and `dir`. See [Delivery](delivery.md#templates).

**Notifications** the system writes (payment notices, renewal reminders) carry a message key in their
data (`{"i18n": {"key": "notices.payment_received", "params": {…}}}`) and are rendered for each reader:
the feed in the reader's language, the e-mail in the recipient's. Create one with
`CreateInput{Message: &notifications.Message{Key: …, Params: …}}`.

**Client app.** `web/app/src/i18n/catalogs/<namespace>/<code>.json`, one folder per screen area. See
[Translations and theming](../app/translations-and-theming.md).

## Plurals

Text that depends on a number has CLDR plural forms instead of one string:

```json
"common.duration.days": {"one": "{{.Count}} day", "other": "{{.Count}} days"}
```

The app does the same with `{count}`. Each language gives the forms its rules have: English and
German `one`/`other`, Spanish and French also `many`, Arabic six (`zero`, `one`, `two`, `few`, `many`,
`other`). `{{.Count}}` and `{count}` are shown in the language's digits (`৩ দিন`).

## Right to left

Arabic sets `dir="rtl"` on the page and the whole app mirrors: the stylesheets use logical
properties only (`margin-inline-start`, `inset-inline-end`, `text-align: start`), and
`npm run check:logical` fails on a `left`/`right` property. Icons that point along the reading
direction turn around. E-mail addresses, URLs, passwords, phone numbers, typed numbers, code, IDs,
amounts and charts stay left to right inside an RTL page.

## Changing the wording

Don't edit a module's catalog to change a word in your project: add an override with only the keys
you change, and kit upgrades won't conflict.

```json title="internal/app/locales/validation/en.json"
{"validation.required": "Please fill in {{.Attribute}}."}
```

The app's equivalent is `web/app/src/i18n/overrides/<namespace>/<code>.json`. An override of a key
that doesn't exist is refused (it would be a typo).

## Adding a language

The `translate` skill (`.agents/skills/translate`) is the recipe an AI agent follows; by hand it is
the same steps:

1. **Descriptor**, the same in both lists: `languages()` in `internal/app/i18n.go` (or
   `i18n.Builtin()` for a kit language) and `web/app/src/i18n/languages.json`.

   ```go
   i18n.Language{Code: "pt", Label: "Português", English: "Portuguese", Intl: "pt", Dir: i18n.LTR, Digits: "latn"}
   ```

2. **List what is missing** and the plural forms the language needs:

   ```bash
   ims i18n:missing --locale pt          # API keys and template files, with the English text
   ims i18n:missing --locale pt --json   # the same for tools
   node web/app/scripts/i18n.mjs missing pt
   ```

3. **Translate** into the files above, using a glossary
   (`.agents/skills/translate/glossary/<code>.json`) so product terms stay consistent.
4. **Review** in a second pass, then set `reviewed: true` (until then the menu marks it *beta*).
5. **Check**: `ims i18n:coverage` and `npm --prefix web/app run i18n:check` pass only when every key,
   placeholder, plural form and template variant is there. The dashboard's **Inspector →
   Translations** shows the same report.

A module adds its own text the same way: `ims gen module` writes `locales/<name>/en.json` and
`locales.go`; register `<name>.Translations()` in `translationBundles` and translate the file for
every language.

## Checks

| Check | Fails when |
| --- | --- |
| `ims i18n:coverage`, `TestTranslationsCoverEveryLanguage` | an API key or template variant is missing, extra, or loses a placeholder or plural form |
| `npm run i18n:check` (CI) | the same for the app, the key types are stale, or a source file names a language code |
| `TestNoHardCodedLanguageCodes` | Go code names a language code outside the registry |
| `TestAppLanguagesMatchTheAPI` | the app's built-in list differs from the API's |
| `npm run check:logical` (CI) | a stylesheet uses a physical `left`/`right` property |
