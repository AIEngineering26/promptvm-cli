package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	sdk "github.com/AIEngineering26/promptvm-go-sdk"
	"github.com/spf13/cobra"
)

func newPromptsGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <prompt-id>",
		Short: "Get prompt details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.Prompts.GetPrompt(cmd.Context(), &sdk.GetPromptRequest{
				PromptID: args[0],
			})
			if err != nil {
				return err
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				d := resp.GetData()
				fmt.Fprintf(w, "Name:        %s\n", d.GetName())
				fmt.Fprintf(w, "ID:          %s\n", d.GetID())
				fmt.Fprintf(w, "Status:      %s\n", d.GetStatus())
				fmt.Fprintf(w, "Kind:        %s\n", d.GetKind())
				fmt.Fprintf(w, "Workspace:   %s\n", d.GetWorkspaceID())
				fmt.Fprintf(w, "Created:     %s\n", d.GetCreatedAt().Format("2006-01-02"))
				fmt.Fprintf(w, "Updated:     %s\n", humanTime(d.GetUpdatedAt()))

				if tags := d.GetTags(); len(tags) > 0 {
					fmt.Fprintf(w, "Tags:        %s\n", strings.Join(tags, ", "))
				}

				if cv := d.GetCurrentVersion(); cv != nil {
					if n := cv.GetVersionNumber(); n != nil {
						fmt.Fprintf(w, "Version:     v%d\n", *n)
					}
					if c := cv.GetContent(); c != nil {
						fmt.Fprintf(w, "\nContent:\n---\n%s\n---\n", *c)
					}
				}
				return nil
			})
		},
	}
	return cmd
}

func init() {
	promptsCmd.AddCommand(newPromptsGetCmd())
}
