# CLAUDE.md

See [`AGENTS.md`](AGENTS.md) — it is the canonical working guide for this repository (commands,
module anatomy, layering rules, cross-cutting patterns, database workflow, testing and design
decisions).

Task recipes are skills in `.claude/skills/` (`add-module`, `add-cli-command`, `db-change`,
`dev-dashboard`, `verify-changes`). They are copies of `.agents/skills/`: edit the `.agents` version, then run
`make skills-sync` (`make gen-check` and CI fail when the copies differ).
