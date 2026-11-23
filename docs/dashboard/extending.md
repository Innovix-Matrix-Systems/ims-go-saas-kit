---
title: Extending the dashboard
description: Add a maintenance command, a dashboard endpoint, a panel or a palette entry.
---

## Add a maintenance command

The Commands panel runs an **allowlist** — nothing else can run from the browser. Add an entry to
`internal/app/dev_commands.go`:

```go title="internal/app/dev_commands.go"
{
	Name: "products:reindex", Label: "Reindex products", Group: "Maintenance",
	Description: "Rebuilds the product search index.",
	Confirm:    true,             // changes data → the UI asks first
	Background: true,             // slow → returns at once; watch the run
	Timeout:    10 * time.Minute,
	Options: []dev.CommandOption{
		{Name: "tenant", Label: "Tenant id", Type: "number", Description: "Empty = every tenant."},
		{Name: "dry_run", Label: "Dry run", Type: "bool", Default: "true"},
	},
	Run: func(ctx context.Context, opts map[string]string, out io.Writer) error {
		count, err := p.Products.Reindex(ctx, opts["tenant"], opts["dry_run"] == "true")
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "reindexed %d products\n", count)
		return nil
	},
},
```

- Option types: `bool`, `number`, `text`, `select` (with `Choices`).
- Write human-readable output to `out`; it is stored with the run and shown in the panel.
- Every run is audited (`command.run`) with its options, status and duration.
- Keep the CLI and the dashboard doing the **same** work: call the same service method from the
  Cobra command, and give both the same name ([CLI commands](../guides/cli-commands.md#the-same-task-in-the-dashboard-and-the-scheduler)).

## Add a dashboard endpoint

1. A service method in `internal/modules/dev` (or call an existing module service).
2. A handler on `dev.Handler` — it embeds the same `httpx.Kit` as every module (`h.Bind`,
   `h.PathID`, `h.Data`).
3. The route in `internal/modules/dev/routes.go`; mutations add `r.With(h.manage)` and record an
   audit entry with `h.audit.Record(...)`.
4. Document it in `api/openapi.yaml`, run `make openapi` and `make openapi-check`.
5. Refresh the SPA's types: `npm --prefix web/dev run openapi:gen`.

## Add a panel

Every panel follows the same template, so a new one fits without a redesign:

1. A route file `web/dev/src/routes/_app.<name>.tsx` (TanStack Router file routes).
2. One entry in `web/dev/src/components/navigation.ts` with its group, icon and `g` shortcut
   letter. The sidebar, the command palette, the shortcuts sheet and the top bar all read that
   list.
3. Fetch with TanStack Query and the typed `api()` client (`src/api/client.ts`); export the
   response type from `src/api/types.ts`.
4. Build the page from the kit in `components/ui.tsx`: `PageHeader` (title, one-line description,
   actions, meta), then an optional filter row, then content in `Card`s; `StatTile`, `Badge`,
   `StatusDot`, `LiveDot` (running work only), `Drawer`, `Modal`, `Tabs`, `DetailStrip`; layout
   with `Stack`, `Cluster`, `Split` and `Grid`. Use **`Select`, `DatePicker`, `TimePicker`** from
   `components/inputs.tsx`, never the browser's native `<select>` or date input.
5. No inline `style={{…}}` for layout and no raw colours: styles use the tokens in
   `src/styles/tokens.css`, and a panel's own rules go in `src/styles/screens/<name>.css`.
   Inline styles are for data-driven values only (a bar's width, a virtual row's offset).
6. Deep-link state belongs in the URL (`validateSearch`), so links like `?tenant=7` work.
7. Check it at `/dev/kit` and on the page in both themes and at phone width.

```bash
make dev-web             # Vite with hot reload on :8080/dev
make web-typecheck       # tsc
make web-style-check     # contrast, type scale, no raw colours
make web-build           # what the release embeds
```

## Add a palette entry

Pages come from `navigation.ts` (above). `web/dev/src/components/CommandPalette.tsx` lists
Inspector tabs and actions; add yours to the matching array so **Ctrl+K** can reach it.
