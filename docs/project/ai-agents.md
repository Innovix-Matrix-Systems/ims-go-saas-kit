---
title: Working with AI agents
description: AGENTS.md and the task skills that let Claude Code, Codex and other agents work on this codebase the way the team does.
---

The repository is set up so an AI coding agent can work on it with the same conventions as a
person — and so a person can read exactly what the agent was told.

## AGENTS.md

[`AGENTS.md`](../../AGENTS.md) at the root is the canonical working guide: commands, module
anatomy, layering rules, cross-cutting patterns, the database workflow, testing and the design
decisions. `CLAUDE.md` points Claude Code at it; other agents read `AGENTS.md` directly.

## Skills

Task recipes live in `.agents/skills/` (canonical) and are mirrored to `.claude/skills/` for Claude
Code:

| Skill | Use it for |
| --- | --- |
| `add-module` | a new module or endpoint: generator, migration, queries, service, handler, routes, permissions, wiring, OpenAPI, tests |
| `db-change` | migrations, sqlc queries, regenerating `internal/dbgen`, nullable columns, sequences |
| `dev-dashboard` | dashboard endpoints, panels, commands, palette entries, form controls |
| `verify-changes` | the gates to run before calling a change done, including a live check |

Edit the `.agents` copy, then sync:

```bash
make skills-sync     # copies .agents/skills → .claude/skills
make skills-check    # fails on drift (part of make gen-check and CI)
```

Both run `go run ./cmd/skills`, so they work in PowerShell as well as a Unix shell.

## Keeping agents honest

The same safety nets that protect human changes protect agent changes:

- route permissions, tenant isolation and wiring tests,
- `make gen-check` for generated code and docs,
- `make openapi-check` for the contract,
- `golangci-lint` and the race detector in CI.

When an agent learns something non-obvious about the codebase, the place to write it down is
`AGENTS.md`, a skill, or [Design decisions](../architecture/decisions.md) — not a chat history.
