package cli

import (
	"context"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
)

func registerDatabaseCommands(registry *Registry, state *State) error {
	commands := []*cobra.Command{
		newMigrateCommand(state),
		newMigrateStatusCommand(state),
		newMigrateRollbackCommand(state),
	}
	for _, cmd := range commands {
		if err := registry.Add(cmd); err != nil {
			return err
		}
	}
	return nil
}

func openDatabase(ctx context.Context, state *State) (*database.Registry, func(), error) {
	cfg := state.Config()
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}
	registry, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return nil, nil, err
	}
	return registry, func() { registry.Close() }, nil
}

func newMigrateCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply all pending database migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			registry, cleanup, err := openDatabase(ctx, state)
			if err != nil {
				return err
			}
			defer cleanup()

			applied, err := database.MigrateUp(ctx, registry.Primary().Pool)
			if err != nil {
				return err
			}
			if applied == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Nothing to migrate.")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Applied %d migration(s).\n", applied)
			return nil
		},
	}
}

func newMigrateStatusCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "migrate:status",
		Short: "Show migration status",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			registry, cleanup, err := openDatabase(ctx, state)
			if err != nil {
				return err
			}
			defer cleanup()

			statuses, err := database.MigrationStatuses(ctx, registry.Primary().Pool)
			if err != nil {
				return err
			}

			writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(writer, "STATUS\tVERSION\tNAME")
			for _, status := range statuses {
				state := "pending"
				if status.Applied {
					state = "applied"
				}
				fmt.Fprintf(writer, "%s\t%d\t%s\n", state, status.Version, status.Name)
			}
			if err := writer.Flush(); err != nil {
				return err
			}

			pool := registry.Primary().Pool
			riverVersion, riverExists, err := database.RiverSchemaVersion(ctx, pool)
			if err != nil {
				return err
			}
			if !riverExists {
				fmt.Fprintln(cmd.OutOrStdout(), "River schema: not migrated")
				return nil
			}
			riverPending, err := database.RiverPendingCount(ctx, pool)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "River schema: v%d (%d pending migration(s))\n", riverVersion, riverPending)
			return nil
		},
	}
}

func newMigrateRollbackCommand(state *State) *cobra.Command {
	var steps int

	cmd := &cobra.Command{
		Use:   "migrate:rollback",
		Short: "Roll back the most recent migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			registry, cleanup, err := openDatabase(ctx, state)
			if err != nil {
				return err
			}
			defer cleanup()

			reverted, err := database.MigrateDown(ctx, registry.Primary().Pool, steps)
			if err != nil {
				return err
			}
			if reverted == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Nothing to roll back.")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Reverted %d migration(s).\n", reverted)
			return nil
		},
	}
	cmd.Flags().IntVar(&steps, "steps", 1, "number of migrations to roll back")
	return cmd
}
