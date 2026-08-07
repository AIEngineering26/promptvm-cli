package cmd

import (
	"context"
	"fmt"
	"net/url"

	"github.com/AIEngineering26/promptvm-cli/internal/api"
	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	promptvmgosdk "github.com/AIEngineering26/promptvm-go-sdk"
	"github.com/spf13/cobra"
)

// orphanResourceRow is the subset of the workspace resource list response we
// need for cleanup — id + name are enough to list, confirm, and delete.
type orphanResourceRow struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type orphanListResponse struct {
	Data []orphanResourceRow `json:"data"`
}

func newWsCleanupOrphansCmd() *cobra.Command {
	var (
		workspace string
		yes       bool
	)

	cmd := &cobra.Command{
		Use:   "cleanup-orphans",
		Short: "Delete workspace resources not bound to any prompt or skill (dry-run by default)",
		Long: "Lists resources in the workspace that are not bundled into any skill\n" +
			"version and not attached to any prompt — the leftovers from failed\n" +
			"CLI skill uploads that clutter the workspace root. Without --yes this\n" +
			"is a dry-run; the resources are only printed. With --yes each one is\n" +
			"deleted through the standard DELETE /api/v1/resources/:id endpoint.\n" +
			"\n" +
			"Requires the backend to support the `orphansOnly` filter (added in\n" +
			"backend PR #200); on older backends the endpoint silently ignores the\n" +
			"param and would return every workspace resource. As a safeguard,\n" +
			"cleanup-orphans REFUSES to delete when the count exceeds --max\n" +
			"(default 500) — if you legitimately have that many orphans, re-run\n" +
			"with --max <n> after eyeballing the dry-run output.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if workspace == "" {
				var err error
				workspace, err = resolveDefaultWorkspace(cmd)
				if err != nil {
					return err
				}
			}

			caller, err := api.NewFromContext(cmd)
			if err != nil {
				return err
			}
			sdk, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			params := url.Values{}
			params.Set("workspaceId", workspace)
			params.Set("orphansOnly", "true")

			var resp orphanListResponse
			if err := caller.Get("/api/v1/resources?"+params.Encode(), &resp); err != nil {
				return fmt.Errorf("listing orphan resources: %w", err)
			}

			max, _ := cmd.Flags().GetInt("max")
			if len(resp.Data) > max {
				return fmt.Errorf(
					"refusing to proceed: found %d orphan resources, above --max=%d.\n"+
						"Re-run with --max %d after reviewing the dry-run list.",
					len(resp.Data), max, len(resp.Data))
			}

			if len(resp.Data) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No orphan resources found.")
				return nil
			}

			// Always show the list first — even under --yes — so the operator
			// sees what will be deleted before any DELETE fires.
			for _, r := range resp.Data {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s  %s\n", r.ID, r.Name)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\n%d orphan resource(s) in workspace %s.\n", len(resp.Data), workspace)

			if !yes {
				fmt.Fprintln(cmd.OutOrStdout(), "Dry-run. Re-run with --yes to delete them.")
				return nil
			}

			ctx := context.Background()
			deleted, failed := 0, 0
			for _, r := range resp.Data {
				if err := sdk.Resources.DeleteResource(ctx, &promptvmgosdk.DeleteResourceRequest{ResourceID: r.ID}); err != nil {
					failed++
					fmt.Fprintf(cmd.ErrOrStderr(), "  delete %s (%s) failed: %v\n", r.ID, r.Name, err)
					continue
				}
				deleted++
			}

			fmt.Fprintf(cmd.OutOrStdout(), "\nDeleted %d, failed %d.\n", deleted, failed)
			if failed > 0 && output.Format(cmd) == "table" {
				return fmt.Errorf("%d orphan resource(s) could not be deleted; see stderr for details", failed)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace ID (default: config defaults.workspace)")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Actually delete (default: dry-run)")
	cmd.Flags().Int("max", 500, "Refuse to run when more than this many orphans are found (safety net against an old backend that ignores orphansOnly)")
	return cmd
}

func init() {
	workspacesCmd.AddCommand(newWsCleanupOrphansCmd())
}
