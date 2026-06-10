package cmd

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/AIEngineering26/promptvm-cli/internal/api"
	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	sdk "github.com/AIEngineering26/promptvm-go-sdk"
	"github.com/spf13/cobra"
)

var orgsCmd = &cobra.Command{
	Use:   "orgs",
	Short: "Manage organizations",
	Long:  "List, inspect, and administer organizations, members, roles, permissions, and invitations.",
}

var orgsMembersCmd = &cobra.Command{
	Use:   "members",
	Short: "Manage organization members",
}

var orgsRolesCmd = &cobra.Command{
	Use:   "roles",
	Short: "Manage organization roles",
}

var orgsInvitationsCmd = &cobra.Command{
	Use:   "invitations",
	Short: "Manage organization invitations",
}

func init() {
	rootCmd.AddCommand(orgsCmd)

	// orgs list / get
	orgsCmd.AddCommand(newOrgsListCmd())
	orgsCmd.AddCommand(newOrgsGetCmd())

	// orgs members
	orgsCmd.AddCommand(orgsMembersCmd)
	orgsMembersCmd.AddCommand(newOrgsMembersListCmd())
	orgsMembersCmd.AddCommand(newOrgsMembersAddCmd())
	orgsMembersCmd.AddCommand(newOrgsMembersRemoveCmd())
	orgsMembersCmd.AddCommand(newOrgsMembersSetRoleCmd())

	// orgs roles
	orgsCmd.AddCommand(orgsRolesCmd)
	orgsRolesCmd.AddCommand(newOrgsRolesListCmd())

	// orgs permissions
	orgsCmd.AddCommand(newOrgsPermissionsCmd())

	// orgs invite
	orgsCmd.AddCommand(newOrgsInviteCmd())

	// orgs invitations
	orgsCmd.AddCommand(orgsInvitationsCmd)
	orgsInvitationsCmd.AddCommand(newOrgsInvitationsListCmd())
	orgsInvitationsCmd.AddCommand(newOrgsInvitationsRevokeCmd())
}

// ===================== orgs list =====================

func newOrgsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List organizations",
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := api.NewFromContext(cmd)
			if err != nil {
				return err
			}

			var resp struct {
				Data []orgItem `json:"data"`
			}
			if err := raw.Get("/api/v1/organizations", &resp); err != nil {
				return err
			}

			return output.Print(cmd, resp.Data, func(w io.Writer) error {
				return output.Table(w, []string{"ID", "NAME", "SLUG", "ROLE"}, func(tw *tabwriter.Writer) {
					for _, o := range resp.Data {
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", o.ID, o.Name, o.Slug, o.Role)
					}
				})
			})
		},
	}
}

type orgItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	Role string `json:"role"`
}

// ===================== orgs get =====================

func newOrgsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get organization details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := api.NewFromContext(cmd)
			if err != nil {
				return err
			}

			var resp map[string]interface{}
			if err := raw.Get(fmt.Sprintf("/api/v1/organizations/%s", args[0]), &resp); err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}

			data, _ := resp["data"].(map[string]interface{})
			if data == nil {
				data = resp
			}
			printField(cmd, "ID", data["id"])
			printField(cmd, "Name", data["name"])
			printField(cmd, "Slug", data["slug"])
			printField(cmd, "Logo", data["logo"])
			printField(cmd, "Created", data["createdAt"])
			return nil
		},
	}
}

// ===================== orgs members list =====================

func newOrgsMembersListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <org-id>",
		Short: "List organization members",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := api.NewFromContext(cmd)
			if err != nil {
				return err
			}

			var resp struct {
				Data []memberItem `json:"data"`
			}
			if err := raw.Get(fmt.Sprintf("/api/v1/organizations/%s/members", args[0]), &resp); err != nil {
				return err
			}

			return output.Print(cmd, resp.Data, func(w io.Writer) error {
				return output.Table(w, []string{"USER ID", "NAME", "EMAIL", "ROLE", "JOINED"}, func(tw *tabwriter.Writer) {
					for _, m := range resp.Data {
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
							m.UserID, m.Name, m.Email, m.Role, m.JoinedAt)
					}
				})
			})
		},
	}
}

type memberItem struct {
	UserID   string `json:"userId"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Role     string `json:"role"`
	JoinedAt string `json:"joinedAt"`
}

// ===================== orgs members add =====================

func newOrgsMembersAddCmd() *cobra.Command {
	var (
		email string
		role  string
	)

	cmd := &cobra.Command{
		Use:   "add <org-id>",
		Short: "Add a member to an organization",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := api.NewFromContext(cmd)
			if err != nil {
				return err
			}

			body := map[string]string{
				"email": email,
				"role":  role,
			}
			if err := raw.Post(fmt.Sprintf("/api/v1/organizations/%s/members", args[0]), body, nil); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Added %s to organization %s (role: %s)\n", email, args[0], role)
			return nil
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "Member email (required)")
	cmd.MarkFlagRequired("email")
	cmd.Flags().StringVar(&role, "role", "viewer", "Role to assign (owner|admin|member|viewer)")
	return cmd
}

// ===================== orgs members remove =====================

func newOrgsMembersRemoveCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "remove <org-id> <user-id>",
		Short: "Remove a member from an organization",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				if !output.Confirm(fmt.Sprintf("⚠ Remove member %s from organization %s?", args[1], args[0])) {
					fmt.Fprintln(cmd.OutOrStdout(), "Cancelled.")
					return nil
				}
			}

			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			if err := c.Organizations.RemoveOrganizationMember(cmd.Context(), &sdk.RemoveOrganizationMemberRequest{
				OrgID:    args[0],
				MemberID: args[1],
			}); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Removed member %s from organization %s\n", args[1], args[0])
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

// ===================== orgs members set-role =====================

func newOrgsMembersSetRoleCmd() *cobra.Command {
	var role string

	cmd := &cobra.Command{
		Use:   "set-role <org-id> <user-id>",
		Short: "Change a member's role",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			r, err := sdk.NewUpdateOrganizationMemberRoleRequestRoleFromString(role)
			if err != nil {
				return err
			}

			if err := c.Organizations.UpdateOrganizationMemberRole(cmd.Context(), &sdk.UpdateOrganizationMemberRoleRequest{
				OrgID:    args[0],
				MemberID: args[1],
				Role:     r,
			}); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Updated role for member %s to %s\n", args[1], role)
			return nil
		},
	}

	cmd.Flags().StringVar(&role, "role", "", "New role (owner|admin|member|viewer) (required)")
	cmd.MarkFlagRequired("role")
	return cmd
}

// ===================== orgs roles list =====================

func newOrgsRolesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <org-id>",
		Short: "List available roles",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := api.NewFromContext(cmd)
			if err != nil {
				return err
			}

			var resp struct {
				Data []roleItem `json:"data"`
			}
			if err := raw.Get(fmt.Sprintf("/api/v1/organizations/%s/roles", args[0]), &resp); err != nil {
				return err
			}

			return output.Print(cmd, resp.Data, func(w io.Writer) error {
				return output.Table(w, []string{"ID", "NAME", "BASE ROLE", "DESCRIPTION"}, func(tw *tabwriter.Writer) {
					for _, r := range resp.Data {
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.ID, r.Name, r.BaseRole, r.Description)
					}
				})
			})
		},
	}
}

type roleItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	BaseRole    string `json:"baseRole"`
	Description string `json:"description"`
}

// ===================== orgs permissions =====================

func newOrgsPermissionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "permissions <org-id>",
		Short: "Show permission matrix",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := api.NewFromContext(cmd)
			if err != nil {
				return err
			}

			var resp map[string]interface{}
			if err := raw.Get(fmt.Sprintf("/api/v1/organizations/%s/permissions", args[0]), &resp); err != nil {
				return err
			}

			// Always output as JSON/YAML — permissions are complex nested structures
			return output.Print(cmd, resp, func(w io.Writer) error {
				// Default to JSON for table mode since permissions are a matrix
				enc := fmt.Sprintf("%v", resp)
				_, err := fmt.Fprintln(w, enc)
				return err
			})
		},
	}
}

// ===================== orgs invite =====================

func newOrgsInviteCmd() *cobra.Command {
	var (
		email string
		role  string
	)

	cmd := &cobra.Command{
		Use:   "invite <org-id>",
		Short: "Send an invitation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			r, err := sdk.NewCreateOrganizationInvitationRequestRoleFromString(role)
			if err != nil {
				return err
			}

			if err := c.Organizations.CreateOrganizationInvitation(cmd.Context(), &sdk.CreateOrganizationInvitationRequest{
				OrgID: args[0],
				Email: email,
				Role:  r,
			}); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Invitation sent to %s (role: %s)\n", email, role)
			return nil
		},
	}

	cmd.Flags().StringVar(&email, "email", "", "Invitee email (required)")
	cmd.MarkFlagRequired("email")
	cmd.Flags().StringVar(&role, "role", "viewer", "Role to assign (owner|admin|member|viewer)")
	return cmd
}

// ===================== orgs invitations list =====================

func newOrgsInvitationsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <org-id>",
		Short: "List pending invitations",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := api.NewFromContext(cmd)
			if err != nil {
				return err
			}

			var resp struct {
				Data []invitationItem `json:"data"`
			}
			if err := raw.Get(fmt.Sprintf("/api/v1/organizations/%s/invitations", args[0]), &resp); err != nil {
				return err
			}

			return output.Print(cmd, resp.Data, func(w io.Writer) error {
				return output.Table(w, []string{"ID", "EMAIL", "ROLE", "STATUS", "EXPIRES"}, func(tw *tabwriter.Writer) {
					for _, inv := range resp.Data {
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
							inv.ID, inv.Email, inv.Role, inv.Status, inv.ExpiresAt)
					}
				})
			})
		},
	}
}

type invitationItem struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	Status    string `json:"status"`
	ExpiresAt string `json:"expiresAt"`
}

// ===================== orgs invitations revoke =====================

func newOrgsInvitationsRevokeCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "revoke <org-id> <invitation-id>",
		Short: "Revoke a pending invitation",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				if !output.Confirm(fmt.Sprintf("Revoke invitation %s?", args[1])) {
					fmt.Fprintln(cmd.OutOrStdout(), "Cancelled.")
					return nil
				}
			}

			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			if err := c.Organizations.RevokeOrganizationInvitation(cmd.Context(), &sdk.RevokeOrganizationInvitationRequest{
				OrgID:        args[0],
				InvitationID: args[1],
			}); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Revoked invitation %s\n", args[1])
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}
