---
name: add-module
description: Use when adding a new feature/CRUD module to this Go starter (internal/modules/<name>), adding an endpoint to an existing module, or porting a Laravel module — covers the generator, migration, sqlc queries, repository, service, handler (httpx.Kit), routes with permissions, wiring, OpenAPI, activity log and testkit tests.
---

# Adding a module (or an endpoint)

`AGENTS.md` "Module anatomy" is the reference; this skill is the execution recipe.
Reference implementations: `internal/modules/branches` (small, canonical),
`internal/modules/identity` (rich), `internal/modules/billing` (big-module layout).

## 1. Start from the generator

```bash
# binary: ims gen module ...   |   from source: go run ./cmd/app gen module ...
go run ./cmd/app gen module --name=product --fields="name:string,description:text?,price:decimal,is_active:bool,launch_on:date"
```

It writes `migrations/000NN_create_products.sql`, `sql/queries/products.sql`,
`internal/modules/product/{model,repository,service,request,handler,routes,service_test}.go`,
runs sqlc and refreshes the OpenAPI regions. The output already follows every rule below; adapt
it (global vs tenant-scoped, branch scope, extra endpoints). Field types: `string text int bigint
decimal bool date datetime json`; `?` = optional.

## 2. Order of work (never skip steps)

1. **Decide the shape** — tenant-scoped or global, branch-scoped or not, which permissions,
   which changes belong in the activity log. Note non-obvious choices for step 14.
2. **Migration + queries** — follow the `db-change` skill. Scoped tables carry `tenant_id`
   (and `branch_id`), audit columns, `deleted_at`, partial unique indexes. Single-row queries
   take the tenant predicate `(@all_tenants::boolean OR tenant_id = @tenant_id)`.
3. **model.go** — domain struct + mapper (`pgtype.Timestamptz` → `time.Time` / `*time.Time`).
4. **repository.go** — interface + sqlc impl. Single-row methods take a `tenancy.Filter`
   (`scope.Filter()`; a zero filter matches nothing). `pgx.ErrNoRows` →
   `apperr.Newf(apperr.ResourceNotFound, ...)`; other errors → `database.MapError("x.op", err)`.
5. **service.go** — business rules, tenancy from the `tenancy.Scope` argument, stamping
   tenant/actor, transactions (`TxManager.WithTx`; plan limits: `QuotaFor` before `WithTenantLock`, then only
   `q`-bound queries inside the lock), and
   `s.activity.Record(ctx, activity.Entry{...})` after changes tenant admins should see. Every
   exported write opens a span: name the `err` result, then
   `ctx, end := tracing.Begin(ctx, "<module>.<operation>", attribute.Int64("tenant.id", tenantID))`
   and `defer end(&err)` (ids and counts only; `TestServiceWritesOpenASpan` checks it).
   **No permission checks here** — those are on the route. Optional collaborators are
   `WithX(...)` options so unit tests can leave them out.
6. **request.go** — request structs with `validate:` tags; response DTOs + `NewXResponse`.
7. **handler.go** — embed `httpx.Kit`; actions are bind → service → respond:

   ```go
   type Handler struct {
       service *Service
       checker *authz.Checker
       httpx.Kit
   }

   func NewHandler(service *Service, checker *authz.Checker, kit httpx.Kit) *Handler {
       return &Handler{service: service, checker: checker, Kit: kit}
   }

   func (h *Handler) can(permission string) func(http.Handler) http.Handler {
       return authz.Require(h.checker, permission, h.Responder)
   }

   func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
       id, ok := h.PathID(w, r, "id")
       if !ok {
           return
       }
       var req InsertUpdateRequest
       if !h.Bind(w, r, &req) {
           return
       }
       item, err := h.service.Update(r.Context(), tenancy.From(r), authn.UserID(r), id, req.toInput())
       if err != nil {
           h.Error(w, r, err)
           return
       }
       h.Data(w, r, http.StatusOK, NewResponse(*item))
   }
   ```

   Lists: `page, perPage, offset := httpx.PageParams(r)`, `httpx.OrderParams(r, "id", "name")`,
   `meta, links := httpx.NewPagination(r, page, perPage, total)`, `h.Paginated(...)`.
   Use `h.checker.MustCan(...)` in a handler only when the permission depends on the payload.
8. **routes.go** — registration + permission per route:
   `r.With(h.can(PermCreate)).Post("/product", h.store)`. Mount in `internal/app/routes.go` in the
   right group (public / authenticated / tenant-resolved / tenant-exempt). Confirm each route is
   mounted with its permission and group middleware:
   `go run ./cmd/app route:list --path=/api/v1/product --middleware` (`--json` to parse it).
9. **Wiring** — `internal/app/wiring.go` (HTTP; handlers get the shared `kit`) or
   `internal/app/platform.go` (shared with CLI, jobs, scheduler). Pass `platform.Activity` via
   `WithActivity`. Recurring tasks go in the list in `internal/app/schedule.go`.
10. **Permissions** — constants + catalog entries in `internal/authz/catalog.go`; add the group to
    `AdminTenantPermissions` if tenant admins should hold them; run `permission:sync`.
11. **OpenAPI** — `go run ./cmd/app gen openapi --module=<name>` for generated regions, or
    hand-write outside the `BEGIN/END GENERATED` markers. `make openapi-check` must report
    "no route drift". Dashboard types: `cd web/dev && npm run openapi:gen`.
    To expose an operation to AI agents, uncomment the generated `# x-mcp:` line (or add
    `x-mcp: {tool: <snake_name>}`); regeneration keeps it. Only when asked: tools inherit the
    route's permission and tenancy, and a DELETE must be marked `destructive: true`. Test the
    tool next to the REST test: `testkit.New(t, testkit.WithMCP())`, `app.Agent(t, user,
    testkit.AllowChanges()).Call(t, "list_x", nil).OK(t)` (see `docs/guides/testing.md`).
12. **Tests** — service unit tests with fakes; an integration test with `testkit` in
    `package <name>_test`, including cross-tenant isolation (read, update, delete denied):

    ```go
    app := testkit.New(t)
    admin := app.User(t)                                               // Admin of tenant 1
    other := app.User(t, testkit.InTenant(app.Tenant(t, "Tenant B")))  // Admin of tenant B
    var created struct{ ID int64 `json:"id"` }
    app.As(admin).Post("/api/v1/product", `{"name":"Pen"}`).Status(t, http.StatusCreated).Data(t, &created)
    app.As(other).Get(fmt.Sprintf("/api/v1/product/%d", created.ID)).Status(t, http.StatusNotFound)
    ```

    A route without `can:` fails `route_permissions_integration_test.go` unless it is meant to be
    open to every authenticated user — then add it to that test's allowlist with a reason.
13. **Verify** — the `verify-changes` skill.
14. **Record** — add non-obvious decisions to `docs/architecture/decisions.md` (and `AGENTS.md`);
    update the developer guide if you introduced a new pattern.

## Gotchas

- **Personal data:** a table with a user id (`user_id`, `*_user_id`, a polymorphic `owner_id`/
  `target_id`, an FK to `users`) or a person's details needs a `PrivacyContributor` in the module's
  `privacy.go` (`Columns`, `Export`, `Erase`; see `internal/modules/notifications/privacy.go`),
  registered in `internal/app/privacy.go` before identity, or an exemption there with the reason.
  `created_by`/`updated_by` are covered already (they point at the erased user's tombstone).
  `TestCoverage` in `internal/modules/privacy` fails until the column is covered.

- Reserved SQL words (`group`): quote the column, rename the param (`@group_name`).
- Every route must be in `api/openapi.yaml`, or `make openapi-check` fails.
- Do not import `httpx`/`authn` in services or repositories (`authn.UserIDFromContext` is fine in
  services that run outside HTTP too).
