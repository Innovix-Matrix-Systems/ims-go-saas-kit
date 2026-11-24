package cli

import (
	"crypto/rand"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
)

func registerKeyCommands(registry *Registry, state *State) error {
	return registry.Add(newKeyGenerateCommand(state))
}

var appKeyLine = regexp.MustCompile(`(?m)^APP_KEY=.*$`)

// newKeyGenerateCommand is Laravel's key:generate: a new APP_KEY, written to
// the .env file (or printed with --show).
func newKeyGenerateCommand(state *State) *cobra.Command {
	var show, force bool

	cmd := &cobra.Command{
		Use:   "key:generate",
		Short: "Generate the application encryption key (APP_KEY)",
		Long: "Generate a random APP_KEY and write it to the .env file. APP_KEY encrypts values the\n" +
			"app must read back (two-factor secrets, sensitive job payloads). Replacing a key that is\n" +
			"in use makes those values unreadable: move the old key to APP_PREVIOUS_KEYS first.",
		Example: "  ims key:generate\n  ims key:generate --show",
		RunE: func(cmd *cobra.Command, args []string) error {
			key := make([]byte, config.AppKeyLength)
			if _, err := rand.Read(key); err != nil {
				return fmt.Errorf("generate key: %w", err)
			}
			encoded := config.EncodeAppKey(key)
			if show {
				fmt.Fprintln(cmd.OutOrStdout(), encoded)
				return nil
			}
			path := state.Config().App.EnvFile
			if path == "" {
				path = ".env"
			}
			contents, err := os.ReadFile(path)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("read %s: %w", path, err)
			}
			current := appKeyLine.Find(contents)
			if current != nil && strings.TrimSpace(strings.TrimPrefix(string(current), "APP_KEY=")) != "" && !force {
				return fmt.Errorf("%s already has an APP_KEY; replacing it makes encrypted values unreadable. Move it to APP_PREVIOUS_KEYS, then run with --force", path)
			}
			line := "APP_KEY=" + encoded
			if current != nil {
				contents = appKeyLine.ReplaceAll(contents, []byte(line))
			} else {
				if len(contents) > 0 && !strings.HasSuffix(string(contents), "\n") {
					contents = append(contents, '\n')
				}
				contents = append(contents, []byte(line+"\n")...)
			}
			if err := os.WriteFile(path, contents, 0o600); err != nil {
				return fmt.Errorf("write %s: %w", path, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Application key set in %s.\n", path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&show, "show", false, "print the key instead of writing it")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing APP_KEY")
	return cmd
}
