---
name: translate
description: Use when adding a language to this kit or filling the gaps in one — the language descriptor (API and app), the API catalogs per module, the client app catalogs, e-mail/SMS template variants, plural forms, the glossary and the review pass, and the checks (ims i18n:coverage, npm run i18n:check) that must pass before a language is marked reviewed.
---

# Translating

Human guide: `docs/guides/languages.md`. The system is not tied to any language: a language is a
descriptor plus catalog files, and the checks fail until every file exists and agrees with English.
English is the reference text; nothing else in the code names a language.

## Where the text lives

| What | Files | Placeholders |
|---|---|---|
| API messages (validation, HTTP titles, notices, OAuth pages) | `<package>/locales/<namespace>/<code>.json`, one namespace per owning module, registered in `translationBundles` (`internal/app/i18n.go`) | `{{.Name}}` |
| E-mail, SMS, push | `<package>/templates/<name>/<file>.<code>.<ext>` next to the English `<file>.<ext>` (only files with words of their own) | `{{.Data.x}}`, `{{.Brand.Name}}`, `{{duration .Data.minutes}}` |
| Client app | `web/app/src/i18n/catalogs/<namespace>/<code>.json` | `{name}` |
| Wording changes in a project | `internal/app/locales/…`, `web/app/src/i18n/overrides/…` (replace existing keys only) | as above |

A value is a string, or CLDR plural forms when it depends on a number:
`{"one": "{{.Count}} day", "other": "{{.Count}} days"}` (API, `PluralCount` selects) or
`{"one": "{count} file", "other": "{count} files"}` (app, `{count}` selects). Give **every** form
the language uses — `ims i18n:missing` and `npm run i18n:check` print them (Arabic: zero, one, two,
few, many, other). A key ending in `._context` is a note for you, never translated.

## Adding a language

1. **Descriptor**, the same in both places:
   - `internal/platform/i18n/languages.go` `Builtin()` for a kit language, or `languages()` in
     `internal/app/i18n.go` for a project's own;
   - `web/app/src/i18n/languages.json` (`TestAppLanguagesMatchTheAPI` keeps them equal).
   Fields: `code` (two letters), `label` (its own name), `english`, `intl` (BCP 47, e.g. `de`,
   `bn-BD`), `dir` (`ltr`/`rtl`), `digits` (`latn`, `beng`, `arab`…), `reviewed: false`.
2. **Glossary**: `.agents/skills/translate/glossary/<code>.json` — product terms, words that stay
   in English, and style notes (formal/informal address, punctuation, quotes). Copy the shape of
   an existing one; decide the terms before translating anything.
3. **List what is missing:**
   ```bash
   go run ./cmd/app i18n:missing --locale <code> --json      # API keys + template files
   node web/app/scripts/i18n.mjs missing <code>              # app keys (JSON)
   ```
4. **Translate**, namespace by namespace, into the files above. Rules:
   - Keep every placeholder exactly (`{{.Attribute}}`, `{count}`), and never translate inside
     `{{…}}` or HTML tags.
   - Use the glossary's terms every time; brand and product names stay as they are (IMS Go SaaS
     Kit, bKash, Nagad, MCP, Google, GitHub).
   - Match the English tone: short, plain, sentence case; the same address form throughout.
   - Templates: copy the English file to `<file>.<code>.<ext>` and translate the words only.
5. **Review pass** (a second, separate pass over the whole language): glossary consistency,
   one form of address, placeholders and plural forms, meaning against English (not a word-for-word
   echo), length in buttons and table headers, punctuation and quotes for the language. Fix, then
   set `reviewed: true` in both descriptors. Until then menus show the language as beta.
6. **Checks** (all must pass):
   ```bash
   go run ./cmd/app i18n:coverage
   go test ./internal/app -run 'Translations|Template|HardCoded|AppLanguages'
   cd web/app && npm run i18n:check && npm run typecheck
   ```
7. **Right to left**: with `dir: "rtl"` the app flips through CSS logical properties; check the
   screens listed in `docs/guides/languages.md` (sidebar, tables, menus, pagination, forms, charts,
   an e-mail).
8. Deployments with `APP_SUPPORTED_LOCALES` set must add the code there; empty offers every language.

## Filling gaps after a change

A new key in English makes every other language fail the checks. Run step 3 for each language,
translate only what is listed, then step 6. A module's new namespace is registered with its bundle
(`gen module` writes `locales/<name>/en.json` and `locales.go`).

## Don't

- Add a key in another module's namespace (refused at start) — use your own, or the overrides.
- Leave a language half-done with `reviewed: true`, or mark it reviewed without the second pass.
- Write a language code in Go or TS source (`TestNoHardCodedLanguageCodes`, `npm run i18n:check`).
