---
title: Testing
description: Unit tests with fakes, integration tests with testkit, tenant isolation tests and the safety nets that run in CI.
layers: [database, api]
---

## Two kinds of tests

| | Unit | Integration |
| --- | --- | --- |
| Lives | next to the code (`package product`) | the module's external package (`package product_test`) or `internal/app` |
| Uses | fakes for repositories and collaborators | a real Postgres schema, the real router |
| Needs | nothing | `DATABASE_TEST_DSN` — skipped without it |
| Run | `make test` / `go test ./...` | `make test-integration` |

```bash
export DATABASE_TEST_DSN="postgres://postgres:postgres@127.0.0.1:5432/ims_go_api_test?sslmode=disable"
go test -count=1 ./...                 # everything
go test -count=1 -race -p 2 ./...      # race detector (needs cgo; -p 2 keeps memory down)
go test -count=1 -run TestProducts -v ./internal/modules/product/
```

:::caution[A skipped test is not a passing test]
Without `DATABASE_TEST_DSN` integration tests **skip** and report `ok`. Run with `-v` to confirm
a new test actually ran.
:::

## Integration tests with testkit

`internal/testkit` gives you the fully wired application on a fresh, migrated, seeded schema —
like a Laravel feature test:

```go
func TestBranchCreate(t *testing.T) {
	app := testkit.New(t)       // clean schema, seeded, router built
	admin := app.User(t)        // Admin of the seeded tenant, signed in

	app.As(admin).Post("/api/v1/branch", `{"name":"Dhaka"}`).Status(t, http.StatusCreated)

	viewer := app.User(t, testkit.Role(app.Role(t, "Viewer", "branch.view.all")))
	app.As(viewer).Post("/api/v1/branch", `{"name":"Sylhet"}`).Status(t, http.StatusForbidden)
}
```

| Helper | Gives you |
| --- | --- |
| `testkit.New(t, opts...)` | the app; empties every table first (`WithLogger` to see logs) |
| `app.User(t, opts...)` | a signed-in user — Admin of the seeded tenant by default |
| `testkit.Role(name)`, `testkit.InTenant(id)`, `testkit.Email(e)`, `testkit.Name(n)` | user options |
| `app.Role(t, name, perms...)` | a tenant role with exactly those permissions |
| `app.Tenant(t, name)` | another tenant (with a default branch, on an unlimited plan) |
| `app.SuperAdmin(t)` | the seeded super admin |
| `app.DefaultBranch(t, tenantID)` | a tenant's default branch id |
| `app.As(u)` / `app.Guest()` | a request client |
| `.InTenant(tenantID, branchID)` | acting headers, for platform accounts |
| `.Header(k, v)` | any header |
| `.Get/Post/Patch/Put/Delete(path, body)` | body is a string or any value (JSON-encoded) |
| `.Status(t, code)` | asserts the status (prints the body on failure) |
| `.Data(t, &v)` / `.JSON(t, &v)` | decodes `data` / the whole body |
| `app.Deps`, `app.Pool`, `app.Config` | everything else |

Testkit tenants sit on an **unlimited** plan so quotas never get in the way. To test a limit,
create a package and assign it — see `internal/app/quota_concurrency_integration_test.go`.

### Every module needs an isolation test

```go
other := app.User(t, testkit.InTenant(app.Tenant(t, "Tenant B")))
path := fmt.Sprintf("/api/v1/product/%d", id)
app.As(other).Get(path).Status(t, http.StatusNotFound)
app.As(other).Patch(path, `{"name":"x"}`).Status(t, http.StatusNotFound)
app.As(other).Delete(path).Status(t, http.StatusNotFound)
```

### Testing an MCP tool

When you mark an operation `x-mcp`, test it as a tool too. `app.Agent` connects an agent the way
Claude would: it signs in on the authorization page, consents and redeems the code, so the test
goes through the real OAuth flow, `/mcp` and the dispatcher:

```go
func TestProductTools(t *testing.T) {
	app := testkit.New(t, testkit.WithMCP())        // MCP_ENABLED=true for this app
	admin := app.User(t)
	agent := app.Agent(t, admin, testkit.AllowChanges())

	var product struct{ Name string `json:"name"` }
	agent.Call(t, "create_product", map[string]any{"body": map[string]any{"name": "Tea"}}).OK(t).Data(t, &product)
	agent.Call(t, "create_product", map[string]any{"body": map[string]any{}}).Refused(t, "VALIDATION_FAILED")

	other := app.User(t, testkit.InTenant(app.Tenant(t, "Tenant B")))
	var seen []struct{ Name string `json:"name"` }
	app.Agent(t, other).Call(t, "list_products", nil).OK(t).Data(t, &seen) // tenant B sees none of A's
}
```

| Helper | Gives you |
| --- | --- |
| `testkit.WithMCP()` | an option for `New`: the MCP server on |
| `app.Agent(t, user, opts...)` | an agent signed in as `user`; read-only by default |
| `testkit.AllowChanges()`, `testkit.WorkIn(tenantID)` | tick "Allow changes"; the tenant a platform account picks |
| `agent.Call(t, tool, args)` | calls a tool; `args` is a map or nil |
| `.OK(t)`, `.Refused(t, code)`, `.Data(t, &v)` | asserts success / a refusal with that `error_code`; decodes `data` |
| `agent.Tools(t)`, `agent.Send(method, params)` | the tool names; a raw JSON-RPC call for transport checks (401, 403) |
| `agent.AccessToken`, `agent.RefreshToken`, `app.ClientID(t)` | for token-endpoint tests |

`internal/modules/branches/testkit_example_test.go` has a complete example, and
`internal/app/mcp_integration_test.go` covers the connector itself.

## How test databases work

`dbtest` gives **every test package its own Postgres schema** (named after the package directory)
with the goose and River migrations applied once. Packages run in parallel against one database
without stepping on each other. Inside a package, `testkit.New` truncates the application tables,
so tests within one package must not use `t.Parallel()`.

For repository-level tests without the whole app: `dbtest.Queries(t)` and
`dbtest.Clean(t, pool, "table", ...)`.

## Unit tests with fakes

Services take interfaces, so unit tests swap in fakes:

```go
type fakeRepo struct{ created []product.CreateInput }

func (f *fakeRepo) Create(_ context.Context, in product.CreateInput) (*product.Product, error) {
	f.created = append(f.created, in)
	return &product.Product{ID: 1, TenantID: in.TenantID}, nil
}
```

Optional collaborators are nil-safe (`activity.Recorder`, notifiers), so a unit test only
provides what it exercises.

## Safety nets that run in CI

| Test | Guards |
| --- | --- |
| `route_permissions_integration_test.go` | every protected route refuses a permissionless token |
| `tenancy_isolation_integration_test.go` | cross-tenant reads and writes answer 404 |
| `platform_wiring_test.go` | every shared service is wired (no surprise 503s) |
| `quota_concurrency_integration_test.go` | plan limits hold — and nothing deadlocks — under concurrent creates |
| `cachetest` contract suite | every cache driver behaves the same |
| `openapi-check` | the spec matches the router |
| `gen-check` | sqlc, OpenAPI and docs output is committed and current |

## Before you push

```bash
make check                # gofmt + vet + golangci-lint + unit tests
make test-integration     # with DATABASE_TEST_DSN
make gen-check            # generated code is current
make openapi-check        # contract matches the router
```
