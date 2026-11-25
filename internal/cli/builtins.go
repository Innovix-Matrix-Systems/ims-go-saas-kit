package cli

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/app"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/config"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/version"
)

func registerBuiltins(registry *Registry, state *State) error {
	commands := []*cobra.Command{
		newVersionCommand(),
		newConfigShowCommand(state),
		newHealthCheckCommand(state),
		newOpenAPICheckCommand(state),
	}
	for _, cmd := range commands {
		if err := registry.Add(cmd); err != nil {
			return err
		}
	}
	return nil
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the build version",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "ims %s\n", version.Version)
			return err
		},
	}
}

func newConfigShowCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "config:show",
		Short: "Print the effective configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := state.Config()
			out := cmd.OutOrStdout()
			rows := [][2]string{
				{"APP_NAME", cfg.App.Name},
				{"APP_ENV", cfg.App.Env},
				{"APP_DEBUG", fmt.Sprintf("%t", cfg.App.Debug)},
				{"APP_URL", cfg.App.URL},
				{"APP_LISTEN_ADDR", cfg.HTTP.ListenAddr},
				{"APP_TIMEZONE", cfg.App.Timezone},
				{"APP_LOCALE", cfg.App.Locale},
				{"APP_SUPPORTED_LOCALES", supportedLocales(cfg.App.SupportedLocales)},
				{"APP_FALLBACK_LOCALE", cfg.App.FallbackLocale},
				{"APP_LOG_LEVEL", cfg.App.LogLevel},
				{"APP_DATA_DIR", cfg.App.DataDir},
				{"APP_STORAGE_DIR", cfg.App.StorageDir},
				{"APP_CACHE_DIR", cfg.App.CacheDir},
				{"APP_ENV_FILE", cfg.App.EnvFile},
				{"HTTP_READ_TIMEOUT", cfg.HTTP.ReadTimeout.String()},
				{"HTTP_WRITE_TIMEOUT", cfg.HTTP.WriteTimeout.String()},
				{"HTTP_IDLE_TIMEOUT", cfg.HTTP.IdleTimeout.String()},
				{"HTTP_SHUTDOWN_TIMEOUT", cfg.HTTP.ShutdownTimeout.String()},
				{"HTTP_REQUEST_TIMEOUT", cfg.HTTP.RequestTimeout.String()},
				{"HTTP_MAX_REQUEST_BODY_BYTES", fmt.Sprintf("%d", cfg.HTTP.MaxRequestBodyBytes)},
				{"HTTP_STRICT_JSON_FIELDS", fmt.Sprintf("%t", cfg.HTTP.StrictJSONFields)},
				{"RATE_LIMIT_PER_MINUTE", fmt.Sprintf("%d", cfg.RateLimit.PerMinute)},
				{"DATABASE_DRIVER", cfg.Database.Primary.Driver},
				{"DATABASE_DSN", maskDSN(cfg.Database.Primary.DSN)},
				{"DATABASE_MAX_CONNS", fmt.Sprintf("%d", cfg.Database.Primary.MaxConns)},
				{"DATABASE_MIN_CONNS", fmt.Sprintf("%d", cfg.Database.Primary.MinConns)},
				{"DATABASE_CONN_TIMEOUT", cfg.Database.Primary.ConnTimeout.String()},
				{"DATABASE_CONNECTIONS", strings.Join(connectionNames(cfg), ",")},
			}
			for _, row := range rows {
				fmt.Fprintf(out, "%-28s %s\n", row[0], row[1])
			}
			return nil
		},
	}
}

func newHealthCheckCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "health:check",
		Short: "Run application health checks",
		RunE: func(cmd *cobra.Command, args []string) error {
			// No cfg.Validate(): reporting what is wrong with a broken install
			// is the point of this command, so it must start anyway (M46).
			deps, cleanup, err := app.NewDependencies(state.Config(), state.Logger())
			if err != nil {
				return err
			}
			defer cleanup()

			results := deps.Health.Run(cmd.Context())
			out := cmd.OutOrStdout()
			for _, result := range results.CheckResults {
				fmt.Fprintf(out, "%-8s %s: %s\n", result.Status, result.Name, result.ShortSummary)
			}
			if results.HasFailures() {
				return fmt.Errorf("one or more health checks failed")
			}
			return nil
		},
	}
}

func maskDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil || u.User == nil {
		return dsn
	}
	if _, hasPassword := u.User.Password(); !hasPassword {
		return dsn
	}
	rest := u.Path
	if u.RawQuery != "" {
		rest += "?" + u.RawQuery
	}
	return u.Scheme + "://" + u.User.Username() + ":********@" + u.Host + rest
}

func connectionNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Database.Connections))
	for _, conn := range cfg.Database.Connections {
		names = append(names, conn.Name)
	}
	return names
}

// supportedLocales shows APP_SUPPORTED_LOCALES, or what empty means.
func supportedLocales(codes []string) string {
	if len(codes) == 0 {
		return "(every registered language)"
	}
	return strings.Join(codes, ",")
}
