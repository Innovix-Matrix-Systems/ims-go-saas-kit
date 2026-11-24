package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/app"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
)

func registerScheduleCommands(registry *Registry, state *State) error {
	commands := []*cobra.Command{
		newScheduleCommand(state),
		newScheduleListCommand(state),
		newScheduleRunCommand(state),
	}
	for _, cmd := range commands {
		if err := registry.Add(cmd); err != nil {
			return err
		}
	}
	return nil
}

func newScheduleCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "schedule",
		Short: "Run the scheduler daemon (one process per deployment)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withPlatform(state, func(platform *app.Platform) error {
				ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
				defer stop()
				// One scheduler per database: a second process would run every
				// task twice (renewal reminders, expiry, backups).
				release, ok, err := platform.Tx.TryHoldLock(ctx, database.SchedulerLockID)
				if err != nil {
					return err
				}
				if !ok {
					return errors.New("another scheduler is already running against this database; only one `ims schedule` process is allowed")
				}
				defer release()
				return platform.ScheduleRunner.Start(ctx)
			})
		},
	}
}

func newScheduleListCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "schedule:list",
		Short: "List registered scheduled tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			// No cfg.Validate(): listing the task registry is read-only and
			// should work on a half-configured install (M46).
			cfg := state.Config()
			platform, cleanup, err := app.NewPlatform(cfg, state.Logger())
			if err != nil {
				return err
			}
			defer cleanup()

			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(writer, "ID\tSPEC\tDESCRIPTION")
			for _, task := range platform.ScheduleRegistry.Tasks() {
				fmt.Fprintf(writer, "%s\t%s\t%s\n", task.ID, task.Spec, task.Description)
			}
			return writer.Flush()
		},
	}
}

func newScheduleRunCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "schedule:run <task-id>",
		Short: "Run a scheduled task once, immediately",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withPlatform(state, func(platform *app.Platform) error {
				ctx, cancel := context.WithCancel(cmd.Context())
				defer cancel()
				if err := platform.ScheduleRunner.RunNow(ctx, args[0]); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Task %q finished.\n", args[0])
				return nil
			})
		},
	}
}
