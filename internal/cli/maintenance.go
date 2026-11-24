package cli

import (
	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/app"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/maintenance"
)

// Maintenance mode: Laravel's `artisan down` / `up`.
func registerMaintenanceCommands(registry *Registry, state *State) error {
	for _, cmd := range []*cobra.Command{
		newDownCommand(state),
		newUpCommand(state),
		newMaintenanceStatusCommand(state),
	} {
		if err := registry.Add(cmd); err != nil {
			return err
		}
	}
	return nil
}

func newDownCommand(state *State) *cobra.Command {
	var message, secret string
	var retry int
	var withSecret bool

	cmd := &cobra.Command{
		Use:   "down",
		Short: "Put the application into maintenance mode",
		Long: "Put the application into maintenance mode: every API process answers 503 MAINTENANCE_MODE\n" +
			"within MAINTENANCE_CHECK_INTERVAL. /up, /api/health, /metrics, /dev and /docs keep working;\n" +
			"a client sending the bypass secret in the " + maintenance.BypassHeader + " header passes.",
		Example: "  ims down --message=\"Upgrading the database\" --retry=120 --with-secret",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withPlatform(state, func(platform *app.Platform) error {
				down, generated, err := platform.Maintenance.Down(cmd.Context(), maintenance.DownInput{
					Message: message, RetryAfter: retry, Secret: secret, GenerateSecret: withSecret,
				})
				if err != nil {
					return err
				}
				app.WriteMaintenanceDown(cmd.OutOrStdout(), down, generated)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&message, "message", "", "message shown to API clients (default: \""+maintenance.DefaultMessage+"\")")
	cmd.Flags().IntVar(&retry, "retry", maintenance.DefaultRetryAfter, "Retry-After hint in seconds")
	cmd.Flags().StringVar(&secret, "secret", "", "bypass secret clients may send in the "+maintenance.BypassHeader+" header")
	cmd.Flags().BoolVar(&withSecret, "with-secret", false, "generate a bypass secret and print it once")
	return cmd
}

func newUpCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Bring the application out of maintenance mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withPlatform(state, func(platform *app.Platform) error {
				changed, err := platform.Maintenance.Up(cmd.Context(), 0)
				if err != nil {
					return err
				}
				app.WriteMaintenanceUp(cmd.OutOrStdout(), changed)
				return nil
			})
		},
	}
}

func newMaintenanceStatusCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "maintenance:status",
		Short: "Show whether the application is in maintenance mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withPlatform(state, func(platform *app.Platform) error {
				current, err := platform.Maintenance.Status(cmd.Context())
				if err != nil {
					return err
				}
				app.WriteMaintenanceStatus(cmd.OutOrStdout(), current)
				return nil
			})
		},
	}
}
