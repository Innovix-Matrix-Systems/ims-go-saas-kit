---
name: verify-changes
description: Use when finishing any coding task in this repo — run format, vet, lint, unit and integration tests (DATABASE_TEST_DSN), generated-code and OpenAPI checks, the dashboard typecheck/build, and a live check against the running app before reporting done.
---

# Verifying changes

Run every gate that applies; report exact results. Do not claim success without output.

## Gates (in order)

```bash
gofmt -l .                       # must print nothing (or: make format-check)
go vet ./...
golangci-lint run ./...          # exit 0

# Integration tests (real Postgres; each package gets its own schema)
$env:DATABASE_TEST_DSN = "postgres://postgres:postgres@127.0.0.1:5432/ims_go_api_test?sslmode=disable"
go test -count=1 ./...
go test -count=1 -race -p 2 ./...   # -p 2 locally: race builds are memory-hungry

make gen-check                   # sqlc, oapi-codegen, OpenAPI regions and skills are current
make openapi-check               # spec valid + "no route drift"
go run ./cmd/app i18n:coverage   # every language has every API key, plural form and template variant
```

Client app (`web/app`) when its screens, catalogs or styles changed:

```bash
cd web/app
npm run i18n:types               # after adding or changing English keys
npm run i18n:check               # every language complete, types current, no language codes in code
npm run check:style              # tokens: WCAG AA contrast, type scale, no raw colours
npm run check:logical            # no left/right CSS properties (right-to-left languages)
npx tsc --noEmit
npm run build
```

Dashboard (`web/dev`) when the SPA, `api/openapi.yaml` or `/dev/api` changed:

```bash
cd web/dev
npm run openapi:gen              # refresh src/api/schema.d.ts after spec changes
npm run check:style              # tokens: WCAG AA contrast, type scale, no raw colours
npx tsc --noEmit
npm run build                    # what the release embeds
```

Tests that skip without `DATABASE_TEST_DSN` are not "passing" — say so when no DSN is available.
`go test -v -run Name` confirms a new test actually ran.

## Live check (HTTP, CLI and dashboard changes)

`make dev` runs the SPA on `http://localhost:8080/dev` and the API on `:8081` (air). Apply new
migrations first (`go run ./cmd/app migrate`). Check new endpoints with curl or in the browser:

```bash
# login (seed password: AUTH_DEFAULT_PASSWORD, default 123456)
curl -s -X POST localhost:8081/api/v1/login -H "Content-Type: application/json" \
  -d '{"email":"superadmin@ims.com","password":"123456","device":"smoke"}'
# then -H "Authorization: Bearer <token>"; impersonation: -H "X-Acting-Tenant: 1" -H "X-Acting-Branch: 1"
```

A new route should appear in `go run ./cmd/app route:list --path=/api/v1/x` with its permission
before you curl it (in Git Bash set `MSYS_NO_PATHCONV=1`, or it rewrites `/api/...` arguments into
Windows paths). If it is listed but answers 404/405, the running server is stale (below).

**Air on Windows** sometimes leaves the old API running, so the rebuilt binary cannot bind :8081
("Only one usage of each socket address") and new routes answer 404/405. Check
`Get-Process ims | Select Id, StartTime`; if it predates your change, stop that process and make
a real content change to a Go file (air skips unchanged files), or restart `make dev`. Queue and
scheduler processes (`make queue`, `make schedule`) never hot-reload — restart them after changes
that affect jobs or tasks.

Without `make dev`: `go build -o bin/ims.exe ./cmd/app`, start `bin/ims.exe serve`
(listens on `APP_LISTEN_ADDR`, default `:8080`), test, stop it. PowerShell `curl.exe` mangles
inline JSON quotes — pass bodies with `--data-binary "@file.json"`.

## Also check

- New tables/columns: `migrate:status` shows every goose migration applied and
  `River schema: vN (0 pending)`.
- Seeding: `db:seed` twice reports no duplicates.
- New routes are protected (`route_permissions_integration_test.go`) and documented
  (`openapi-check`).
- Docs: behavior or workflow changes are reflected in `docs/` (the guide for the feature,
  `docs/architecture/decisions.md` for a decision), `AGENTS.md` and the skills (then
  `make skills-sync`). New CLI command, env var, permission or make target → `make docs-gen`.
- User-visible change → a line under `## [Unreleased]` in `CHANGELOG.md`.
- Commit only when the user asks: one commit per feature, message says what and why.
