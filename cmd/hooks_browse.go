package cmd

import (
	"fmt"
	"io"
	"net/url"
	"strings"
	"text/tabwriter"

	"github.com/AIEngineering26/promptvm-cli/internal/api"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/spf13/cobra"
)

// hooksBrowseItem is the JSON shape returned by GET /api/v1/hooks.
type hooksBrowseItem struct {
	ID           string   `json:"id"`
	Slug         string   `json:"slug"`
	Name         string   `json:"name"`
	Events       []string `json:"events"`
	HandlerTypes []string `json:"handler_types"`
	Tags         []string `json:"tags"`
	Version      int      `json:"version"`
	IsPublic     bool     `json:"isPublic"`
}

type hooksBrowseResponse struct {
	Data       []hooksBrowseItem `json:"data"`
	Pagination struct {
		Cursor  string `json:"cursor"`
		HasMore bool   `json:"hasMore"`
	} `json:"pagination"`
}

func newHooksBrowseCmd() *cobra.Command {
	var (
		workspace string
		event     string
		limit     string
		public    bool
	)

	cmd := &cobra.Command{
		Use:   "browse",
		Short: "Browse available hooks",
		Long:  "List hooks available in your workspace from the PromptVM registry.",
		RunE: func(cmd *cobra.Command, args []string) error {
			caller, err := api.NewFromContext(cmd)
			if err != nil {
				return err
			}

			params := url.Values{}
			params.Set("workspaceId", workspace)
			if event != "" {
				params.Set("event", event)
			}
			if limit != "" {
				params.Set("limit", limit)
			}
			if public {
				params.Set("public", "true")
			}

			path := "/api/v1/hooks?" + params.Encode()

			var resp hooksBrowseResponse
			if err := caller.Get(path, &resp); err != nil {
				return err
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				if len(resp.Data) == 0 {
					fmt.Fprintln(w, "No hooks found.")
					return nil
				}
				return output.Table(w, []string{"SLUG", "NAME", "EVENTS", "HANDLER_TYPES", "TAGS"}, func(tw *tabwriter.Writer) {
					for _, h := range resp.Data {
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
							h.Slug,
							h.Name,
							strings.Join(h.Events, ", "),
							strings.Join(h.HandlerTypes, ", "),
							strings.Join(h.Tags, ", "),
						)
					}
				})
			})
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace ID (required)")
	cmd.MarkFlagRequired("workspace")
	cmd.Flags().StringVar(&event, "event", "", "Filter by event (e.g. PreToolUse)")
	cmd.Flags().StringVarP(&limit, "limit", "l", "", "Max results per page")
	cmd.Flags().BoolVar(&public, "public", false, "Show only public hooks")

	return cmd
}

func init() {
	hooksCmd.AddCommand(newHooksBrowseCmd())
}
