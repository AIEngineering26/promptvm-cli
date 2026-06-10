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

func newPromptsReferencesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "references <prompt-id>",
		Short: "List prompt references",
		Long:  "Returns all [[include:]] references in the current version.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.PromptOrganization.ListPromptReferences(cmd.Context(), &sdk.ListPromptReferencesRequest{
				PromptID: args[0],
			})
			if err != nil {
				return err
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				return output.Table(w, []string{"ID", "ALIAS", "TYPE", "REFERENCE_ID"}, func(tw *tabwriter.Writer) {
					for _, r := range resp.Data {
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
							r.GetID(),
							r.GetAlias(),
							r.GetReferenceType(),
							r.GetReferenceID(),
						)
					}
				})
			})
		},
	}
	return cmd
}

func init() {
	promptsCmd.AddCommand(newPromptsReferencesCmd())
}
