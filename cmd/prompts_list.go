package cmd

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	sdk "github.com/AIEngineering26/promptvm-go-sdk"
	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newPromptsListCmd() *cobra.Command {
	var (
		workspace string
		limit     string
		cursor    string
		search    string
		status    string
		kind      string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List prompts",
		Long:  "Returns a paginated list of prompts. Requires a workspace ID.",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.ListPromptsRequest{
				WorkspaceID: workspace,
			}
			if limit != "" {
				req.Limit = &limit
			}
			if cursor != "" {
				req.Cursor = &cursor
			}
			if search != "" {
				req.Search = &search
			}
			if status != "" {
				s, err := sdk.NewListPromptsRequestStatusFromString(status)
				if err != nil {
					return err
				}
				req.Status = &s
			}
			if kind != "" {
				k, err := sdk.NewListPromptsRequestKindFromString(kind)
				if err != nil {
					return err
				}
				req.Kind = &k
			}

			resp, err := c.Prompts.ListPrompts(cmd.Context(), req)
			if err != nil {
				return err
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				return output.Table(w, []string{"ID", "NAME", "STATUS", "KIND", "UPDATED"}, func(tw *tabwriter.Writer) {
					for _, p := range resp.Data {
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
							p.GetID(),
							p.GetName(),
							p.GetStatus(),
							p.GetKind(),
							humanTime(p.GetUpdatedAt()),
						)
					}
				})
			})
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace ID (required)")
	cmd.MarkFlagRequired("workspace")
	cmd.Flags().StringVarP(&limit, "limit", "l", "", "Max results per page")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor")
	cmd.Flags().StringVarP(&search, "search", "s", "", "Search by name")
	cmd.Flags().StringVar(&status, "status", "", "Filter by status (draft|published)")
	cmd.Flags().StringVar(&kind, "kind", "", "Filter by kind (template|instance)")

	return cmd
}

func humanTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func init() {
	promptsCmd.AddCommand(newPromptsListCmd())
}
