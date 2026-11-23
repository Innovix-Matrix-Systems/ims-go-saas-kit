---
name: add-cli-command
description: Use when adding or changing an `ims` CLI command in this repo (internal/cli) — the newXCommand constructor, the registerXCommands registrar and the BuildRoot list in root.go, withPlatform, flags, output and errors, the BuildRoot smoke test, make docs-gen, and the matching dashboard command or scheduled task.
---

# Adding a CLI command

Human guide: `docs/guides/cli-commands.md`. Reference implementations: `internal/cli/activity.go`
(one command), `internal/cli/maintenance.go` (several, shared output), `internal/cli/backup.go`
(tables), `internal/cli/gen.go` (subcommands).

## Order of work

1. **Service first.** The rules live in a service method (callable from HTTP, CLI, jobs and the
   scheduler). The command only reads flags, calls it and prints. CLI callers are trusted: no
   permission checks.
2. **Constructor** `newXCommand(state *State) *cobra.Command` in `internal/cli/<area>.go`:
   - `Use: "area:verb"`, `Short` (a verb, no full stop), `Long`/`Example` when useful.
   - `RunE` (never `Run`); `return withPlatform(state, func(platform *app.Platform) error {...})`
     when it needs the database or services; `state.Config()` alone when it doesn't.
   - Flags bound to local variables (`cmd.Flags().IntVar(&days, "days", 0, "...")`), kebab-case.
   - Output to `cmd.OutOrStdout()` (`text/tabwriter` for tables); return errors, never print them.
   - Pass `cmd.Context()`. No interactive prompts: `--dry-run`, and `--force` for production danger.
   - A service the command needs must be on `app.Platform` (`internal/app/platform.go`).
3. **Register:** add it to the area's `registerXCommands(registry, state)`; a new registrar goes
   into the `registrars` list in `BuildRoot` (`internal/cli/root.go`). Duplicate names fail at
   startup. Subcommands: register the parent, `parent.AddCommand(child)` (not checked for
   duplicates).
4. **Smoke test** in `internal/cli/<area>_test.go` through the real tree:
   `out, err := run(t, "area:verb", "--flag", "x")`; for platform commands start with
   `t.Setenv("DATABASE_DSN", dbtest.DSN(t))` and `t.Setenv("APP_ENV_FILE", "")`.
5. **Docs:** `make docs-gen` regenerates `docs/reference/cli.md` (`make gen-check` fails otherwise).
6. **Twins (optional):** a dashboard command (`devCommands` in `internal/app/dev_commands.go`,
   see the `dev-dashboard` skill) and/or a scheduled task (`internal/app/schedule.go`) call the
   **same** service method and use the **same** name.

Finish with the `verify-changes` skill.
