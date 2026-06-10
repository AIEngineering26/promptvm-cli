package cmd

import (
	"fmt"

	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	sdk "github.com/AIEngineering26/promptvm-go-sdk"
	"github.com/spf13/cobra"
)

func newPromptsDeleteCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "delete <prompt-id>",
		Short: "Delete a prompt",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				if !output.Confirm(fmt.Sprintf("Delete prompt %s?", args[0])) {
					fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
			}

			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.Prompts.DeletePrompt(cmd.Context(), &sdk.DeletePromptRequest{
				PromptID: args[0],
			})
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Deleted prompt %s\n", args[0])
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation")

	return cmd
}

func init() {
	promptsCmd.AddCommand(newPromptsDeleteCmd())
}
