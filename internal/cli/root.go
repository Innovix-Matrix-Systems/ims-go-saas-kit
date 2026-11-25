package cli

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/logging"
)

// State holds the lazily loaded configuration and logger shared by commands.
// Commands that need no infrastructure (version, help) never touch it.
type State struct {
	cfg    *config.Config
	logger *slog.Logger
}

// Config returns the loaded configuration (nil before load).
func (s *State) Config() *config.Config {
	return s.cfg
}

// Logger returns the configured logger (nil before load).
func (s *State) Logger() *slog.Logger {
	return s.logger
}

func (s *State) load(envFile string, verbose bool) error {
	if s.cfg != nil {
		return nil
	}
	cfg, err := config.Load(envFile)
	if err != nil {
		return err
	}
	s.cfg = cfg
	s.logger = logging.New(cfg.App.Env, cfg.App.LogLevel, verbose || cfg.App.Debug, os.Stdout)
	return nil
}

// BuildRoot assembles the CLI command tree from the command registry.
func BuildRoot(state *State) (*cobra.Command, error) {
	var envFile string
	var verbose bool

	root := &cobra.Command{
		Use:           "ims",
		Short:         "IMS Go SaaS Kit",
		Long:          "IMS Go SaaS Kit. Run `ims help` to list commands.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return state.load(envFile, verbose)
		},
	}
	root.PersistentFlags().StringVar(&envFile, "env-file", "", "path to the env file (default: APP_ENV_FILE or .env)")
	root.PersistentFlags().BoolVar(&verbose, "verbose", false, "enable debug logging")

	registry := NewRegistry()
	registrars := []func(*Registry, *State) error{
		registerBuiltins,
		registerDatabaseCommands,
		registerSeedCommands,
		registerCacheCommands,
		registerMaintenanceCommands,
		registerKeyCommands,
		registerTwoFactorCommands,
		registerActivityCommands,
		registerPrivacyCommands,
		registerDeliveryCommands,
		registerRouteCommands,
		registerQueueCommands,
		registerScheduleCommands,
		registerSettingsCommands,
		registerDataProcessingCommands,
		registerBillingCommands,
		registerBackupCommands,
		registerOAuthCommands,
		registerDevCommands,
		registerGenCommands,
		registerAppCommands,
	}
	for _, register := range registrars {
		if err := register(registry, state); err != nil {
			return nil, err
		}
	}
	for _, cmd := range registry.Commands() {
		root.AddCommand(cmd)
	}
	return root, nil
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	state := &State{}
	root, err := BuildRoot(state)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	return 0
}
