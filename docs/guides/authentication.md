---
title: Authentication
description: Opaque per-device tokens, login throttling, OTP, password reset and e-mail verification.
---

## Tokens

The API issues **opaque personal access tokens**, like Laravel Sanctum. A token looks like
`{id}|{secret}`; only a SHA-256 hash of the secret is stored, one row per device in
`personal_access_tokens`.

```http
POST /api/v1/login
Content-Type: application/json

{ "email": "admin@acme.test", "password": "secret123", "device": "iPhone 16" }
```

```json
{ "data": { "status": "AUTH_SUCCESS", "message": "…", "token": "42|q9…", "user": { "id": 7, "name": "…" } } }
```

With OTP enabled, `status` is `OTP_REQUIRED`, `token` is `null`, and the client completes the
login with `POST /api/v1/verify-otp`.

Send it as `Authorization: Bearer 42|q9…`. Tokens expire after `AUTH_TOKEN_EXPIRATION` minutes
(`0` = never, refused in production). `last_used_at` is touched at most once a minute.

| Endpoint | Does |
| --- | --- |
| `POST /api/v1/login` | e-mail + password (+ device) → token, or an OTP challenge |
| `POST /api/v1/verify-otp` | completes an OTP login |
| `POST /api/v1/social/{provider}/login` | a Google or GitHub sign-in → the same token ([social login](social-login.md)) |
| `POST /api/v1/logout` | revokes the current token |
| `GET /api/v1/sessions` | the caller's own sessions ([below](#sessions)) |
| `GET /api/v1/user/profile` | the signed-in user |

The engine sits behind an `authn.Authenticator` interface (`Issue/Verify/Revoke/RevokeAll`), so a
JWT driver can be added without touching handlers. Every login, successful or not, is an
`auth.login` entry in the [activity log](activity-log.md).

Two features use OAuth, in opposite roles: [social login](social-login.md) (the API as a client of
Google or GitHub) and the [MCP connector](mcp.md) (the API as a server for AI apps). See
[OAuth in this starter](oauth.md).

## Login checks

- **Throttling**: `AUTH_LOGIN_MAX_ATTEMPTS` failures per e-mail + IP within
  `AUTH_LOGIN_DECAY_MINUTES` lock the login (cache-backed).
- **Active and verified**: `AUTH_CHECK_IS_ACTIVE`, `AUTH_CHECK_EMAIL_VERIFIED` (and phone) refuse
  inactive or unverified accounts.
- **OTP** (`AUTH_OTP_ENABLED`): a 6-digit code (`AUTH_OTP_LENGTH`) valid for
  `AUTH_OTP_EXPIRY_MINUTES`, stored only in the cache and mailed through the
  [delivery system](delivery.md) (template `otp-code`, in the user's language). Users who turned
  on [two-factor authentication](#two-factor-authentication) get their own method instead.
  Password login only: a social login is already authenticated by the provider.

## Password reset

```http
POST /api/v1/forgot-password   { "email": "admin@acme.test" }
POST /api/v1/reset-password    { "email": "…", "token": "…", "password": "…", "password_confirmation": "…" }
```

The mail links to `AUTH_PASSWORD_RESET_URL` (your frontend) with `{token}` and `{email}`
placeholders. The token is random, only its SHA-256 lives in the cache, it works **once** and
expires after `AUTH_PASSWORD_RESET_EXPIRY`. The new password needs at least 8 characters. Responses never reveal whether an e-mail exists. A
successful reset signs the user out **everywhere**.

## E-mail verification

```http
POST /api/v1/email/verification-notification   (re)sends the link
POST /api/v1/email/verify                      { "token": "…" }
```

With `AUTH_NEW_USERS_VERIFIED=false`, admin-created and imported users must verify before they
can sign in. Links use `AUTH_EMAIL_VERIFY_URL` and expire after `AUTH_EMAIL_VERIFY_EXPIRY`.

## Two-factor authentication

Each user can turn on a second factor, one of:

| Method | The code comes from | |
| --- | --- | --- |
| `totp` | an authenticator app (Google Authenticator, 1Password, Authy…) | works offline; the default to suggest |
| `email` | a mail sent at sign-in (template `otp-code`) | for users without an authenticator app |

Either way the user also gets **eight recovery codes** (`k7p2m-x9q4w`), each signing in once when
the phone or mailbox is lost.

**Turning it on** (the caller's own account, password confirmed):

```http
POST /api/v1/two-factor          { "method": "totp", "password": "…" }
→ { "method": "totp", "secret": "JBSW…", "otpauth_url": "otpauth://totp/…" }   # show the URL as a QR code
POST /api/v1/two-factor/confirm  { "code": "123456" }
→ { "recovery_codes": ["k7p2m-x9q4w", …] }                                       # shown once
```

With `"method": "email"` the first call mails a code instead (the address must be verified).
`GET /api/v1/two-factor` shows the state, `POST /api/v1/two-factor/recovery-codes` replaces the
codes and `DELETE /api/v1/two-factor` turns it off (both with the password; turning it off also
signs the other sessions out). Turning it on or off, and using a recovery code, mails the user.

**Signing in** asks for the code after the password:

```http
POST /api/v1/login       { "email": "…", "password": "…", "device": "web" }
→ { "status": "OTP_REQUIRED", "method": "totp", "token": null }
POST /api/v1/verify-otp  { "email": "…", "otp": "123456", "device": "web" }   # or a recovery code
→ { "status": "AUTH_SUCCESS", "token": "42|…" }
```

A code is accepted only within five minutes of that password step, so a code alone never signs
anyone in, and each app code works once. Social logins ask for it too (the provider proves the
e-mail address, not the second factor), and so do the MCP connector's sign-in page and the
developer dashboard.

**Requiring it.** The setting `security.require_two_factor` (per tenant) makes a tenant's users
turn it on, and `AUTH_REQUIRE_2FA_PLATFORM=true` does the same for platform accounts. Until they
do, signing in answers `TWO_FACTOR_SETUP_REQUIRED` with a **setup-only token**: it reaches
`/api/v1/two-factor*`, `/api/v1/sessions*` and `/api/v1/logout`, and every other route answers
`403 TWO_FACTOR_SETUP_REQUIRED`. Confirming the enrolment returns a full token that replaces it.

**Lost phone and codes.** A tenant admin with `user.two_factor.reset` calls
`DELETE /api/v1/user/{id}/two-factor`; for platform accounts, `ims two-factor:reset --email=…`.
The user is mailed and the reset is a `user.two_factor_reset` activity row.

TOTP secrets are encrypted with `APP_KEY` (see [deploy](../operations/deploy.md)); recovery codes
are stored as SHA-256 hashes. The server does not render the QR code: show `otpauth_url` with any
QR library in your client.

## Sessions

Every sign-in is a session: a token for one device, with the IP and user agent it was opened
from. Users manage their own:

| Endpoint | Does |
| --- | --- |
| `GET /api/v1/sessions` | the caller's sessions: device, IP, `browser` ("Chrome 131 on Windows"), created, last used, `current` for this one |
| `DELETE /api/v1/sessions/{id}` | signs one out (another user's id answers 404) |
| `POST /api/v1/sessions/revoke-others` | keeps this session, signs out every other one and disconnects the caller's MCP connections |

They run without tenant resolution, so platform accounts use them too, and always act on the
caller (`X-Acting-User` does not apply). Revocations are `auth.session_revoked` entries in the
activity log. MCP connections are listed separately, at `/api/v1/user/connections`.

- **Pruning:** the `auth.prune-tokens` task deletes expired tokens daily; with
  `AUTH_SESSION_IDLE_DAYS` set it also signs out sessions unused for that many days.
- **New sign-in alert:** with the setting `security.new_sign_in_alert` on (globally or per
  tenant), a user who signs in from an IP address they have not used in the last 90 days gets a
  mail (template `new-sign-in`) with the time, IP, browser and device. A first sign-in ever is not
  alerted, and the alert never blocks the sign-in.

## Inviting users

An admin does not have to choose someone else's password. Create the user with
`send_invitation: true` (the same `POST /api/v1/user`, `user.create`):

```http
POST /api/v1/user  { "name": "Nadia Islam", "email": "nadia@acme.test", "branch_id": 1, "roles": [2], "send_invitation": true }
→ 201 { "is_active": false, "invitation_status": "pending", … }
```

The user is created **inactive**, with a password nobody knows, and mailed a link (template
`invitation`, naming the inviter) to `AUTH_INVITATION_URL` (your frontend; `{token}` and
`{email}` are replaced) that works once for `AUTH_INVITATION_TTL` (default 7 days). The frontend
posts the new password:

```http
POST /api/v1/invitation/accept  { "email": "…", "token": "…", "password": "…", "password_confirmation": "…" }
```

That activates the account and verifies the address; the user then signs in with
`POST /api/v1/login` like everyone else, so every sign-in check (and a tenant's two-factor
policy) applies.

- **Resend:** `POST /api/v1/user/{id}/invitation` (`user.update`; internal users:
  `/api/v1/internal/user/{id}/invitation`) mails a new link and the old one stops working. It also
  invites an existing user who has never signed in. A user who joined answers 409.
- **List:** `GET /api/v1/user?invitation=pending` (or `accepted`); every user has
  `invitation_status`: `none`, `pending` or `accepted`.
- **Seats:** an invited user counts against `users.max` from the moment they are created.
- **Safety:** a link works only while the invitation is pending, so an old link can never
  reactivate a user an admin deactivated after they joined. A mistyped e-mail does not use the link
  up.
- **Activity:** `user.invited`, `user.invitation_resent`, `user.invitation_accepted`.
- **Preview:** before asking for a password, the invitation page can show who is inviting whom:
  `POST /api/v1/invitation/preview {email, token}` → `{name, email, organization_name,
  inviter_name}`. It does not use the link up; a wrong address or an expired link is 422, an
  accepted invitation 404.

Like reset links, invitation tokens live in the cache: with the `memory` driver a restart
invalidates pending links (resend fixes it). Use `redis` or `database` in production.

## Changing the e-mail address

```http
POST /api/v1/user/profile/change-email   { "password": "…", "email": "new@acme.test" }   → 202
POST /api/v1/email/change/confirm        { "token": "…" }                                → 200
```

The first call checks the password and that the address is free, then mails a link to the **new**
address (template `email-change`, `AUTH_EMAIL_CHANGE_URL`, default
`APP_URL/confirm-email-change?token={token}`, valid for `AUTH_EMAIL_VERIFY_EXPIRY`). Nothing
changes until the link is opened, so a typo never locks anyone out. Confirming switches the
account to the new address (verified), mails the old address (`email-changed`, with the new one
masked) and signs the user out everywhere. One request a minute per account; refused while acting
as another user.

## Language

`GET /api/v1/user/profile` returns `locale` (`null` = default) and
`POST /api/v1/user/profile/update` accepts it: `"bn"` saves it, `""` clears it, leaving it out
keeps it. Clients use it to show the user's language on every device; requests still choose
their response language with `Accept-Language`.

## Changing passwords

| Who | Endpoint | Effect |
| --- | --- | --- |
| the user | `POST /api/v1/user/profile/change-password` | needs the current password; signs out the user's **other** devices |
| a tenant admin | `POST /api/v1/user/change-password` | `user.update` |
| platform staff | `PATCH /api/v1/internal/user/change-password` | internal permission |

## Acting as a user

A holder of `user.impersonate` can send `X-Acting-User: <user id>` on tenant routes to make that
one request run as the user: their permissions, tenant and branch. No token is issued and the
user's sessions and `last_login_at` are untouched; stop by not sending the header. Profile,
password, avatar and branch-switch endpoints refuse with 403 `IMPERSONATION_FORBIDDEN` while
acting, and every request is written to the system audit log. Rules and refusals are in the
[tenancy guide](tenancy.md#how-a-request-is-resolved).

In code, `authn.IdentityFromContext(ctx)` is the target; `identity.Impersonated()` and
`identity.ImpersonatorID` tell you someone else is calling.

## In code

```go
userID := authn.UserID(r)                       // 0 when anonymous
identity, ok := authn.IdentityFromContext(ctx)  // id, name, email, tenant, branch
```
