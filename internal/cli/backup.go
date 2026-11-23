package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/app"
	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/modules/backup"
)

func registerBackupCommands(registry *Registry, state *State) error {
	commands := []*cobra.Command{
		newBackupRunCommand(state),
		newBackupListCommand(state),
		newBackupCleanCommand(state),
		newBackupMonitorCommand(state),
	}
	for _, cmd := range commands {
		if err := registry.Add(cmd); err != nil {
			return err
		}
	}
	return nil
}

func newBackupRunCommand(state *State) *cobra.Command {
	var withFiles bool
	var destinations string
	var filename string
	cmd := &cobra.Command{
		Use:   "backup:run",
		Short: "Create a database backup archive",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withPlatform(state, func(platform *app.Platform) error {
				opts := backup.RunOptions{Filename: filename}
				if cmd.Flags().Changed("with-files") {
					opts.IncludeFiles = &withFiles
				}
				if destinations != "" {
					for _, value := range strings.Split(destinations, ",") {
						opts.Destinations = append(opts.Destinations, strings.TrimSpace(value))
					}
				}
				run, err := platform.Backup.RunNow(cmd.Context(), backup.TriggerCLI, 0, opts)
				if run != nil {
					printBackupRun(cmd, run)
				}
				return err
			})
		},
	}
	cmd.Flags().BoolVar(&withFiles, "with-files", false, "include BACKUP_FILES_PATHS in the archive")
	cmd.Flags().StringVar(&destinations, "destinations", "", "comma-separated destinations (local is always included)")
	cmd.Flags().StringVar(&filename, "filename", "", "archive filename override")
	return cmd
}

func newBackupListCommand(state *State) *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "backup:list",
		Short: "List recent backup runs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withPlatform(state, func(platform *app.Platform) error {
				runs, err := platform.Backup.List(cmd.Context(), backup.ListFilter{Limit: int32(limit)})
				if err != nil {
					return err
				}
				writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
				fmt.Fprintln(writer, "ID\tCREATED\tSTATUS\tTRIGGER\tFILENAME\tSIZE\tDESTINATIONS")
				if len(runs) == 0 {
					fmt.Fprintln(writer, "-\t-\t-\t-\t(no backups)\t0\t-")
				}
				for _, run := range runs {
					destinations := make([]string, 0, len(run.Destinations))
					for _, destination := range run.Destinations {
						value := destination.Destination + ":" + destination.Status
						destinations = append(destinations, value)
					}
					fmt.Fprintf(writer, "%d\t%s\t%s\t%s\t%s\t%d\t%s\n",
						run.ID, run.CreatedAt.Format("2006-01-02 15:04:05"), run.Status,
						run.Trigger, run.Filename, run.SizeBytes, strings.Join(destinations, ","))
				}
				return writer.Flush()
			})
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "maximum runs to list")
	return cmd
}

func newBackupCleanCommand(state *State) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "backup:clean",
		Short: "Apply the retention policy to stored backups",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withPlatform(state, func(platform *app.Platform) error {
				result, err := platform.Backup.Clean(cmd.Context(), dryRun)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "candidates=%d deleted=%d failed=%d dry_run=%t ids=%v\n",
					result.Candidates, result.Deleted, result.Failed, result.DryRun, result.IDs)
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be deleted without deleting")
	return cmd
}

func newBackupMonitorCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "backup:monitor",
		Short: "Check backup freshness and storage usage",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withPlatform(state, func(platform *app.Platform) error {
				result, err := platform.Backup.Monitor(cmd.Context())
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "status=%s age_hours=%.1f total_size=%d successful_runs=%d\n%s\n",
					result.Status, result.AgeHours, result.TotalSize, result.SuccessfulRuns, result.Summary)
				if result.Status != "ok" {
					return fmt.Errorf("backup monitor status is %s", result.Status)
				}
				return nil
			})
		},
	}
}

func printBackupRun(cmd *cobra.Command, run *backup.Run) {
	fmt.Fprintf(cmd.OutOrStdout(), "run #%d %s status=%s size=%d checksum=%s\n",
		run.ID, run.Filename, run.Status, run.SizeBytes, run.ChecksumSHA256)
	for _, destination := range run.Destinations {
		location := destination.Path
		if destination.Key != "" {
			location = destination.Key
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  %s: %s %s %s\n",
			destination.Destination, destination.Status, location, destination.Error)
	}
	if run.Error != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "error: %s\n", run.Error)
	}
}
