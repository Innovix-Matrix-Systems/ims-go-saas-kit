---
title: Contributing
description: Branches, commits, the pull-request checklist and how to write documentation for this project.
---

## Workflow

1. Branch from `main` (`feat/…`, `fix/…`, `docs/…`).
2. Make the change **with its tests and docs** in the same branch.
3. Run the checks below; open a pull request. CI runs them all again on Linux, including the
   race detector and the integration suite against Postgres.

Commits say **what** changed and **why** — one commit per feature or fix, imperative mood:

```text
fix(quota): read plan limits before the per-tenant lock

User and branch creation counted and inserted under a per-tenant advisory lock, but the
plan-limit lookup inside the lock used a second pool connection …
```

## Pull-request checklist

- [ ] New or changed SQL → `make sqlc` ran and `internal/dbgen` is committed.
- [ ] New permission → in `internal/authz/catalog.go`; `ims permission:sync` ran.
- [ ] Tenant-scoped reads and writes filter on the explicit tenant id — with an isolation test.
- [ ] Related data is batch-loaded (no query per row in a list).
- [ ] Handlers are thin; business rules live in services; no `httpx`/`authn` below the handler.
- [ ] Endpoint documented in `api/openapi.yaml`; `make openapi` and `make openapi-check` ran.
- [ ] `make gen-check` is green (sqlc, OpenAPI, generated docs, skill copies).
- [ ] `make check` is green; `make test-integration` too when you have a database.
- [ ] Dashboard touched → `make web-typecheck` and `make web-build`.
- [ ] Docs updated: the guide for the feature, and `docs/architecture/decisions.md` for a
      non-obvious decision.
- [ ] No secrets in logs or payloads.

```bash
make check               # gofmt + vet + golangci-lint + unit tests
make test-integration    # DATABASE_TEST_DSN=postgres://…/ims_go_api_test
make gen-check           # generated code and docs are current
make openapi-check       # the contract matches the router
```

## Writing documentation

The documentation is the Markdown in `docs/`, read on GitHub.

- **Add a page**: create `docs/<section>/<page>.md` with a `title` and `description` in its front
  matter, and link it from [`docs/README.md`](../README.md).
- **Link between pages** with relative file links — `[Tenancy](../guides/tenancy.md)`.
- **Screenshots** go in `docs/images/` and are referenced relatively (`../images/x.png`).
- **Never edit** `docs/reference/{cli,configuration,permissions,make}.md` — run `make docs-gen`.

Write for someone who just cloned the repository: say what a thing is for before how it works,
show real commands and real code from this codebase, and prefer a table to a paragraph of options.

## Getting help

| You want to… | Do this |
| --- | --- |
| report a bug or suggest a change | [open an issue](https://github.com/Innovix-Matrix-Systems/ims-go-admin-starter/issues/new) on GitHub, with steps to reproduce |
| ask how to do something with the starter | [support@innovixmatrixsystem.com](mailto:support@innovixmatrixsystem.com) |
| report a security problem | e-mail [support@innovixmatrixsystem.com](mailto:support@innovixmatrixsystem.com) privately; please don't open a public issue |
| have the team build or extend your product on it | [hello@innovixmatrixsystem.com](mailto:hello@innovixmatrixsystem.com) |
| anything else | [info@innovixmatrixsystem.com](mailto:info@innovixmatrixsystem.com) |
