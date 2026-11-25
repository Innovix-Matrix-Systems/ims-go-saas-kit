package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/app"
)

func registerBillingCommands(registry *Registry, state *State) error {
	commands := []*cobra.Command{
		newBillingExpireCommand(state),
		newBillingRemindersCommand(state),
	}
	for _, cmd := range commands {
		if err := registry.Add(cmd); err != nil {
			return err
		}
	}
	return nil
}

func newBillingExpireCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "billing:expire-subscriptions",
		Short: "Mark overdue subscriptions as expired and start scheduled ones",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withPlatform(state, func(platform *app.Platform) error {
				expired, started, err := platform.BillingSubs.ProcessPeriodEnds(cmd.Context())
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Expired %d subscription(s), started %d scheduled.\n", expired, started)
				return nil
			})
		},
	}
}

func newBillingRemindersCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "billing:send-renewal-reminders",
		Short: "Log renewal reminders for subscriptions expiring within 7/3/1 days",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withPlatform(state, func(platform *app.Platform) error {
				sent, err := platform.BillingSubs.SendRenewalReminders(cmd.Context(), state.Logger(), []int{7, 3, 1})
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Queued %d renewal reminder(s).\n", sent)
				return nil
			})
		},
	}
}
