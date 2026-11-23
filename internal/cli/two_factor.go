package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/app"
)

func registerTwoFactorCommands(registry *Registry, state *State) error {
	return registry.Add(newTwoFactorResetCommand(state))
}

// newTwoFactorResetCommand turns a user's two-factor authentication off: the
// way back in for a platform account that lost its phone and recovery codes
// (tenant users have DELETE /api/v1/user/{id}/two-factor).
func newTwoFactorResetCommand(state *State) *cobra.Command {
	var email string

	cmd := &cobra.Command{
		Use:     "two-factor:reset",
		Short:   "Turn off a user's two-factor authentication",
		Long:    "Turn off two-factor authentication for the user with --email (mailed and recorded as user.two_factor_reset). Check who is asking before you run it.",
		Example: "  ims two-factor:reset --email=developer@ims.com",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(email) == "" {
				return fmt.Errorf("--email is required")
			}
			return withPlatform(state, func(platform *app.Platform) error {
				user, err := platform.IdentityRepo.FindByEmail(cmd.Context(), email)
				if err != nil {
					return err
				}
				if err := platform.TwoFactor.Disable(cmd.Context(), user, true); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Two-factor authentication is off for %s.\n", user.Email)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "the user's e-mail address")
	return cmd
}
