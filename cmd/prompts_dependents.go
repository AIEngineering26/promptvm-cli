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

func newPromptsDependentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dependents <prompt-id>",
		Short: "List prompt dependents",
		Long:  "Returns all prompts that reference this one.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.PromptOrganization.ListPromptDependents(cmd.Context(), &sdk.ListPromptDependentsRequest{
				PromptID: args[0],
			})
			if err != nil {
				return err
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				return output.Table(w, []string{"PROMPT_ID", "PROMPT_NAME", "VERSION", "ALIAS"}, func(tw *tabwriter.Writer) {
					for _, d := range resp.Data {
						fmt.Fprintf(tw, "%s\t%s\tv%d\t%s\n",
							d.GetPromptID(),
							d.GetPromptName(),
							d.GetVersionNumber(),
							d.GetAlias(),
						)
					}
				})
			})
		},
	}
	return cmd
}

func init() {
	promptsCmd.AddCommand(newPromptsDependentsCmd())
}
