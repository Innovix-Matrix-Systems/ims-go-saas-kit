---
title: Sign-up and onboarding
description: Let people create an organization themselves, and track each new organization's first-run setup.
---

Organizations normally come from platform staff (`POST /api/v1/tenant`). With self-service
**sign-up**, anyone can create one from your app's sign-up page; the
`internal/modules/onboarding` module does the work.

## Signing up

```http
POST /api/v1/signup
{ "name": "Nadia Islam", "email": "nadia@acme.test", "password": "a long passphrase",
  "organization_name": "Acme Digital", "phone": "+8801711000001", "locale": "bn", "accept_terms": true }
→ 202 { "data": { "message": "Check your e-mail: …" } }
```

One request creates, in order:

1. the **organization** (tenant) with its default branch and the tenant **Admin** role;
2. its **subscription** to the sign-up package (below);
3. the **owner**: an Admin user who must confirm the address (the verification mail goes out
   at once), whatever `AUTH_NEW_USERS_VERIFIED` says;
4. an onboarding record naming the owner, and the accepted **terms of service** and **privacy
   policy** versions (`user_agreements`, with the IP address and browser);
5. a `tenant.signed_up` activity entry.

These run through the existing tenant, billing and identity services rather than one
transaction; if a later step fails, the half-created organization is soft-deleted again.

**Nothing is revealed about existing accounts.** A valid request always answers the same 202.
When the address already has an account, no organization is created and the address's owner
gets the `signup-account-exists` mail (sign-in and password-reset links) instead.

Refusals: `422` for a missing `accept_terms`, an invalid field or a password that breaks the
[password rule](authentication.md); `403` when sign-up is closed; `503` when there is no package
to start on (or it allows no users); `429` after ten attempts from one IP address in an hour.

### Settings (group **Sign-up**)

| Setting | Default | Meaning |
| --- | --- | --- |
| `signup.enabled` | `true` | Off: only staff create organizations (`403`). |
| `signup.package_id` | `0` | The package new organizations start on. `0` picks the first active free package by sort order; with none, sign-up answers `503`. Either way the package needs `users.max` of at least 1 (or `-1`) for the owner. |
| `signup.terms_version` | `"1"` | The terms and privacy-policy version recorded with each acceptance. Raise it when the documents change. |

The seeded **Unlimited** package is hidden (inactive), so it is never picked by `0`: create a
free package for sign-ups, with room for at least one user. Every check runs before anything is
created; if a later step still fails, the organization is removed and its subscription cancelled.

## First-run setup

| Endpoint | Who | Does |
| --- | --- | --- |
| `GET /api/v1/onboarding` | any member | `{completed, completed_at, skipped_steps, source, is_owner}` |
| `POST /api/v1/onboarding/complete` | the owner | `{skipped_steps: ["invite", …]}` marks setup done (steps: `organization`, `package`, `invite`, `two_factor`) |

A client shows its setup wizard while `completed` is false and the caller `is_owner`. Completing
twice keeps the first time. Organizations created before onboarding existed (or by staff without
an owner) answer `404`.

## Privacy

`user_agreements` and `tenant_onboarding.owner_user_id` are covered by the module's privacy
contributor: exports list the accepted documents and owned organizations; erasure keeps the
acceptances (evidence of consent) but clears their IP address and browser.
