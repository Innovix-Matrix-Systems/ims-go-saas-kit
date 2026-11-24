package cli

import (
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/app"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/oauth"
)

// The MCP connector's manually registered OAuth clients, for ops
// and scripts. The same as /api/v1/oauth-client.
func registerOAuthCommands(registry *Registry, state *State) error {
	for _, cmd := range []*cobra.Command{
		newOAuthClientCreateCommand(state),
		newOAuthClientListCommand(state),
		newOAuthClientRotateCommand(state),
		newOAuthClientDeleteCommand(state),
	} {
		if err := registry.Add(cmd); err != nil {
			return err
		}
	}
	return nil
}

func newOAuthClientCreateCommand(state *State) *cobra.Command {
	var name string
	var redirects []string
	var confidential, readOnly bool
	var tenantID int64
	cmd := &cobra.Command{
		Use:   "oauth:client:create",
		Short: "Register an OAuth client for the MCP connector",
		Example: `  ims oauth:client:create --name "Ops agent" --redirect https://agent.example.com/callback --confidential
  ims oauth:client:create --name "Local tool" --redirect http://localhost/callback`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withPlatform(state, func(platform *app.Platform) error {
				in := oauth.ClientInput{Name: name, Type: oauth.TypePublic, RedirectURIs: redirects, TenantID: tenantID, IsActive: true}
				if confidential {
					in.Type = oauth.TypeConfidential
				}
				if readOnly {
					in.Scopes = []string{"mcp:read"}
				}
				created, err := platform.OAuthService().CreateClient(cmd.Context(), 0, in)
				if err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "client_id:     %s\n", created.ClientID)
				if created.Secret != "" {
					fmt.Fprintf(out, "client_secret: %s\n", created.Secret)
					fmt.Fprintln(out, "Store the secret now: it is not shown again.")
				}
				fmt.Fprintf(out, "type:          %s\nscopes:        %s\nredirect_uris: %s\n",
					created.Type, strings.Join(created.Scopes, " "), strings.Join(created.RedirectURIs, ", "))
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "name shown on the consent page (required)")
	cmd.Flags().StringArrayVar(&redirects, "redirect", nil, "redirect URI (repeat for more; https, or http on localhost)")
	cmd.Flags().BoolVar(&confidential, "confidential", false, "give the client a secret (server-side clients)")
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "never grant mcp:write")
	cmd.Flags().Int64Var(&tenantID, "tenant", 0, "only this tenant's users may connect")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("redirect")
	return cmd
}

func newOAuthClientListCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "oauth:client:list",
		Short: "List the OAuth clients registered for the MCP connector",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withPlatform(state, func(platform *app.Platform) error {
				rows, _, err := platform.OAuthService().ListClients(cmd.Context(), "", 500, 0)
				if err != nil {
					return err
				}
				w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
				fmt.Fprintln(w, "ID\tCLIENT_ID\tNAME\tTYPE\tSCOPES\tACTIVE")
				for _, row := range rows {
					fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%v\n", row.ID, row.ClientID, row.Name, row.Type,
						strings.Join(row.Scopes, " "), row.IsActive)
				}
				return w.Flush()
			})
		},
	}
}

func newOAuthClientRotateCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "oauth:client:rotate <id>",
		Short: "Give a confidential OAuth client a new secret (the old one works for MCP_OAUTH_SECRET_GRACE)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("the id must be a number")
			}
			return withPlatform(state, func(platform *app.Platform) error {
				rotated, err := platform.OAuthService().RotateSecret(cmd.Context(), 0, id)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "client_secret: %s\nStore the secret now: it is not shown again.\n", rotated.Secret)
				return nil
			})
		},
	}
}

func newOAuthClientDeleteCommand(state *State) *cobra.Command {
	return &cobra.Command{
		Use:   "oauth:client:delete <id>",
		Short: "Delete an OAuth client and revoke every connection made with it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("the id must be a number")
			}
			return withPlatform(state, func(platform *app.Platform) error {
				if err := platform.OAuthService().DeleteClient(cmd.Context(), 0, id); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "deleted; its connections are revoked")
				return nil
			})
		},
	}
}
