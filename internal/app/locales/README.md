# Translation overrides

Change the wording of any existing key without editing the module that owns it: add
`<namespace>/<code>.json` here (for example `validation/en.json`) with only the keys you change.
Overrides are registered last, so they win; a key that doesn't exist elsewhere is refused.
See `docs/guides/languages.md`.
