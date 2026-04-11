package cmd

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	sdk "github.com/AIEngineering26/promptvm-go-sdk"
	"github.com/AIEngineering26/promptvm-cli/internal/api"
	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/config"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/spf13/cobra"
)

var workspacesCmd = &cobra.Command{
	Use:     "workspaces",
	Aliases: []string{"ws"},
	Short:   "Manage workspaces",
	Long:    "Create, list, get, update, delete, transfer, pin, and unpin workspaces.",
}

func init() {
	rootCmd.AddCommand(workspacesCmd)
	workspacesCmd.AddCommand(newWsListCmd())
	workspacesCmd.AddCommand(newWsCreateCmd())
	workspacesCmd.AddCommand(newWsGetCmd())
	workspacesCmd.AddCommand(newWsUpdateCmd())
	workspacesCmd.AddCommand(newWsDeleteCmd())
	workspacesCmd.AddCommand(newWsTransferCmd())
	workspacesCmd.AddCommand(newWsPinCmd())
	workspacesCmd.AddCommand(newWsUnpinCmd())
}

// resolveOrgID resolves organization ID from flag → profile → error.
func resolveOrgID(cmd *cobra.Command, flagName string) (string, error) {
	if flagName != "" {
		if v, _ := cmd.Flags().GetString(flagName); v != "" {
			return v, nil
		}
	}
	// Fall back to profile organization
	cfg, err := config.Load()
	if err == nil {
		if p, err := cfg.ActiveProfileData(); err == nil && p.Organization != "" {
			return p.Organization, nil
		}
	}
	return "", fmt.Errorf("no organization specified. Use --org flag or set organization in your profile via `promptvm auth login`")
}

// resolveDefaultWorkspace resolves workspace ID from flag → config → error.
func resolveDefaultWorkspace(cmd *cobra.Command) (string, error) {
	if v, _ := cmd.Flags().GetString("workspace"); v != "" {
		return v, nil
	}
	cfg, err := config.Load()
	if err == nil && cfg.Defaults.Workspace != "" {
		return cfg.Defaults.Workspace, nil
	}
	return "", fmt.Errorf("no workspace specified. Use --workspace or set a default:\n  promptvm config set defaults.workspace <workspace-id>")
}

// --- list ---

func newWsListCmd() *cobra.Command {
	var org string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List workspaces",
		RunE: func(cmd *cobra.Command, args []string) error {
			orgID, err := resolveOrgID(cmd, "org")
			if err != nil {
				return err
			}

			raw, err := api.NewFromContext(cmd)
			if err != nil {
				return err
			}

			var resp struct {
				Data []wsItem `json:"data"`
			}
			if err := raw.Get(fmt.Sprintf("/api/v1/workspaces?organizationId=%s", orgID), &resp); err != nil {
				return err
			}

			// Load default workspace for marker
			defaultWs := ""
			if cfg, err := config.Load(); err == nil {
				defaultWs = cfg.Defaults.Workspace
			}

			return output.Print(cmd, resp.Data, func(w io.Writer) error {
				return output.Table(w, []string{"", "ID", "NAME", "VISIBILITY", "PINNED", "UPDATED"}, func(tw *tabwriter.Writer) {
					for _, ws := range resp.Data {
						marker := " "
						if ws.ID == defaultWs {
							marker = "*"
						}
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%v\t%s\n",
							marker, ws.ID, ws.Name, ws.Visibility, ws.Pinned, humanTimePtr(ws.UpdatedAt))
					}
				})
			})
		},
	}

	cmd.Flags().StringVar(&org, "org", "", "Organization ID")
	return cmd
}

type wsItem struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug"`
	Description string     `json:"description"`
	Visibility  string     `json:"visibility"`
	Pinned      bool       `json:"pinned"`
	IsDefault   bool       `json:"isDefault"`
	UpdatedAt   *time.Time `json:"updatedAt"`
}

func humanTimePtr(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return humanTime(*t)
}

// --- create ---

func newWsCreateCmd() *cobra.Command {
	var (
		name        string
		description string
		visibility  string
		org         string
		setDefault  bool
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			orgID, err := resolveOrgID(cmd, "org")
			if err != nil {
				return err
			}

			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.CreateWorkspaceRequest{
				Name:           name,
				OrganizationID: orgID,
			}
			if description != "" {
				req.Description = &description
			}
			if visibility != "" {
				v, err := sdk.NewCreateWorkspaceRequestVisibilityFromString(visibility)
				if err != nil {
					return err
				}
				req.Visibility = v.Ptr()
			}

			resp, err := c.Workspaces.CreateWorkspace(cmd.Context(), req)
			if err != nil {
				return err
			}

			wsID := ""
			wsName := name
			if d := resp.GetData(); d != nil {
				if id, ok := d["id"].(string); ok {
					wsID = id
				}
				if n, ok := d["name"].(string); ok {
					wsName = n
				}
			}

			if setDefault && wsID != "" {
				cfg, err := config.Load()
				if err == nil {
					cfg.Defaults.Workspace = wsID
					_ = cfg.Save()
				}
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created workspace %s %q\n", wsID, wsName)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Workspace name (required)")
	cmd.MarkFlagRequired("name")
	cmd.Flags().StringVar(&description, "description", "", "Workspace description")
	cmd.Flags().StringVar(&visibility, "visibility", "", "Visibility (private|public|internal)")
	cmd.Flags().StringVar(&org, "org", "", "Organization ID")
	cmd.Flags().BoolVar(&setDefault, "set-default", false, "Set as default workspace in config")
	return cmd
}

// --- get ---

func newWsGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get workspace details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.Workspaces.GetWorkspace(cmd.Context(), &sdk.GetWorkspaceRequest{
				WorkspaceID: args[0],
			})
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}

			d := resp.GetData()
			printField(cmd, "ID", d["id"])
			printField(cmd, "Name", d["name"])
			printField(cmd, "Slug", d["slug"])
			printField(cmd, "Description", d["description"])
			printField(cmd, "Visibility", d["visibility"])
			printField(cmd, "Owner", d["ownerId"])
			printField(cmd, "Default", d["isDefault"])
			return nil
		},
	}
	return cmd
}

func printField(cmd *cobra.Command, label string, value interface{}) {
	if value != nil && value != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "%-14s %v\n", label+":", value)
	}
}

// --- update ---

func newWsUpdateCmd() *cobra.Command {
	var (
		name        string
		description string
		visibility  string
	)

	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.UpdateWorkspaceRequest{
				WorkspaceID: args[0],
			}
			if name != "" {
				req.Name = &name
			}
			if description != "" {
				req.Description = &description
			}
			if visibility != "" {
				v, err := sdk.NewUpdateWorkspaceRequestVisibilityFromString(visibility)
				if err != nil {
					return err
				}
				req.Visibility = v.Ptr()
			}

			resp, err := c.Workspaces.UpdateWorkspace(cmd.Context(), req)
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Updated workspace %s\n", args[0])
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "New name")
	cmd.Flags().StringVar(&description, "description", "", "New description")
	cmd.Flags().StringVar(&visibility, "visibility", "", "New visibility (private|public|internal)")
	return cmd
}

// --- delete ---

func newWsDeleteCmd() *cobra.Command {
	var (
		yes     bool
		cascade bool
	)

	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				if !output.Confirm(fmt.Sprintf("⚠ This will permanently delete workspace %q and all its contents.\nAre you sure?", args[0])) {
					fmt.Fprintln(cmd.OutOrStdout(), "Cancelled.")
					return nil
				}
			}

			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.DeleteWorkspaceRequest{
				WorkspaceID: args[0],
			}
			if cascade {
				cas := sdk.DeleteWorkspaceRequestCascadeTrue
				req.Cascade = cas.Ptr()
			}

			resp, err := c.Workspaces.DeleteWorkspace(cmd.Context(), req)
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted workspace %s\n", args[0])
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&cascade, "cascade", false, "Also delete all prompts and directories")
	return cmd
}

// --- transfer ---

func newWsTransferCmd() *cobra.Command {
	var (
		newOwner string
		yes      bool
	)

	cmd := &cobra.Command{
		Use:   "transfer <id>",
		Short: "Transfer workspace ownership",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				if !output.Confirm(fmt.Sprintf("⚠ Transfer ownership of workspace %q to %s?", args[0], newOwner)) {
					fmt.Fprintln(cmd.OutOrStdout(), "Cancelled.")
					return nil
				}
			}

			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.Workspaces.TransferWorkspaceOwnership(cmd.Context(), &sdk.TransferWorkspaceOwnershipRequest{
				WorkspaceID: args[0],
				NewOwnerID:  newOwner,
			})
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Transferred workspace %s to %s\n", args[0], newOwner)
			return nil
		},
	}

	cmd.Flags().StringVar(&newOwner, "new-owner", "", "New owner user ID (required)")
	cmd.MarkFlagRequired("new-owner")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

// --- pin / unpin ---

func newWsPinCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pin <id>",
		Short: "Pin a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.Workspaces.UpdateWorkspacePin(cmd.Context(), &sdk.UpdateWorkspacePinRequest{
				WorkspaceID: args[0],
				Pinned:      true,
			})
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Pinned workspace %s\n", args[0])
			return nil
		},
	}
}

func newWsUnpinCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unpin <id>",
		Short: "Unpin a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.Workspaces.UpdateWorkspacePin(cmd.Context(), &sdk.UpdateWorkspacePinRequest{
				WorkspaceID: args[0],
				Pinned:      false,
			})
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Unpinned workspace %s\n", args[0])
			return nil
		},
	}
}
