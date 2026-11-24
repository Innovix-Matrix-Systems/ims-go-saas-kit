package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/app"
)

func newOpenAPICheckCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "openapi:check",
		Short: "Validate the OpenAPI document and check for route drift",
		Long: "Validate the embedded OpenAPI document (api/openapi.yaml) and compare it\n" +
			"against the live router. Requires database connectivity (the route table is\n" +
			"built from the full dependency graph), like health:check.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Force the config-gated operational routes on so the route table
			// matches the full contract regardless of the runtime flags. No
			// cfg.Validate(): CI runs this without a deployment .env (M46).
			cfg := state.Config()
			cfg.Ops.DocsEnabled = true
			cfg.Ops.MetricsEnabled = true

			deps, cleanup, err := app.NewDependencies(cfg, state.Logger())
			if err != nil {
				return err
			}
			defer cleanup()

			doc, mismatches, err := app.CheckEmbeddedContract(deps.Router())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "spec valid: %d paths\n", len(doc.Paths.Map()))
			if len(mismatches) > 0 {
				for _, m := range mismatches {
					fmt.Fprintf(out, "drift: %s %s (present only in %s)\n", m.Method, m.Path, m.In)
				}
				return fmt.Errorf("%d contract mismatch(es)", len(mismatches))
			}
			fmt.Fprintln(out, "no route drift")

			root, err := filepath.Abs(".")
			if err != nil {
				return err
			}
			return checkOpenAPIRegions(cmd, root)
		},
	}
}
