---
title: Social login
description: Sign existing users in with Google or GitHub — how it works, setting up each provider, the front-end and mobile side, production, troubleshooting and adding a provider.
---

A user can sign in with their Google or GitHub account instead of a password, and gets **exactly
the token and response a password login gives**. The API needs no migration, keeps no state, and
is off until you list a provider in `SOCIAL_PROVIDERS`.

New to it? The tutorial [Sign in with GitHub locally](../tutorial/sign-in-with-github.mdx) walks
through a real sign-in on your machine in about fifteen minutes. This page explains how it works
and how to set it up for real.

## How it works

The provider (Google, GitHub) answers one question: **who is this?** Its answer is an e-mail
address it has verified. Everything after that is the same as a password login:

```text
 Front end / mobile app            Provider (Google, GitHub)            API
 ──────────────────────            ─────────────────────────            ───
 1. user clicks "Sign in with …" ─▶ user signs in, approves
                                 ◀─ id_token (Google)
                                    or a one-time code (GitHub)
 2. POST /api/v1/social/{provider}/login { id_token | code, …, device } ─────────▶
                                                     3. ask the provider: is this proof real?
                                                        → a verified e-mail address
                                                     4. find our user with that e-mail
                                                     5. password login's checks: active,
                                                        verified, throttle
                                                     6. issue the normal device token
 ◀─────────────────────────────────── { user, status: AUTH_SUCCESS, token } ───────
```

- **The password check is swapped for a provider check; nothing else changes.** Tenant, branch,
  roles and permissions come from **our** user record, exactly as after a password login, so
  social login knows nothing about tenancy. A tenant user and a tenantless super admin sign in the
  same way.
- **Existing users only.** Social login finds users; it never creates them. Admins create users
  as today. If nobody has the e-mail, the answer is 403 `SOCIAL_ACCOUNT_NOT_FOUND`.
- **Stateless.** The front end (or the mobile SDK) talks to the provider; the API verifies one
  POST and keeps nothing: no session, no stored provider tokens, no link table, no callback route
  on the API.
- **No OTP step.** `AUTH_OTP_ENABLED` applies to password login only: the provider has already
  authenticated the user (Google and GitHub usually with their own 2FA).
- **Never on an unverified e-mail.** Anyone can open a GitHub account with someone else's address
  and leave it unverified; signing in on it would hand them that person's account. Such attempts
  get 403 `ACCOUNT_UNVERIFIED`.

### Two kinds of OAuth in this starter

They share the vocabulary, nothing else ([OAuth in this starter](oauth.md) explains both side by
side, with the terms):

| | Social login (this page) | [MCP connector](mcp.md) |
| --- | --- | --- |
| Our role | an OAuth **client** of Google or GitHub | an OAuth **server** for AI apps |
| Who signs in where | the user signs in **on Google's or GitHub's** page | the user signs in **on our** page |
| What comes out | our normal session token, for `/api/v1` | an MCP access token, only for `/mcp` |
| Turned on by | `SOCIAL_PROVIDERS` | `MCP_ENABLED` |

### The two kinds of proof

| Provider | The client sends | The API then |
| --- | --- | --- |
| **Google** (OpenID Connect) | `id_token`: a signed statement from Google | checks the signature against Google's published keys, the issuer, the expiry, and that it was issued to one of **your** client ids; reads `email` and `email_verified` |
| **GitHub** (OAuth 2.0, no id_token) | `code` from GitHub's redirect, plus the PKCE `code_verifier` and `redirect_uri` it used | trades the code for a GitHub access token with your **client secret** (GitHub checks the verifier and redirect URI), reads `/user/emails`, takes the **primary** address and whether it is verified, and forgets the access token |

That is why Google needs no secret on the API and GitHub does: Google's proof is checkable by
anyone with Google's public keys; GitHub's code is only worth something together with the secret.

## Turn it on

```bash
SOCIAL_PROVIDERS=google,github                 # empty (the default) = off; order = login screen order
SOCIAL_GOOGLE_CLIENT_IDS=web-id,ios-id,android-id
SOCIAL_GITHUB_CLIENT_ID=Ov23li…
SOCIAL_GITHUB_CLIENT_SECRET=…                  # a secret: .env or your secret store, never git
```

| Setting | Needed for | |
| --- | --- | --- |
| `SOCIAL_PROVIDERS` | everything | comma-separated: `google`, `github`. Empty = off |
| `SOCIAL_GOOGLE_CLIENT_IDS` | `google` | every client id an id_token may be issued to (web, iOS, Android) |
| `SOCIAL_GITHUB_CLIENT_ID` / `_CLIENT_SECRET` | `github` | from the GitHub OAuth app |
| `CORS_ALLOWED_ORIGINS` | a browser front end on another origin | e.g. `https://app.example.com`; empty disables CORS |

The app refuses to start when a listed provider is missing its settings, or when a provider is
listed twice. Restart the API after changing these.

| Endpoint | Does |
| --- | --- |
| `GET /api/v1/social/providers` | the enabled providers (`name`, `display_name`), for the login screen; `[]` when off |
| `POST /api/v1/social/{provider}/login` | the provider's proof + `device` → the same body as `POST /login` |

## GitHub

### 1. Create the OAuth app

On GitHub: **Settings → Developer settings → OAuth Apps → New OAuth App**
(<https://github.com/settings/developers>). For an organisation, create it under the
organisation's settings instead, so it does not depend on one person's account.

| Field | Value |
| --- | --- |
| **Application name** | what users see on GitHub's approval page, e.g. your product's name |
| **Homepage URL** | your front end, e.g. `https://app.example.com` |
| **Redirect URI** (callback URL) | the **front-end page** GitHub sends the user back to, e.g. `https://app.example.com/auth/github`. Not the API: the API has no callback route |
| **Allow wildcard matching** | off: an exact URI is safer |
| **Enable Device Flow** | off: not used |
| **Expire user access tokens** | either: the API uses GitHub's token once, to read the e-mail, and discards it |

**Register application**, copy the **Client ID**, then **Generate a new client secret** and copy
it (GitHub shows it once). The client id is public; the secret goes only into the API's
environment.

You can add up to ten redirect URIs to one app, but keep **one OAuth app per environment** (local,
staging, production): separate secrets, and a leaked local secret can't touch production.

### 2. Configure the API

```bash
SOCIAL_PROVIDERS=github
SOCIAL_GITHUB_CLIENT_ID=Ov23li…
SOCIAL_GITHUB_CLIENT_SECRET=…
CORS_ALLOWED_ORIGINS=https://app.example.com
```

### 3. The front end

The front end runs GitHub's redirect itself, with **PKCE** and a **state** value, and sends the
code to the API:

```js
// Login page: start the redirect.
const b64url = bytes => btoa(String.fromCharCode(...bytes)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
const verifier = b64url(crypto.getRandomValues(new Uint8Array(32)))   // PKCE: 43 characters
const state = b64url(crypto.getRandomValues(new Uint8Array(16)))
const challenge = b64url(new Uint8Array(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(verifier))))
sessionStorage.setItem('gh_verifier', verifier)
sessionStorage.setItem('gh_state', state)

location = 'https://github.com/login/oauth/authorize?' + new URLSearchParams({
  client_id: 'YOUR_CLIENT_ID',
  redirect_uri: 'https://app.example.com/auth/github',   // exactly as registered
  scope: 'user:email',                                    // needed to read the e-mail address
  state,
  code_challenge: challenge,
  code_challenge_method: 'S256',
})
```

```js
// https://app.example.com/auth/github: GitHub sends the user back here with ?code=…&state=…
const params = new URLSearchParams(location.search)
if (params.get('state') !== sessionStorage.getItem('gh_state')) throw new Error('state mismatch')

const res = await fetch('https://api.example.com/api/v1/social/github/login', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    code: params.get('code'),
    code_verifier: sessionStorage.getItem('gh_verifier'),
    redirect_uri: 'https://app.example.com/auth/github',
    device: 'web',
  }),
})
const { data } = await res.json()   // data.token — store it like the one from POST /login
```

- **`state`** proves the user started this sign-in from your page (it stops a forged callback);
  the front end checks it, so it never reaches the API.
- **PKCE** (`code_verifier` / `code_challenge`) means a code intercepted on the way back is
  useless without the verifier, which never left the browser.
- **The code is single use and lives ten minutes.** Send it once, right away.
- **Mobile:** the same flow in a browser tab (`ASWebAuthenticationSession`, Custom Tabs) with an
  app-scheme or universal-link redirect URI registered on the OAuth app.

## Google

### 1. Create the OAuth clients

In the [Google Cloud console](https://console.cloud.google.com/apis/credentials): **APIs &
Services → Credentials → Create credentials → OAuth client ID**. Configure the **OAuth consent
screen** first if asked (app name, support e-mail; the `email` and `openid` scopes are enough).

| Client type | For | Settings |
| --- | --- | --- |
| **Web application** | the browser front end | *Authorized JavaScript origins*: `https://app.example.com` (and `http://localhost:5173` for local work) |
| **iOS** | the iOS app | its bundle id |
| **Android** | the Android app | its package name and signing certificate SHA-1 |

Each client has its own client id. Google needs **no secret** on the API: an id_token is checked
with Google's public keys.

### 2. Configure the API

```bash
SOCIAL_PROVIDERS=google
SOCIAL_GOOGLE_CLIENT_IDS=123-web.apps.googleusercontent.com,123-ios.apps.googleusercontent.com,123-android.apps.googleusercontent.com
```

List **every** client id your apps use. A token issued to any other client id is refused, even a
genuine Google token: otherwise any app that can get a user to sign in with Google could sign in
to yours as them.

### 3. The front end

Google Identity Services gives the page an id_token; send it as it is:

```html
<script src="https://accounts.google.com/gsi/client" async></script>
<div id="g_id_onload" data-client_id="123-web.apps.googleusercontent.com" data-callback="onGoogle"></div>
<div class="g_id_signin"></div>
<script>
  async function onGoogle({ credential }) {
    const res = await fetch('https://api.example.com/api/v1/social/google/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id_token: credential, device: 'web' }),
    })
    const { data } = await res.json()   // data.token
  }
</script>
```

**Mobile:** Google Sign-In for iOS and Android (or Credential Manager on Android) returns the same
`idToken`; send it with the device name, e.g. `{"id_token": "…", "device": "iPhone 16"}`.

## The answers

| Case | Answer |
| --- | --- |
| The provider verified the e-mail and a user has it | **200**, the same body as `POST /login`, always with a token |
| The proof is bad: expired, issued to another app, a used or wrong code, a wrong verifier | **401** `INVALID_CREDENTIALS` |
| The provider has not verified the e-mail (GitHub: the primary address) | **403** `ACCOUNT_UNVERIFIED` |
| No user has the e-mail | **403** `SOCIAL_ACCOUNT_NOT_FOUND` |
| The user is inactive, or unverified with us (`AUTH_CHECK_*`) | the same **403** as on password login |
| No `id_token` / `code` in the body | **422** `VALIDATION_FAILED` |
| The provider is not enabled | **404** |
| Too many failed attempts from this IP | **429** `ACCOUNT_LOCKED`, with `retry_after` |
| The provider cannot be reached | **503** `SERVICE_UNAVAILABLE` |

- **Throttling:** failed attempts that the caller caused count against the IP
  (`AUTH_LOGIN_MAX_ATTEMPTS` per `AUTH_LOGIN_DECAY_MINUTES`); a provider outage (503) does not, so
  it can't lock out an office behind one address.
- **Matching is by e-mail,** case-insensitive, among live users. If a user's e-mail with us
  differs from the one at the provider, social login won't find them: they use their password, or
  an admin changes the address. Changing a user's e-mail moves their social login with it.
- **Activity:** every attempt is an `auth.login` entry with `method: github` or `google` (see
  the [activity log](activity-log.md#logins)): successes in the user's tenant, failures as system
  rows with the error code and the e-mail the provider returned.

## In production

- **HTTPS everywhere.** Google only accepts `http://` origins for `localhost`; GitHub accepts any,
  but a code or token over plain HTTP can be read on the way.
- **One OAuth app/client per environment,** each with its own redirect URIs and secret.
- **Secrets in the environment,** like the database password: never in git, never in the front
  end. Rotate the GitHub secret by generating a second one on GitHub, deploying it, then deleting
  the old one.
- **`CORS_ALLOWED_ORIGINS`** lists your real front-end origins only.
- **Users must exist first.** Decide how accounts are created (admin, import, invitation) before
  showing "Sign in with GitHub" to people who have none.

## Troubleshooting

| Symptom | Cause and fix |
| --- | --- |
| GitHub says *"The redirect_uri is not associated with this application"* | the `redirect_uri` in the redirect and in the POST must match a registered one **exactly**, including the trailing `/` |
| Browser console: *blocked by CORS policy* | add the front end's origin to `CORS_ALLOWED_ORIGINS` and restart the API |
| `401 INVALID_CREDENTIALS` from GitHub | the code was used already, is older than ten minutes, or the `code_verifier` is not the one the challenge was made from (e.g. a second tab overwrote it). Start the sign-in again |
| `401 INVALID_CREDENTIALS` from Google, "made for another app" | the page uses a client id missing from `SOCIAL_GOOGLE_CLIENT_IDS` |
| `401 INVALID_CREDENTIALS`, "GitHub did not share the e-mail address" | the redirect did not ask for `scope: 'user:email'` |
| `403 ACCOUNT_UNVERIFIED` | the provider has not verified the address: on GitHub, verify the **primary** e-mail under *Settings → Emails* |
| `403 SOCIAL_ACCOUNT_NOT_FOUND` | no live user has that address. The failed `auth.login` row in the activity log shows which address the provider returned |
| `404` on the login route | the provider is not in `SOCIAL_PROVIDERS`, or the API was not restarted |
| The app does not start: `SOCIAL_PROVIDERS includes github: …` | its client id or secret is missing |

## Adding a provider

Providers are drivers in `internal/authn/social`, one file each, behind one interface:

```go
type Provider interface {
	Name() string        // "gitlab": the {provider} in the URL
	DisplayName() string // "GitLab": for the login screen
	Verify(ctx context.Context, proof Proof) (*Identity, error)
}
```

`Verify` returns the e-mail and whether the provider verified it; `authn.Service.SocialLogin`
does the rest. Two building blocks cover almost every provider:

- **OpenID Connect** (Microsoft, Apple, GitLab…): `newOIDCProvider(name, displayName, issuer,
  keysURL, clientIDs)` verifies an id_token. `google.go` is the whole example.
- **Code exchange** (Facebook, LinkedIn…): `exchangeCode` trades the code for an access token;
  then read the provider's profile API for the e-mail. `github.go` is the example.

The recipe:

1. `internal/authn/social/<name>.go`: build the provider and decide when the e-mail counts as
   verified. When unsure, it does not. (Microsoft's `email` claim, for example, is only verified
   when `xms_edov` is true; Facebook returns only confirmed addresses.)
2. Its settings in `internal/config/social.go` (`SOCIAL_<NAME>_*`, the check in `loadSocial`), a
   case in `social.New`, and the lines in `.env.example`.
3. A test against a fake provider (`httptest`), like `oidc_test.go` or `github_test.go`.
4. A section on this page: where to create the app and what the client sends.

Nothing else changes: the endpoint, the rules, the activity log and the response are shared.
