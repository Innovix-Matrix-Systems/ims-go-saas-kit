# Translation overrides

Change the wording of an existing key without editing the catalogs: add
`<namespace>/<code>.json` here with only the keys you change (for example
`members/en.json`). Overrides are merged last; `npm run i18n:check` refuses a key that does
not exist in `catalogs/`. See `docs/guides/languages.md`.
