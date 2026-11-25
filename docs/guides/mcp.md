---
title: MCP server (AI agents)
description: Let Claude, ChatGPT, Cursor and other AI agents work with your API as a connector, with the user's own permissions.
layers: [api, app]
---

The API can be added to an AI client **as a connector**. The user pastes one URL, signs in with
the account they already have, approves access, and the agent works with the API as that user:
the same permissions, tenant, plan limits, validation and audit trail as a REST call. Nothing about
authorization is re-implemented for agents.

It follows the [Model Context Protocol](https://modelcontextprotocol.io) (revision 2026-07-28,
and 2025-11-25 for older clients) and its OAuth 2.1 authorization spec. The same URL works in
claude.ai, Claude Desktop, Claude mobile, Claude Code, ChatGPT, Cursor, VS Code and the MCP
Inspector.

Here the API is the OAuth **server** that AI apps sign in to. That is the opposite of
[social login](social-login.md), where the API is a **client** of Google or GitHub; the two are
separate features. [OAuth in this kit](oauth.md) puts them side by side.

## Turn it on

```bash
MCP_ENABLED=true
APP_URL=https://api.example.com   # must be the public https URL: it is the OAuth issuer
```

With `MCP_ENABLED=false` (the default), `/mcp`, `/oauth/*` and the `/.well-known/oauth-*`
documents answer 404. Run `ims migrate` once for the OAuth tables (migration `00027`).

| Setting | Default | |
| --- | --- | --- |
| `MCP_ACCESS_TOKEN_TTL` | `1h` | access token lifetime |
| `MCP_REFRESH_TOKEN_TTL` | `720h` | refresh token lifetime; every refresh issues a new one, so an active connection never expires |
| `MCP_OAUTH_CIMD_ENABLED` | `true` | let any client connect without registration (below) |
| `MCP_OAUTH_ALLOWED_CLIENT_HOSTS` | empty | limit those clients to some hosts, e.g. `claude.ai` |
| `MCP_OAUTH_SECRET_GRACE` | `24h` | how long a rotated client secret keeps working |
| `MCP_MAX_REQUEST_BYTES` / `MCP_MAX_RESULT_BYTES` | 1 MiB / 256 KiB | request and tool-result caps |
| `MCP_RATE_LIMIT` | `120` | tool calls per minute per token (on top of the per-IP limit) |

## Two ways for a client to connect

**1. As a connector, with no registration (the default).** The client identifies itself with a
*Client ID Metadata Document* (CIMD): its `client_id` is an https URL that serves a small JSON
file with its name and redirect URIs. The server fetches and checks it; nobody registers anything.
This is how Claude, Claude Code, ChatGPT, Cursor and VS Code connect.

**2. Manually registered, when someone needs it.** An internal admin registers a client and hands
out its `client_id` (and secret). Use it for a client without CIMD, your own agent or automation
platform, or when you want a fixed list of allowed clients (set `MCP_OAUTH_CIMD_ENABLED=false`).

```bash
# API (needs the system permission oauth_client.manage; Super Admin has it)
curl -X POST https://api.example.com/api/v1/oauth-client -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Ops agent","type":"confidential","redirect_uris":["https://agent.example.com/callback"]}'

# or the CLI
ims oauth:client:create --name "Ops agent" --redirect https://agent.example.com/callback --confidential
ims oauth:client:list
ims oauth:client:rotate 3      # new secret; the old one works for MCP_OAUTH_SECRET_GRACE
ims oauth:client:delete 3      # revokes every connection made with it
```

A confidential client's secret is shown once. `type: public` clients have no secret and use PKCE
only. `scopes` caps what the client can ever get, and `tenant_id` limits it to one tenant's users.
Deactivating or deleting a client disconnects its agents.

## Add it to a client

| Client | How |
| --- | --- |
| claude.ai, Claude Desktop, mobile | **Customize → Connectors → Add custom connector**, paste `https://api.example.com/mcp`. For a registered client, open **Advanced settings** and paste its client ID and secret. |
| Claude Code | `claude mcp add --transport http shop https://api.example.com/mcp`, then `/mcp` to sign in |
| Cursor, VS Code | add the URL as a remote (HTTP) MCP server in their MCP settings |
| MCP Inspector | `npx @modelcontextprotocol/inspector`, transport *Streamable HTTP*, the URL, then *Connect* |

A browser window opens on the API's own sign-in page (the same rules as `POST /api/v1/login`:
lockout, inactive accounts, OTP). The consent page then shows the app, where it returns to, and:

- **Allow changes**, unticked by default: without it the connection can only read (`mcp:read`);
  with it, it can also create and update (`mcp:write`).
- For a **platform account** (super admin, internal staff), the tenant to work in, or "no tenant:
  system work only". The connection then acts like `X-Acting-Tenant` / `X-Acting-Branch` (the
  tenant's first branch) on every call. Tenant users get no picker: their tenant is their own.

Users list and remove their connections with `GET /api/v1/user/connections` and
`DELETE /api/v1/user/connections/{id}`. A password change, a deactivated account and "log out
other devices" disconnect them too.

### On your computer

The [Connect an AI agent](../tutorial/connect-an-ai-agent.mdx) tutorial walks through it step by
step: turning the server on, Claude Desktop (through the `mcp-remote` bridge, with a registered
client), Claude Code, OpenCode, checking what the agent did, and troubleshooting. In short:

| App | On `localhost` |
| --- | --- |
| Claude Code | `claude mcp add --transport http ims-local http://localhost:8080/mcp`; no registration |
| Claude Desktop | `npx mcp-remote http://localhost:8080/mcp --allow-http --static-oauth-client-info '{"client_id":"ims_…"}'` in its config, with a client registered for `http://localhost/oauth/callback` |
| OpenCode 1.x | `"oauth": {"clientId": "ims_…"}` on a remote server, with a client registered for `http://127.0.0.1/mcp/oauth/callback` |
| claude.ai, Claude Desktop's **Add custom connector** | they connect from Anthropic's servers: they need a public https URL (deployed, or a tunnel to the API on port 8081 under `make dev`, with `APP_URL` set to it) |

The MCP Inspector runs in your browser: add its origin to `CORS_ALLOWED_ORIGINS` if you set any.

## Tools come from the OpenAPI document

An operation becomes a tool with an `x-mcp` mark in `api/openapi.yaml`. No Go code:

```yaml
/api/v1/branch:
  post:
    operationId: createBranch
    x-mcp: {tool: create_branch}   # scope defaults to mcp:read for GET, mcp:write otherwise
```

| Key | Default |
| --- | --- |
| `tool` | the operationId in snake_case |
| `title`, `description` | the operation's summary and description |
| `scope` | `mcp:read` for GET, `mcp:write` otherwise (a write can't be `mcp:read`) |
| `readOnly`, `destructive`, `idempotent` | from the method (DELETE must be destructive) |

The tool's input is the operation's path and query parameters plus its JSON body under `body`;
its output schema is the 2xx response. Acting headers are never arguments: they come from the
connection. Multipart operations (uploads) can't be tools. Startup and the tests fail on a bad
mark, a duplicate name, or a DELETE not marked destructive.

`ims gen module` writes a commented `# x-mcp:` line on the list operation it generates; uncomment
it to expose the list. `ims gen openapi` keeps a mark you enabled.

The kit exposes five tools, enough to prove reads, writes, tenancy and audit: `whoami`,
`list_branches`, `create_branch`, `list_users`, `create_user`. Which operations a product exposes
is its own choice.

Test a tool next to its REST test with `testkit`: `app.Agent(t, user, testkit.AllowChanges())`
then `agent.Call(t, "list_products", nil).OK(t)`. See
[Testing an MCP tool](./testing.md#testing-an-mcp-tool).

## How a tool call runs

A tool does not call a service. It serves the matching operation **through the `/api/v1` router,
in-process**:

```
agent ─POST /mcp─▶ origin check ▶ bearer token ▶ scope check ▶ rate limit ▶ MCP server
   tool "create_branch" ▶ POST /api/v1/branch (the user's identity, the connection's tenant)
       ▶ tenancy.Resolve ▶ branch.create permission ▶ plan limit ▶ handler ▶ service ▶ SQL
```

- The identity goes into the sub-request's context; the token never leaves `/mcp`.
- 2xx: the JSON body is the tool's `structuredContent` (and text). 4xx: a tool error carrying the
  problem document, so the model can fix a validation error or tell the user they lack a
  permission. 5xx: only the title, code and request id.
- A write tool called with a read-only token answers HTTP 403 with
  `WWW-Authenticate: Bearer error="insufficient_scope"`, so the client can ask for more.

## Tokens and separation

- An MCP access token is a personal access token tied to its connection, with scopes and the
  audience `APP_URL/mcp`. It is accepted **only** on `/mcp`; `/api/v1` and `/dev` refuse it. A
  session token is refused on `/mcp`. A leaked agent token can't call the REST API, and a leaked
  mobile session can't drive an agent.
- Refresh tokens rotate on every use. A refresh token (or authorization code) presented twice
  revokes the whole connection.

## Audit

- Every activity entry written during a tool call carries `"_via": {"source": "mcp", "tool": …,
  "client": …}` in its changes.
- `oauth.authorized` and `oauth.revoked` are written to the user's tenant log (the system log for
  platform accounts); `oauth_client.*` changes are system entries.
- The access log adds `via=mcp mcp_tool=…`; the dashboard's request list shows an **MCP** chip;
  `ims_mcp_tool_calls_total{tool,outcome}` counts calls.

## Security notes

- Results contain user-entered data, so treat prompt injection as given. The protections are
  structural: RBAC and tenant scoping bound what an agent can do, connections are read-only unless
  the user allows changes, write tools carry hints so clients confirm them, and there are no delete
  tools in the kit.
- The CIMD fetch is SSRF-guarded: https on port 443 only, public addresses only (checked after DNS,
  at dial time), no redirects, 5 s, 64 KiB, cached 5 minutes to 24 hours.
- A bad `client_id` or redirect URI shows an error page and never redirects. Loopback redirect URIs
  (`http://localhost`, `http://127.0.0.1`, `http://[::1]`) match on any port and any of those
  three hosts, as native apps pick both; the path must match. The consent page warns that "an app
  on this computer" will connect.
- The sign-in and consent pages carry CSRF tokens, `X-Frame-Options: DENY` and
  `frame-ancestors 'none'`. Codes, verifiers and tokens are hashed at rest and never logged.

## In the app

Members see the AI assistants they connected under **Account → Connected apps** (`/settings/connections`) and can disconnect one there, which revokes its access.
