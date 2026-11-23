package cli

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/authz"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
	dbgen "github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/dbgen"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/access"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/featureflags"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/settings"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/cache/memory"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/seeder"
)

func registerSeedCommands(registry *Registry, state *State) error {
	commands := []*cobra.Command{
		newPermissionSyncCommand(state),
		newDBSeedCommand(state),
	}
	for _, cmd := range commands {
		if err := registry.Add(cmd); err != nil {
			return err
		}
	}
	return nil
}

func newPermissionSyncCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "permission:sync",
		Short: "Sync the permission catalog into the database",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			registry, cleanup, err := openDatabase(ctx, state)
			if err != nil {
				return err
			}
			defer cleanup()

			queries := dbgen.New(registry.Primary().Pool)
			store := memory.New(state.Config().Cache.Prefix)
			defer store.Close()

			checker := authz.NewChecker(queries, store)
			service := access.NewPermissionService(access.NewPermissionRepository(queries), checker)
			affected, err := service.SyncCatalog(ctx)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Synced %d permission(s).\n", affected)
			return nil
		},
	}
}

func newDBSeedCommand(state *State) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "db:seed",
		Short: "Seed permissions, tenant/roles/admin, settings groups/globals and feature flags",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cfg := state.Config()

			seedPassword, err := resolveSeedPassword(cmd, cfg, force)
			if err != nil {
				return err
			}
			cfg.Auth.DefaultPassword = seedPassword

			registry, cleanup, err := openDatabase(ctx, state)
			if err != nil {
				return err
			}
			defer cleanup()

			primary := registry.Primary()
			queries := dbgen.New(primary.Pool)
			tx := database.NewTxManager(primary)
			store := memory.New(cfg.Cache.Prefix)
			defer store.Close()

			checker := authz.NewChecker(queries, store)
			if err := seeder.Run(ctx, cfg, queries, tx, checker, state.Logger()); err != nil {
				return err
			}
			// Materialize the code-defined settings groups/globals and feature-flag
			// globals so the settings surface is complete after a plain seed.
			if _, err := settings.NewService(settings.NewRepository(queries), store).Sync(ctx); err != nil {
				return err
			}
			if _, err := featureflags.NewService(featureflags.NewRepository(queries), store).Sync(ctx); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Seeding completed.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Allow seeding in production (with safeguards)")
	return cmd
}

// resolveSeedPassword enforces the production seeding policy:
//
//   - production without --force: refused outright — the seed creates
//     publicly-known accounts (superadmin@ims.com, developer@ims.com).
//   - production with --force and a configured (non-default) password: the
//     operator's password is used.
//   - production with --force and the well-known default: a high-entropy
//     password is generated and printed once instead.
func resolveSeedPassword(cmd *cobra.Command, cfg *config.Config, force bool) (string, error) {
	if !cfg.IsProduction() {
		return cfg.Auth.DefaultPassword, nil
	}
	if cfg.Auth.DefaultPassword != config.DefaultSeedPassword {
		return cfg.Auth.DefaultPassword, nil
	}
	if !force {
		return "", fmt.Errorf("db:seed refuses to run in production with the default password: it would create publicly-known admin accounts; set AUTH_DEFAULT_PASSWORD first or pass --force to auto-generate a random password (printed once)")
	}
	password, err := generateSeedPassword()
	if err != nil {
		return "", err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Generated seed password (printed once, store it now): %s\n", password)
	return password, nil
}

func generateSeedPassword() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate seed password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
