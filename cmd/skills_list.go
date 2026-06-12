package cmd

import (
	"fmt"
	"io"
	"net/url"
	"text/tabwriter"

	"github.com/AIEngineering26/promptvm-cli/internal/api"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/spf13/cobra"
)

// skillsListResponse is the cursor-paginated shape of GET /api/v1/skills.
type skillsListResponse struct {
	Data       []skillDetail `json:"data"`
	Pagination struct {
		Cursor  string `json:"cursor"`
		HasMore bool   `json:"hasMore"`
	} `json:"pagination"`
}

func newSkillsListCmd() *cobra.Command {
	var (
		workspace string
		limit     string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List skills in a workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			wsID := workspace
			if wsID == "" {
				var err error
				wsID, err = resolveDefaultWorkspace(cmd)
				if err != nil {
					return err
				}
			}

			caller, err := api.NewFromContext(cmd)
			if err != nil {
				return err
			}

			// Follow cursor pagination until exhausted.
			var items []skillDetail
			cursor := ""
			for {
				params := url.Values{}
				params.Set("workspaceId", wsID)
				if limit != "" {
					params.Set("limit", limit)
				}
				if cursor != "" {
					params.Set("cursor", cursor)
				}

				var resp skillsListResponse
				if err := caller.Get("/api/v1/skills?"+params.Encode(), &resp); err != nil {
					return err
				}
				items = append(items, resp.Data...)
				if !resp.Pagination.HasMore || resp.Pagination.Cursor == "" {
					break
				}
				cursor = resp.Pagination.Cursor
			}

			payload := map[string]interface{}{"data": items}
			return output.Print(cmd, payload, func(w io.Writer) error {
				if len(items) == 0 {
					fmt.Fprintln(w, "No skills found.")
					return nil
				}
				return output.Table(w, []string{"NAME", "SLUG", "STATUS", "FILES", "UPDATED"}, func(tw *tabwriter.Writer) {
					for _, s := range items {
						fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n",
							s.Name, s.Slug, s.Status, len(s.Files), humanTimePtr(s.UpdatedAt))
					}
				})
			})
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace ID (default: config defaults.workspace)")
	cmd.Flags().StringVarP(&limit, "limit", "l", "", "Max results per page")

	return cmd
}

func init() {
	skillsCmd.AddCommand(newSkillsListCmd())
}
