---
title: Handlers and routes
description: The httpx.Kit handler pattern, route permissions, pagination, filters and the response envelope.
layers: [api]
---

## The handler pattern

Every handler embeds `httpx.Kit`, so an action reads like a Laravel controller method:

```go
type Handler struct {
	service *Service
	checker *authz.Checker
	httpx.Kit // Bind, Validate, PathID, Data, Error, NoContent, Paginated, Cursor, Message
}

func NewHandler(service *Service, checker *authz.Checker, kit httpx.Kit) *Handler {
	return &Handler{service: service, checker: checker, Kit: kit}
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := h.PathID(w, r, "id") // positive int64 from {id}; writes the 400 itself
	if !ok {
		return
	}
	var req InsertUpdateRequest
	if !h.Bind(w, r, &req) { // decode JSON + run `validate` tags; writes 400/415/422
		return
	}
	branch, err := h.service.Update(r.Context(), tenancy.From(r), authn.UserID(r), id, req.toInput())
	if err != nil {
		h.Error(w, r, err) // *apperr.Error → RFC 9457 problem
		return
	}
	h.Data(w, r, http.StatusOK, NewResponse(*branch))
}
```

The kit is built **once** in `internal/app/wiring.go` (`httpx.NewKit(binder, validator,
responder)`) and handed to every `NewHandler` — the developer dashboard's handler included.

| Helper | Does |
| --- | --- |
| `h.Bind(w, r, &req)` | strict JSON decode + validation; on failure writes the problem and returns `false` |
| `h.Validate(w, r, &req)` | validation only — for query-string or multipart structs you filled yourself |
| `h.PathID(w, r, "id")` | reads a positive integer route parameter, 400 otherwise |
| `h.Data(w, r, status, v)` | `{"data": v}` |
| `h.Paginated(w, r, status, data, meta, links)` | `{"data": [...], "links": {...}, "meta": {...}}` |
| `h.Cursor(w, r, status, data, meta, links)` | cursor-paginated lists |
| `h.Message(w, r, status, "Done.")` | `{"message": "Done."}` |
| `h.NoContent(w)` | 204 |
| `h.Error(w, r, err)` | the right problem response for any error |
| `authn.UserID(r)` | the signed-in user (`auth()->id()`) |
| `tenancy.From(r)` | the resolved tenant scope |

## Routes and permissions

A module registers its routes in `routes.go`, and **each route declares its permission** — the
equivalent of Laravel's `->middleware('can:...')`:

```go
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.With(h.can(authz.PermBranchViewAll)).Get("/branch", h.index)
	r.With(h.can(authz.PermBranchView)).Get("/branch/{id}", h.show)
	r.With(h.can(authz.PermBranchCreate)).Post("/branch", h.store)
	r.With(h.can(authz.PermBranchUpdate)).Patch("/branch/{id}", h.update)
	r.With(h.can(authz.PermBranchDelete)).Delete("/branch/{id}", h.destroy)
}

// can is route middleware requiring a permission.
func (h *Handler) can(permission string) func(http.Handler) http.Handler {
	return authz.Require(h.checker, permission, h.Responder)
}
```

Only when the permission depends on the **payload** — sending `roles` while creating a user needs
`user.role.assign` — does a handler check inside the action:

```go
if len(req.Roles) > 0 {
	if err := h.checker.MustCan(r.Context(), authn.UserID(r), authz.PermUserRoleAssign); err != nil {
		h.Error(w, r, err)
		return
	}
}
```

:::note[Why permissions live at the edge]
CLI commands, queue jobs and scheduled tasks are trusted code and don't re-check permissions —
exactly like artisan commands and queued jobs. Services stay permission-free. What services
**always** enforce, whoever calls them, is tenant scoping and business invariants. See
[Permissions](./permissions.md).
:::

The module's `RegisterRoutes` is mounted into a route group in `internal/app/routes.go`; the group
decides authentication and tenancy (see [How it works](../getting-started/how-it-works.mdx#route-groups)).

### Listing the routes

`ims route:list` prints the live route table, like Laravel's `route:list`: every method and path
with the permission it requires and its handler. The dashboard's **Inspector → Routes** shows the
same table.

```bash
ims route:list --path=/api/v1/branch          # filter by path (also --method, --except-path)
ims route:list --method=DELETE --middleware   # add each route's middleware
ims route:list --json                         # the full table for scripts and AI agents
```

```text
METHOD  PATH                 PERMISSION       HANDLER
GET     /api/v1/branch       branch.view.all  branches.(Handler).index
POST    /api/v1/branch       branch.create    branches.(Handler).store
GET     /api/v1/branch/{id}  branch.view      branches.(Handler).show
```

A `-` in the permission column means the route needs none: public routes, or ones listed as
self-service in `route_permissions_integration_test.go`.

### Exposing an operation to AI agents

Add `x-mcp: {tool: list_products}` to the operation in `api/openapi.yaml` and it becomes a
tool on the MCP server, served through this router with the same gates. No Go code. See
[MCP server](./mcp.md#tools-come-from-the-openapi-document).

## Duplicate requests

Every write behind sign-in goes through `internal/platform/dedupe` (mounted in
`internal/app/routes.go` after authentication), so a handler never has to think about double
submits:

- the **guard** refuses the same caller sending the same method, path and body while the first
  runs, or for `DEDUPE_WINDOW` (default `3s`) after it succeeded: `409 DUPLICATE_REQUEST`;
- an **`Idempotency-Key`** from the client replays the first answer for `IDEMPOTENCY_TTL`
  (default `24h`) instead.

Tune it in `dedupeOptions` (`internal/app/routes.go`):

```go
Skip: []string{"POST /api/v1/notification/read-all"},              // repeats are normal here
Windows: map[string]time.Duration{"POST /api/v1/billing/subscribe": 30 * time.Second}, // money
```

Routes are named by method and chi pattern. State is in the cache: with `redis` or `database` it
holds across servers; with `memory` it protects one process (the app warns about per-process
caches outside local development). A cache outage lets requests through. `DEDUPE_ENABLED=false`
turns it off.

## Pagination

Management lists use **page-based** pagination:

```go
page, perPage, offset := httpx.PageParams(r)                  // ?page=, ?per_page= (default 10, max 100)
orderBy, orderDir := httpx.OrderParams(r, "id", "name", "created_at") // whitelisted ?order_by=&order_direction=
items, total, err := h.service.List(r.Context(), tenancy.From(r), ListFilter{
	Search: r.URL.Query().Get("search"), OrderBy: orderBy, OrderDir: orderDir,
	Limit: int32(perPage), Offset: int32(offset),
})
meta, links := httpx.NewPagination(r, page, perPage, total)
h.Paginated(w, r, http.StatusOK, data, meta, links)
```

Feeds and streams (notifications, activity, dashboard telemetry) use **cursor** pagination:
`?limit=` (default 20–25, max 100) and `?cursor=`, answered with `h.Cursor` and links from
`httpx.CursorURL`.

`order_by` is always **whitelisted** per endpoint — an unknown column falls back to the default,
so user input never reaches `ORDER BY`.

## The response envelope

| Response | Shape |
| --- | --- |
| a resource | `{"data": {...}}` |
| a list | `{"data": [...], "links": {first, last, prev, next}, "meta": {current_page, per_page, total, …}}` |
| a message | `{"message": "…"}` |
| deleted / logged out | `204 No Content` |
| any error | `application/problem+json` (RFC 9457) — see [Validation & errors](./validation-and-errors.md) |

Timestamps are RFC 3339 in UTC. Money is an integer in minor units (paisa). IDs are 64-bit integers.
