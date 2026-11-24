package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/app"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/activity"
)

func registerActivityCommands(registry *Registry, state *State) error {
	return registry.Add(newActivityPruneCommand(state))
}

// newActivityPruneCommand is the CLI twin of the dashboard's activity:prune
// command and the activity.prune scheduled task: all three call
// activity.Service.Purge.
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
