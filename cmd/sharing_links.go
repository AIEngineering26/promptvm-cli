package cmd

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	sdk "github.com/AIEngineering26/promptvm-go-sdk"
	"github.com/spf13/cobra"
)

// Share LINKS, as opposed to `share collab`, which manages named collaborators.
//
// `share create` could mint a link but nothing could enumerate or revoke one —
// so a link, once created, was unrevokable from the command line even though
// the API has supported both since the Sharing Manager shipped. A link you
// cannot withdraw is worse than one you cannot name.
var shareLinksCmd = &cobra.Command{
	Use:     "links",
	Aliases: []string{"link"},
	Short:   "List and revoke a prompt's share links",
}

func init() {
	shareCmd.AddCommand(shareLinksCmd)
	shareLinksCmd.AddCommand(newShareLinksListCmd())
	shareLinksCmd.AddCommand(newShareLinksRevokeCmd())
}

func newShareLinksListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <prompt-id>",
		Short: "List the share links on a prompt",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.Sharing.ListPromptShareLinks(cmd.Context(),
				&sdk.ListPromptShareLinksRequest{PromptID: args[0]})
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}
			if len(resp.Data) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No share links on this prompt.")
				return nil
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				return output.Table(w, []string{"ID", "LABEL", "TYPE", "PERMISSION", "USES", "EXPIRES"}, func(tw *tabwriter.Writer) {
					for _, l := range resp.Data {
						label := "-"
						if l.Label != nil && *l.Label != "" {
							label = *l.Label
						}
						// A capped link reads "3/10"; an uncapped one just counts up.
						uses := fmt.Sprintf("%d", l.UseCount)
						if l.MaxUses != nil {
							uses = fmt.Sprintf("%d/%d", l.UseCount, *l.MaxUses)
						}
						expires := "never"
						if l.ExpiresAt != nil {
							expires = l.ExpiresAt.Format(time.RFC3339)
						}
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
							l.ID, label, string(l.Kind), l.Permission, uses, expires)
					}
				})
			})
		},
	}
}

func newShareLinksRevokeCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "revoke <prompt-id> <link-id>",
		Short: "Revoke a share link",
		Long: "Revokes a single share link by id — run `share links list <prompt-id>`\n" +
			"to find it. Anyone holding the URL loses access immediately.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				// Outward-facing and irreversible: whoever holds the URL loses
				// access the moment this runs.
				return fmt.Errorf("revoking a share link cannot be undone; pass --yes to confirm")
			}

			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}
			if err := c.Sharing.RevokePromptShareLink(cmd.Context(),
				&sdk.RevokePromptShareLinkRequest{PromptID: args[0], LinkID: args[1]}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Revoked share link %s\n", args[1])
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Confirm the revocation")
	return cmd
}
