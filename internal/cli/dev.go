package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/app"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/dev"
)

func registerDevCommands(registry *Registry, state *State) error {
	commands := []*cobra.Command{
		newDevPruneCommand(state),
	}
	for _, cmd := range commands {
		if err := registry.Add(cmd); err != nil {
			return err
		}
	}
	return nil
}

func newDevPruneCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "dev-dashboard:prune",
		Short: "Delete developer dashboard telemetry past its retention window",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withPlatform(state, func(platform *app.Platform) error {
				cfg := platform.Config
				repo := dev.NewTelemetryRepository(platform.Tx.Queries())
				deleted, err := repo.Prune(cmd.Context(), cfg.DevDashboard.RetentionDays)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(),
					"Pruned telemetry past %d days: requests=%d queries=%d cache=%d queue=%d logs=%d exceptions=%d\n",
					cfg.DevDashboard.RetentionDays,
					deleted["requests"], deleted["queries"], deleted["cache_events"],
					deleted["queue_events"], deleted["logs"], deleted["exceptions"])
				audit, err := dev.NewActionRepository(platform.Tx.Queries()).Prune(cmd.Context(), cfg.DevDashboard.AuditRetentionDays)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Pruned %d audit row(s) past %d days.\n", audit, cfg.DevDashboard.AuditRetentionDays)
				return nil
			})
		},
	}
}
