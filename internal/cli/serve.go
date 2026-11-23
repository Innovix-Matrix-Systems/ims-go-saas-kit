package cli

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/app"
)

func registerAppCommands(registry *Registry, state *State) error {
	return registry.Add(newServeCommand(state))
}

func newServeCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP API server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := state.Config()
			if err := cfg.Validate(); err != nil {
				return err
			}
			if err := cfg.ValidateDeploymentPosture(); err != nil {
				return err
			}
			deps, cleanup, err := app.NewDependencies(cfg, state.Logger())
			if err != nil {
				return err
			}
			defer cleanup()
			if current, err := deps.Platform.Maintenance.Status(cmd.Context()); err == nil && current.Down {
				state.Logger().Warn("the application is in maintenance mode: API requests answer 503 until `ims up`",
					"since", current.Since, "message", current.Message)
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return app.RunHTTP(ctx, cfg, state.Logger(), deps.Router())
		},
	}
}
