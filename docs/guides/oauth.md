---
title: OAuth in this kit
description: The API uses OAuth twice, in opposite roles — as a client of Google and GitHub for social login, and as a server for AI agents. What each one is, how they differ, and the vocabulary.
layers: [api]
---

The API speaks OAuth in two places, and they are easy to mix up because they share the
vocabulary. They are **opposite roles** and two separate features that don't share code or
tokens:

| | Social login | MCP connector |
| --- | --- | --- |
| Our role | OAuth **client**: we ask Google or GitHub who the user is | OAuth **server**: AI apps ask us to let them act as the user |
| Who shows the sign-in page | Google or GitHub | our API, at `/oauth/authorize` |
| What the user approves | "share my e-mail address with this app" | "let this AI app act as me" |
| What comes out | our normal session token, for `/api/v1` | an MCP access token, accepted only on `/mcp` |
| Who holds a secret | our API: the GitHub client secret (Google needs none) | the AI app, only if it is a registered client with a secret; most connect with none (PKCE instead) |
| Code | `internal/authn/social` | `internal/modules/oauth` |
| Turned on by | `SOCIAL_PROVIDERS` | `MCP_ENABLED` |
| Guide | [Social login](social-login.md) | [MCP server](mcp.md) |

## OAuth in one paragraph

OAuth lets one app get access on a user's behalf **without ever seeing the user's password**. The
app sends the user to the service that knows them (the *authorization server*); the user signs in
there and approves; the service sends the user back with a short-lived **code**; the app trades
the code for a **token**. With PKCE, the app proves on that trade that it is the one that started
the sign-in, so a stolen code is useless.

**OpenID Connect** (OIDC) is a layer on top: besides the access token, the service hands over an
**id_token**, a signed statement of who the user is (`sub`, `email`, `email_verified`). Anyone
can check its signature with the service's published keys.

## Role 1: we are the client (social login)

```text
 Your front end             Google / GitHub                  Our API
 ──────────────             ───────────────                  ───────
 "Sign in with GitHub" ───▶ user signs in, approves
                       ◀─── code (GitHub) or id_token (Google)
 POST /api/v1/social/{provider}/login ─────────────────────▶ verify with the provider
                                                              → a verified e-mail
                                                              → our user with that e-mail
                       ◀──────────────────── our normal login response and token
```

- Google and GitHub are the authorization servers; **we are only asking who the user is**.
- Google speaks OIDC: its id_token is checked with Google's public keys, so the API needs no
  secret. GitHub speaks plain OAuth: the API trades the code for a GitHub token with its client
  secret, reads the e-mail from GitHub's API and throws the token away.
- The result is **our own** token, the same one `POST /login` gives. Google's and GitHub's tokens
  are never stored and never used again.
- The front end runs the provider's redirect; our API has no callback route and keeps no state.

## Role 2: we are the server (MCP connector)

```text
 AI app (Claude, Cursor…)           Our API
 ────────────────────────           ───────
 calls /mcp ──────────────────────▶ 401: sign in at /oauth/authorize
 opens the browser ───────────────▶ our sign-in page: password (+ OTP) → consent
                  ◀──────────────── code
 trades code + PKCE verifier ─────▶ /oauth/token → MCP access + refresh token
 calls /mcp with the token ───────▶ tools run as the user, with the user's permissions
```

- The API is the authorization server: it shows the sign-in and consent pages, issues codes and
  tokens, rotates refresh tokens, and lets the user revoke a connection.
- The AI app is the client. It identifies itself with a Client ID Metadata Document or a client an
  admin registered.
- The token it gets is an **MCP token**: scoped (`mcp:read`, `mcp:write`), tied to one
  connection, and accepted **only** on `/mcp`.

## The tokens never cross

| Token | Works on | Refused on |
| --- | --- | --- |
| Session token (password or social login) | `/api/v1`, `/dev/api` | `/mcp` |
| MCP access token | `/mcp` | everything else |
| Google / GitHub tokens | not accepted anywhere; the API discards them | — |

A leaked agent token can't call the REST API, and a leaked session can't drive an agent.

## Common confusions

- **"Social login gives an agent access."** No. A social login ends in a session token for your
  front end. An agent always goes through the MCP sign-in and consent.
- **"The MCP sign-in page has Sign in with Google."** Not today: it takes the account's password
  (and OTP when enabled). Social login is for the API's own login.
- **"We need a callback URL on the API for GitHub."** No. The callback is your front-end page; it
  posts the code to the API.
- **"Google needs a client secret on the API."** No. An id_token is verified with Google's public
  keys. Only providers without an id_token (GitHub) need the secret.
- **"Turning on one turns on the other."** No: `SOCIAL_PROVIDERS` and `MCP_ENABLED` are
  independent, and either can be on alone.

## Vocabulary

| Term | Meaning here |
| --- | --- |
| **Authorization server** | the service that signs the user in and issues codes and tokens: Google/GitHub for social login; our API for MCP |
| **Client** | the app that wants access: our API (social login); an AI app (MCP) |
| **Resource server** | where the token is used: GitHub's API, briefly (social login); `/mcp` (MCP) |
| **Authorization code** | a one-time, short-lived value returned after the user approves; traded for tokens |
| **PKCE** | `code_verifier` / `code_challenge`: proves the trade comes from whoever started the sign-in |
| **state** | a random value the starting page checks on return, so a forged callback is ignored |
| **Redirect URI** | where the browser is sent back with the code; compared exactly |
| **Scope** | what is asked for: `user:email` (GitHub), `mcp:read` / `mcp:write` (MCP) |
| **id_token** | OIDC's signed "who this is" statement (Google) |
| **Access token / refresh token** | what calls an API / what gets a new access token without signing in again |
