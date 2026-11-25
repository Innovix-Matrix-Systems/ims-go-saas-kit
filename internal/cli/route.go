package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/go-chi/chi/v5"
	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/app"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/dev"
)

func registerRouteCommands(registry *Registry, state *State) error {
	return registry.Add(newRouteListCommand(state))
}

// newRouteListCommand is Laravel's `route:list`: the live route table with
// each route's permission, built by the same walker as the dashboard's
// Inspector → Routes, so both always agree.
func newRouteListCommand(state *State) *cobra.Command {
	var method, path, exceptPath string
	var asJSON, withMiddleware bool

	cmd := &cobra.Command{
		Use:   "route:list",
		Short: "List every HTTP route with its permission and handler",
		Long: "List every HTTP route the API registers with this configuration: method, path, the\n" +
			"permission it requires (empty: none) and the handler. --middleware adds each route's own\n" +
			"middleware (after the global stack, printed at the end); --json prints the full table for\n" +
			"tools and AI agents. Builds the full dependency graph, like openapi:check.",
		Example: "  ims route:list --path=/api/v1/branch\n" +
			"  ims route:list --method=DELETE --middleware\n" +
			"  ims route:list --except-path=/dev --json",
		RunE: func(cmd *cobra.Command, args []string) error {
			// No cfg.Validate(): listing routes must work without a
			// deployment .env, like openapi:check.
			cfg := state.Config()
			deps, cleanup, err := app.NewDependencies(cfg, state.Logger())
			if err != nil {
				return err
			}
			defer cleanup()

			router, ok := deps.Router().(chi.Routes)
			if !ok {
				return fmt.Errorf("route:list: the router does not expose its routes")
			}
			report, err := dev.NewInspectorService(deps.Tx.Queries(), cfg, func() chi.Routes { return router }).Routes()
			if err != nil {
				return err
			}
			report.Routes = filterRoutes(report.Routes, method, path, exceptPath)

			out := cmd.OutOrStdout()
			if asJSON {
				encoder := json.NewEncoder(out)
				encoder.SetIndent("", "  ")
				return encoder.Encode(report)
			}

			writer := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
			header := "METHOD\tPATH\tPERMISSION\tHANDLER"
			if withMiddleware {
				header += "\tMIDDLEWARE"
			}
			fmt.Fprintln(writer, header)
			for _, route := range report.Routes {
				permission := strings.Join(route.Permissions, ",")
				if permission == "" {
					permission = "-"
				}
				line := fmt.Sprintf("%s\t%s\t%s\t%s", route.Method, route.Path, permission, route.Handler)
				if withMiddleware {
					line += "\t" + strings.Join(route.Middleware, " → ")
				}
				fmt.Fprintln(writer, line)
			}
			if err := writer.Flush(); err != nil {
				return err
			}
			fmt.Fprintf(out, "\n%d route(s).", len(report.Routes))
			if withMiddleware && len(report.Global) > 0 {
				fmt.Fprintf(out, " Every route first runs: %s.", strings.Join(report.Global, " → "))
			}
			fmt.Fprintln(out)
			return nil
		},
	}
	cmd.Flags().StringVar(&method, "method", "", "only routes with this HTTP method (e.g. GET)")
	cmd.Flags().StringVar(&path, "path", "", "only routes whose path contains this text")
	cmd.Flags().StringVar(&exceptPath, "except-path", "", "leave out routes whose path contains this text")
	cmd.Flags().BoolVar(&withMiddleware, "middleware", false, "add each route's middleware")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the table as JSON (method, path, handler, permissions, middleware)")
	return cmd
}

// filterRoutes applies route:list's --method, --path and --except-path.
func filterRoutes(routes []dev.RouteInfo, method, path, exceptPath string) []dev.RouteInfo {
	out := make([]dev.RouteInfo, 0, len(routes))
	for _, route := range routes {
		if method != "" && !strings.EqualFold(route.Method, method) && route.Method != "ANY" {
			continue
		}
		if path != "" && !strings.Contains(route.Path, path) {
			continue
		}
		if exceptPath != "" && strings.Contains(route.Path, exceptPath) {
			continue
		}
		out = append(out, route)
	}
	return out
}
