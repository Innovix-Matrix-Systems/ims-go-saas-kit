---
title: Mail and translations
description: The mail transport (log or SMTP driver) behind the delivery system, and the English/Bangla message catalogs.
---

## Mail

Mail is one channel of the [delivery system](delivery.md): templates in plain text or HTML, in
English and Bangla, queued, retried and logged. Configure the transport with `MAIL_DRIVER`:

| `MAIL_DRIVER` | Behaviour |
| --- | --- |
| `log` (default locally) | writes the mail to the log — nothing leaves your machine |
| `smtp` | `MAIL_HOST`, `MAIL_PORT`, `MAIL_USERNAME`, `MAIL_PASSWORD`, `MAIL_ENCRYPTION` (`tls`, `starttls`, empty) |

`MAIL_FROM_ADDRESS` and `MAIL_FROM_NAME` set the sender. A stalled SMTP server times out instead
of hanging the worker. For local SMTP testing, point the driver at [Mailpit](https://mailpit.axllent.org)
(`MAIL_HOST=127.0.0.1`, `MAIL_PORT=1025`, the default) and send one with
`ims delivery:test --to=you@example.com`.

OTP codes, password resets and e-mail verification all use this path. Templates, branding, SMS
and the delivery log are in the [delivery guide](delivery.md).

## Translations (i18n)

API messages — validation errors, problem titles, common messages — come from JSON catalogs in
`internal/platform/i18n/locales/` (`en.json`, `bn.json`), about 390 keys in **English** and
**Bangla**.

- The request locale is negotiated from `Accept-Language` against `APP_SUPPORTED_LOCALES`
  (default `en,bn`), falling back to `APP_FALLBACK_LOCALE`.
- Validator tags map to `validation.*` keys, so `required`, `email`, `max` and friends are
  translated automatically.
- One localizer per locale is cached, so translation costs nothing per request.

```bash
curl -s localhost:8080/api/v1/branch -H 'Accept-Language: bn' …   # Bangla errors
```

To add a language, add `locales/<code>.json` with the same keys and list the code in
`APP_SUPPORTED_LOCALES`.
