---
title: CLI commands
description: How the ims command tree is built and registered, and how to add your own command — the constructor, the registrar, booting the app with withPlatform, flags, output, errors, tests, the generated reference, and the same task in the dashboard and the scheduler.
---

The binary is the CLI: `ims serve` starts the API, and every other `ims <command>` — `migrate`,
`cache:clear`, `down` — runs in the same binary, like Laravel's `artisan`. The
[CLI reference](../reference/cli.md) lists every command that ships with the starter. This page
explains how commands are registered and how to add your own.

```bash
ims activity:prune --days=90        # a built binary
go run ./cmd/app activity:prune     # from source
```

## How the command tree is built

Registration is explicit and happens in one place. Nothing registers itself (no `init()`), so
the full list of commands is visible in the code.

```text
cmd/app/main.go         main() → cli.Execute()
internal/cli/root.go    BuildRoot(state)
                          ├─ the root `ims` command + global flags (--env-file, --verbose)
                          ├─ PersistentPreRunE: load config + logger into State
                          ├─ registrars: registerBuiltins, registerDatabaseCommands, …   ← the one list
                          │     each one calls registry.Add(newXCommand(state)) for its commands
                          └─ root.AddCommand(every command the Registry collected)
```

| Piece | File | Job |
| --- | --- | --- |
| `BuildRoot` | `internal/cli/root.go` | builds the tree; holds the **registrars** list |
| `Registry` | `internal/cli/registry.go` | collects commands; a missing or duplicate name stops the binary at startup |
| `registerXCommands(registry, state)` | one file per area (`cache.go`, `backup.go`, …) | adds that area's commands |
| `newXCommand(state)` | the same file | builds one `*cobra.Command` |
| `State` | `internal/cli/root.go` | the loaded config and logger, shared by every command |
| `withPlatform(state, fn)` | `internal/cli/platform.go` | boots the app (database, cache, queue, services), runs `fn`, cleans up |

`ims help` lists commands alphabetically (Cobra sorts them), so the order of the registrars list
does not matter.

## Add a command

The example is a real command in the repository: `ims activity:prune` deletes old activity-log
entries (`internal/cli/activity.go`).

### 1. Put the work in a service

A command is an entry point, like a handler: it reads flags, calls a service and prints the
result. The rules live in the service, so the CLI, the dashboard and the scheduler can all call
it. Here that is `activity.Service.Purge`, which the `activity.prune` scheduled task already
uses. CLI commands are trusted callers, like jobs and scheduled tasks: they do not check
permissions.

### 2. Write the command

```go title="internal/cli/activity.go"
func newActivityPruneCommand(state *State) *cobra.Command {
	var days int

	cmd := &cobra.Command{
		Use:     "activity:prune",
		Short:   "Delete activity-log entries older than a number of days",
		Long:    "Delete activity-log entries older than --days (default ACTIVITY_RETENTION_DAYS, at least 30).",
		Example: "  ims activity:prune --days=90",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withPlatform(state, func(platform *app.Platform) error {
				if days == 0 {
					days = max(platform.Config.Activity.RetentionDays, 30)
				}
				if days < 1 {
					return fmt.Errorf("--days must be at least 1")
				}
				deleted, err := activity.NewService(platform.Tx.Queries()).Purge(cmd.Context(), days)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Deleted %d activity entr(ies) older than %d day(s).\n", deleted, days)
				return nil
			})
		},
	}
	cmd.Flags().IntVar(&days, "days", 0, "delete entries older than this many days (default: ACTIVITY_RETENTION_DAYS, at least 30)")
	return cmd
}
```

- **`withPlatform`** gives you the `*app.Platform`: `platform.Config`, `platform.Tx`, `platform.Cache`,
  `platform.Queue` and every shared service (`platform.Identity`, `platform.Settings`,
  `platform.Backup`, `platform.Maintenance`, …). It validates the config first and closes
  everything afterwards. A service your module builds only for HTTP must be added to `Platform`
  (`internal/app/platform.go`) before a command can use it.
- **Flags** are local variables bound with `cmd.Flags().IntVar(...)` / `StringVar` / `BoolVar`.
- **Use `RunE`, never `Run`.** Return errors; don't print them. `ims` prints `Error: …` and exits
  with code 1. An `*apperr.Error` from a service (a validation error, say) is printed the same way.
- **Write to `cmd.OutOrStdout()`**, not `fmt.Println`, so tests can capture the output. Use
  `text/tabwriter` for tables (see `backup:list`).
- **Pass `cmd.Context()`** to services.

### 3. Register it

Add it to the area's registrar. A new area gets its own registrar in the same file:

```go title="internal/cli/activity.go"
func registerActivityCommands(registry *Registry, state *State) error {
	return registry.Add(newActivityPruneCommand(state))
}
```

Several commands in one area: loop over them (`internal/cli/maintenance.go`):

```go
for _, cmd := range []*cobra.Command{newDownCommand(state), newUpCommand(state)} {
	if err := registry.Add(cmd); err != nil {
		return err
	}
}
```

A **new registrar** goes into the list in `BuildRoot`:

```go title="internal/cli/root.go"
registrars := []func(*Registry, *State) error{
	registerBuiltins,
	// …
	registerActivityCommands,
	// …
}
```

That is the only wiring. If the name is taken, the binary refuses to start with
`cli: duplicate command "activity:prune"`.

### 4. Run it

```bash
go run ./cmd/app activity:prune --help
go run ./cmd/app activity:prune --days=90
```

### 5. Test it

CLI commands get a smoke test that runs the real command tree through `BuildRoot`, exactly as
`ims` would. The package's `run(t, args...)` helper (`internal/cli/oauth_test.go`) returns the
output and the error:

```go title="internal/cli/activity_test.go"
func TestActivityPruneCommand(t *testing.T) {
	dsn := dbtest.DSN(t) // skips without DATABASE_TEST_DSN; migrated per package
	t.Setenv("DATABASE_DSN", dsn)
	t.Setenv("APP_ENV_FILE", "")

	out, err := run(t, "activity:prune", "--days", "90")
	if err != nil {
		t.Fatalf("activity:prune: %v\n%s", err, out)
	}
	if !strings.Contains(out, "older than 90 day(s)") {
		t.Errorf("output = %q, want the retention in days", out)
	}
}
```

A command that does not boot the platform needs no database (`TestVersionCommand` in
`root_test.go`). Test the rules in the service's own unit tests, not through the CLI.

### 6. Regenerate the reference

```bash
make docs-gen
```

The [CLI reference](../reference/cli.md) is generated from the same command tree, using `Use`,
`Short`, `Long`, `Example` and the flags. `make gen-check` (and CI) fail until you regenerate it.

## Conventions

| | Do | Example |
| --- | --- | --- |
| Name | `area:verb`, lower-case, hyphenated words | `activity:prune`, `billing:expire-subscriptions`, `oauth:client:create` |
| `Short` | one sentence, starts with a verb, no full stop | `Clear the configured cache store` |
| `Long` / `Example` | when flags or behaviour need explaining; `Example` lines start with two spaces | `  ims down --retry=120` |
| Flags | kebab-case; give a default and say it in the help | `--days`, `--dry-run`, `--with-secret` |
| Destructive work | no interactive prompts (commands run in scripts and CI); offer `--dry-run`, and refuse production without `--force` when it is dangerous there | `backup:clean --dry-run`, `db:seed --force` |
| Output | short, human-readable sentences or a `tabwriter` table on `cmd.OutOrStdout()` | `Deleted 12 activity entr(ies)…` |
| Errors | returned from `RunE` | `return fmt.Errorf("--days must be at least 1")` |

## Other shapes

**No platform.** A command that needs only the config uses `state.Config()` and skips
`withPlatform` (`version`, `config:show`). It starts faster and needs no database.

**Long-running.** `serve`, `queue:work` and `schedule` run until Ctrl+C or SIGTERM. Derive the
context from the signal, as `internal/cli/serve.go` does:

```go
ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
defer stop()
```

**Subcommands.** `ims gen module` is a parent command with children. Register the parent; it adds
its children with Cobra's own `AddCommand` (`internal/cli/gen.go`):

```go
gen := &cobra.Command{Use: "gen", Short: "Scaffold code from declarative specs"}
gen.AddCommand(newGenModuleCommand())
gen.AddCommand(newGenOpenAPICommand())
return registry.Add(gen)
```

The registry checks only top-level names, so keep subcommand names unique yourself. Prefer the flat
`area:verb` style for new commands.

## The same task in the dashboard and the scheduler

A command that maintainers run often can also run from the developer dashboard's **Commands**
panel, and recurring work can be scheduled. Both are separate lists that call **the same service
method**:

| Where | Add | Guide |
| --- | --- | --- |
| `ims activity:prune` | `newActivityPruneCommand` in `internal/cli` | this page |
| Dashboard → Commands | an entry in `devCommands`, `internal/app/dev_commands.go` | [Extending the dashboard](../dashboard/extending.md) |
| Every night | a task in `internal/app/schedule.go` | [Queue and scheduler](queue-and-scheduler.md) |

Keep their names the same, so `ims activity:prune` and the dashboard's `activity:prune` are
obviously the same work.

## Checklist

1. The work is a service method (tested on its own).
2. `newXCommand(state)` with `RunE`, `withPlatform`, flags, `cmd.OutOrStdout()`.
3. Added in a registrar; a new registrar is in the `BuildRoot` list.
4. A smoke test through `run(t, …)`.
5. `make docs-gen`.
6. Optional: the dashboard entry and the scheduled task, calling the same method.
