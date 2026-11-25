package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/app"
	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/delivery"
)

func registerDeliveryCommands(registry *Registry, state *State) error {
	for _, cmd := range []*cobra.Command{
		newDeliveryTestCommand(state),
		newDeliveryPruneCommand(state),
	} {
		if err := registry.Add(cmd); err != nil {
			return err
		}
	}
	return nil
}

// newDeliveryTestCommand sends one message now through the configured
// driver, the quickest check that mail or SMS settings work. It is the CLI
// twin of the dashboard's delivery:test command.
func newDeliveryTestCommand(state *State) *cobra.Command {
	var to, channel, template, locale string

	cmd := &cobra.Command{
		Use:   "delivery:test",
		Short: "Send a test message by mail, SMS or push through the configured driver",
		Long: "Send one message now (not queued) to --to: an e-mail address for mail, a phone number\n" +
			"(E.164) for sms, a user id for push. Without --template a plain test message is sent;\n" +
			"with one, the template is rendered with its sample data.",
		Example: "  ims delivery:test --to=you@example.com\n" +
			"  ims delivery:test --to=you@example.com --template=password-reset --locale=bn\n" +
			"  ims delivery:test --channel=sms --to=+8801712345678 --template=otp-code",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(to) == "" {
				return fmt.Errorf("--to is required")
			}
			return withPlatform(state, func(platform *app.Platform) error {
				message, err := app.DeliveryTestMessage(platform.Delivery, delivery.Channel(channel), to, template, locale)
				if err != nil {
					return err
				}
				if err := platform.Delivery.SendNow(cmd.Context(), message); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Sent a test %s to %s through the %s driver.\n",
					channel, to, platform.Delivery.Driver(delivery.Channel(channel)))
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "recipient: e-mail (mail), E.164 phone (sms) or user id (push)")
	cmd.Flags().StringVar(&channel, "channel", string(delivery.Mail), "mail | sms | push")
	cmd.Flags().StringVar(&template, "template", "", "template to render with its sample data (default: a plain test message)")
	cmd.Flags().StringVar(&locale, "locale", "", "template locale, e.g. bn (default: APP_FALLBACK_LOCALE)")
	return cmd
}

// newDeliveryPruneCommand is the CLI twin of the delivery.prune task.
func newDeliveryPruneCommand(state *State) *cobra.Command {
	var days int

	cmd := &cobra.Command{
		Use:     "delivery:prune",
		Short:   "Delete delivery-log rows older than a number of days",
		Example: "  ims delivery:prune --days=14",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withPlatform(state, func(platform *app.Platform) error {
				if days == 0 {
					days = platform.Config.Delivery.RetentionDays
				}
				if days < 1 {
					return fmt.Errorf("--days must be at least 1 (DELIVERY_RETENTION_DAYS is 0: keep forever)")
				}
				deleted, err := platform.Delivery.Prune(cmd.Context(), days)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Deleted %d delivery row(s) older than %d day(s).\n", deleted, days)
				return nil
			})
		},
	}
	cmd.Flags().IntVar(&days, "days", 0, "delete rows older than this many days (default: DELIVERY_RETENTION_DAYS)")
	return cmd
}
