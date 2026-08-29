package cmd

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	sdk "github.com/AIEngineering26/promptvm-go-sdk"
	"github.com/spf13/cobra"
)

var shareCmd = &cobra.Command{
	Use:   "share",
	Short: "Manage sharing",
	Long:  "Create share links, manage collaborators on prompts.",
}

var shareCollabCmd = &cobra.Command{
	Use:     "collaborators",
	Aliases: []string{"collab"},
	Short:   "Manage collaborators",
	Long:    "List, add, and remove collaborators on shared prompts.",
}

func init() {
	rootCmd.AddCommand(shareCmd)
	shareCmd.AddCommand(newShareCreateCmd())
	shareCmd.AddCommand(newShareGetCmd())
	shareCmd.AddCommand(newShareRevokeCmd())
	shareCmd.AddCommand(shareCollabCmd)
	shareCollabCmd.AddCommand(newCollabListCmd())
	shareCollabCmd.AddCommand(newCollabAddCmd())
	shareCollabCmd.AddCommand(newCollabRemoveCmd())
}

// --- share create ---

func newShareCreateCmd() *cobra.Command {
	var (
		expires    int
		permission string
		password   string
		maxUses    int
		label      string
		linkType   string
	)

	cmd := &cobra.Command{
		Use:   "create <prompt-id>",
		Short: "Create a share link",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.CreatePromptShareLinkRequest{
				PromptID: args[0],
			}
			if permission != "" {
				p, err := sdk.NewCreatePromptShareLinkRequestPermissionFromString(permission)
				if err != nil {
					return err
				}
				req.Permission = p.Ptr()
			}
			if password != "" {
				req.Password = &password
			}
			if expires > 0 {
				req.ExpiresInHours = &expires
			}
			if maxUses > 0 {
				req.MaxUses = &maxUses
			}
			if label != "" {
				req.Label = &label
			}
			if linkType != "" {
				k, err := sdk.NewCreatePromptShareLinkRequestKindFromString(linkType)
				if err != nil {
					return fmt.Errorf("invalid --type %q: expected web or agent", linkType)
				}
				req.Kind = k.Ptr()
			}

			resp, err := c.Sharing.CreatePromptShareLink(cmd.Context(), req)
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}

			d := resp.GetData()
			if d == nil {
				return fmt.Errorf("empty response")
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Share link created:")
			printField(cmd, "Label", deref(d.GetLabel()))
			printField(cmd, "Type", string(d.GetKind()))
			// The server mints BOTH urls whatever the type; the type decides
			// which one is the point of the link, so lead with that one.
			if u := d.GetURLs(); u != nil {
				if d.GetKind() == "agent" {
					printField(cmd, "URL", u.GetAgent())
					printField(cmd, "Web URL", u.GetWeb())
				} else {
					printField(cmd, "URL", u.GetWeb())
					printField(cmd, "Agent URL", u.GetAgent())
				}
			} else {
				printField(cmd, "URL", d.GetURL())
			}
			printField(cmd, "Token", d.GetToken())
			if d.GetExpiresAt() != nil {
				printField(cmd, "Expires", output.HumanTime(*d.GetExpiresAt()))
			}
			printField(cmd, "Permission", d.GetPermission())
			if d.GetMaxUses() != nil {
				printField(cmd, "Max Uses", *d.GetMaxUses())
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&expires, "expires", 0, "Expiration in hours (e.g., 24 for 1 day, 168 for 7 days)")
	cmd.Flags().StringVar(&permission, "permission", "view", "Permission level: view|edit|comment|execute")
	cmd.Flags().StringVar(&password, "password", "", "Password-protect the share link")
	cmd.Flags().IntVar(&maxUses, "max-uses", 0, "Maximum number of uses (0 = unlimited)")
	cmd.Flags().StringVar(&label, "label", "", "Name for the link, shown in the Sharing Manager (e.g. \"Marketing team review\")")
	// Not --kind: that already means the prompt kind (template|instance) on
	// `prompts create`, and two meanings for one flag name is a trap.
	cmd.Flags().StringVar(&linkType, "type", "", "Link type: web (a public page) or agent (an llms.txt URL for AI agents). Default web")
	return cmd
}

// --- share get ---

func newShareGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <token>",
		Short: "Get share link details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.Sharing.AccessSharedPrompt(cmd.Context(), &sdk.AccessSharedPromptRequest{
				Token: args[0],
			})
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}

			d := resp.GetData()
			if d == nil {
				return fmt.Errorf("share link not found")
			}
			// A share link is a PUBLIC projection: the endpoint deliberately
			// withholds internal identifiers (prompt id, slug, status,
			// isPublic, workspace and author ids), pinned by the backend's
			// "serializes only the public projection" test. Printing them here
			// showed blanks, because the server had already stopped sending
			// them and the SDK was decoding absent JSON into zero values.
			printField(cmd, "Name", d.GetName())
			// The link's own label and type, as opposed to the prompt's kind.
			printField(cmd, "Label", deref(d.GetLabel()))
			if lk := d.GetLinkKind(); lk != nil {
				printField(cmd, "Type", string(*lk))
			}
			printField(cmd, "Kind", string(d.GetKind()))
			printField(cmd, "Content Kind", string(d.GetContentKind()))
			printField(cmd, "Author", deref(d.GetAuthorName()))
			printField(cmd, "Version", deref(d.GetSharedVersionNumber()))
			printField(cmd, "Created", output.HumanTime(d.GetCreatedAt()))
			return nil
		},
	}
	return cmd
}

// --- share revoke ---

func newShareRevokeCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "revoke <prompt-id> <collaborator-id>",
		Short: "Revoke a share (remove collaborator)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				if !output.Confirm(fmt.Sprintf("Revoke collaborator %s from prompt %s?", args[1], args[0])) {
					fmt.Fprintln(cmd.OutOrStdout(), "Cancelled.")
					return nil
				}
			}

			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.Sharing.RevokePromptCollaborator(cmd.Context(), &sdk.RevokePromptCollaboratorRequest{
				PromptID:       args[0],
				CollaboratorID: args[1],
			})
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Revoked collaborator %s\n", args[1])
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

// --- collaborators list ---

func newCollabListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list <prompt-id>",
		Short: "List collaborators",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.Sharing.ListPromptCollaborators(cmd.Context(), &sdk.ListPromptCollaboratorsRequest{
				PromptID: args[0],
			})
			if err != nil {
				return err
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				return output.Table(w, []string{"ID", "NAME", "EMAIL", "PERMISSION", "ADDED"}, func(tw *tabwriter.Writer) {
					for _, collab := range resp.GetData() {
						name := "-"
						if collab.GetName() != nil {
							name = *collab.GetName()
						}
						email := "-"
						if collab.GetEmail() != nil {
							email = *collab.GetEmail()
						}
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
							collab.GetID(), name, email,
							collab.GetPermission(), output.HumanTime(collab.GetCreatedAt()))
					}
				})
			})
		},
	}
	return cmd
}

// --- collaborators add ---

func newCollabAddCmd() *cobra.Command {
	var (
		email  string
		role   string
		userID string
	)

	cmd := &cobra.Command{
		Use:   "add <prompt-id>",
		Short: "Add a collaborator",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			perm, err := sdk.NewSharePromptRequestPermissionFromString(role)
			if err != nil {
				return err
			}

			req := &sdk.SharePromptRequest{
				PromptID:   args[0],
				Permission: perm,
			}

			// Resolve user by email or direct user ID
			if userID != "" {
				req.UserID = &userID
			} else if email != "" {
				// The API accepts userId; for email-based sharing the backend resolves it.
				// Pass email as userID — the backend supports email resolution.
				req.UserID = &email
			} else {
				return fmt.Errorf("--email or --user-id is required")
			}

			resp, err := c.Sharing.SharePrompt(cmd.Context(), req)
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}

			target := email
			if target == "" {
				target = userID
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added %s as %s on prompt %s\n", target, role, args[0])
			return nil
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "Collaborator email address")
	cmd.Flags().StringVar(&userID, "user-id", "", "Collaborator user ID")
	cmd.Flags().StringVar(&role, "role", "view", "Permission: view|comment|edit|execute|import|share")
	return cmd
}

// --- collaborators remove ---

func newCollabRemoveCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "remove <prompt-id> <user-id>",
		Short: "Remove a collaborator",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				if !output.Confirm(fmt.Sprintf("Remove collaborator %s from prompt %s?", args[1], args[0])) {
					fmt.Fprintln(cmd.OutOrStdout(), "Cancelled.")
					return nil
				}
			}

			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.Sharing.RevokePromptCollaborator(cmd.Context(), &sdk.RevokePromptCollaboratorRequest{
				PromptID:       args[0],
				CollaboratorID: args[1],
			})
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed collaborator %s from prompt %s\n", args[1], args[0])
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}
