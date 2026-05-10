package cmd

import (
	"fmt"
	"io"
	"text/tabwriter"

	sdk "github.com/AIEngineering26/promptvm-go-sdk"
	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newSearchCmd())
}

func newSearchCmd() *cobra.Command {
	var (
		workspace string
		kind      string
		limit     int
		wide      bool
	)

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search prompts and files across an organization",
		Long: `Search org-wide for prompts and files matching <query>.

Output columns: name, kind, workspace, score, id. The score is the SDK's
normalised relevance score from SearchOrganizationResponse.

Either --org or a profile-default organization must be set; without one
the command errors immediately rather than guessing.`,
		Example: `  promptvm search "support reply" --org org_abc
  promptvm search "embeddings" --kind prompt --limit 50
  promptvm search "onboarding" --workspace ws_123 -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]

			orgID, err := resolveOrgID(cmd, "org")
			if err != nil {
				return err
			}

			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.SearchOrganizationRequest{
				Q:              query,
				OrganizationID: orgID,
			}
			if workspace != "" {
				ws := workspace
				req.WorkspaceIDs = []*string{&ws}
			}
			if kind != "" {
				k, err := sdk.NewSearchOrganizationRequestKindsItemFromString(kind)
				if err != nil {
					return err
				}
				req.Kinds = []*sdk.SearchOrganizationRequestKindsItem{&k}
			}
			if limit > 0 {
				req.Limit = &limit
			}

			resp, err := c.Search.Organization(cmd.Context(), req)
			if err != nil {
				return err
			}

			titleWidth := 60
			if wide {
				titleWidth = 0 // 0 → no truncation
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				return output.Table(w, []string{"NAME", "KIND", "WORKSPACE", "SCORE", "ID"}, func(tw *tabwriter.Writer) {
					for _, r := range resp.GetResults() {
						title := r.GetTitle()
						if titleWidth > 0 {
							title = truncate(title, titleWidth)
						}
						fmt.Fprintf(tw, "%s\t%s\t%s\t%.4f\t%s\n",
							title,
							string(r.GetKind()),
							r.GetWorkspaceID(),
							r.GetScore(),
							r.GetID(),
						)
					}
				})
			})
		},
	}

	cmd.Flags().String("org", "", "Organization ID (overrides profile default)")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Filter to a single workspace ID")
	cmd.Flags().StringVar(&kind, "kind", "", "Filter by kind (prompt|file)")
	cmd.Flags().IntVarP(&limit, "limit", "l", 0, "Max number of results to return")
	cmd.Flags().BoolVar(&wide, "wide", false, "Print full result titles instead of truncating to 60 chars")

	return cmd
}
