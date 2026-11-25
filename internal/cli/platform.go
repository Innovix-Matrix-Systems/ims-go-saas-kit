package cli

import (
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/app"
)

// withPlatform validates the configuration, boots the application (database,
// cache, queue and services), runs fn and shuts everything down again — what
// Laravel does before every artisan command:
//
//	RunE: func(cmd *cobra.Command, args []string) error {
//		return withPlatform(state, func(platform *app.Platform) error {
//			_, err := platform.Settings.Sync(cmd.Context())
//			return err
//		})
//	},
func withPlatform(state *State, fn func(platform *app.Platform) error) error {
	cfg := state.Config()
	if err := cfg.Validate(); err != nil {
		return err
	}
	platform, cleanup, err := app.NewPlatform(cfg, state.Logger())
	if err != nil {
		return err
	}
	defer cleanup()
	return fn(platform)
}
