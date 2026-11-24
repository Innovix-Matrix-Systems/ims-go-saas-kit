package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/app"
	cachefactory "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/cache/factory"
)

func registerCacheCommands(registry *Registry, state *State) error {
	return registry.Add(newCacheClearCommand(state))
}

func newCacheClearCommand(state *State) *cobra.Command {
	var driver string

	cmd := &cobra.Command{
		Use:   "cache:clear",
		Short: "Clear the configured cache store",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withPlatform(state, func(platform *app.Platform) error {
				cfg := platform.Config
				store := platform.Cache
				cleanStore := func() {}
				if driver != "" && driver != cfg.Cache.Driver {
					override := *cfg
					override.Cache.Driver = driver
					var err error
					store, cleanStore, err = cachefactory.New(override, platform.Tx.Queries())
					if err != nil {
						return err
					}
				}
				defer cleanStore()

				if err := store.Clear(cmd.Context()); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Cache store %q cleared.\n", cfg.Cache.Driver)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&driver, "store", "", "override the cache driver (memory|file|redis|database)")
	return cmd
}
