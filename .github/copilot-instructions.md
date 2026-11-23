# GitHub Copilot instructions

**Read [`AGENTS.md`](../AGENTS.md) first — it is the canonical working guide.** This file only
repeats the essentials.

- Feature-first Go monolith. Prefer the established pattern in this codebase; don't invent a new one.
- Layering: `routes.go` (registration only) → `handler.go` (HTTP) → `service.go` (rules) →
  `repository.go` (SQL via sqlc) → `internal/dbgen` (generated, never edit).
  Large modules (billing) split services into `service/` and HTTP into `api/`.
- Errors: services return `*apperr.Error`; handlers render RFC 9457 via `responder.Error`.
  Validation messages must stay human-readable.
- Tenancy: handlers read `tenancy.FromContext`; repositories take explicit tenant/branch ids;
  `0` means no filter.
- Auth: `authn.IdentityFromContext`; permissions via `authz.Checker.MustCan`; new permissions go
  in `internal/authz/catalog.go`.
- Schema changes: goose migration → `make sqlc` → commit generated code.
- Before finishing: `gofmt -l .` empty, `golangci-lint run ./...` clean, tests green
  (integration tests need `DATABASE_TEST_DSN`; use `-race -p 2` locally).
- Record deliberate design decisions in `docs/architecture/decisions.md` (and `AGENTS.md`).
