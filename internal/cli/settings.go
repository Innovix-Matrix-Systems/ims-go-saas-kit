package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/app"
)

func registerSettingsCommands(registry *Registry, state *State) error {
	commands := []*cobra.Command{
		newSettingsSyncCommand(state),
		newFeatureFlagsSyncCommand(state),
	}
	for _, cmd := range commands {
		if err := registry.Add(cmd); err != nil {
			return err
		}
	}
	return nil
}

func newSettingsSyncCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "settings:sync",
		Short: "Upsert settings groups and insert missing global settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withPlatform(state, func(platform *app.Platform) error {
				created, err := platform.Settings.Sync(cmd.Context())
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Created %d setting(s).\n", created)
				return nil
			})
		},
	}
}

func newFeatureFlagsSyncCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "feature-flags:sync",
		Short: "Insert missing global feature flags with their catalog defaults",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withPlatform(state, func(platform *app.Platform) error {
				created, err := platform.FeatureFlags.Sync(cmd.Context())
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Created %d feature flag(s).\n", created)
				return nil
			})
		},
	}
}
