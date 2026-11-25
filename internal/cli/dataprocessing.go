package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/app"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/modules/dataprocessing"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/tenancy"
)

func registerDataProcessingCommands(registry *Registry, state *State) error {
	return registry.Add(newDataProcessingCleanupCommand(state))
}

func newDataProcessingCleanupCommand(state *State) *cobra.Command {
	var days int
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "data-processing:cleanup-completed",
		Short: "Delete completed processing-job records older than the retention window",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withPlatform(state, func(platform *app.Platform) error {
				ctx := cmd.Context()
				retention := days
				if retention <= 0 {
					retention = dataprocessing.CleanupSetting(ctx, platform.Settings, tenancy.Scope{Internal: true})
				}

				if dryRun {
					count, err := platform.Processing.CountCompleted(ctx, retention)
					if err != nil {
						return err
					}
					fmt.Fprintf(cmd.OutOrStdout(), "%d job(s) would be deleted (retention %d day(s)).\n", count, retention)
					return nil
				}

				deleted, err := platform.Processing.CleanupCompleted(ctx, retention)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Deleted %d job(s) (retention %d day(s)).\n", deleted, retention)
				return nil
			})
		},
	}
	cmd.Flags().IntVar(&days, "days", 0, "retention window in days (default: data_processing_job.cleanup_days setting)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report how many jobs would be deleted")
	return cmd
}
