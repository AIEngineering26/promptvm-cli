package cmd

import (
	"fmt"
	"io"

	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	sdk "github.com/AIEngineering26/promptvm-go-sdk"
	"github.com/spf13/cobra"
)

func newPromptsForkCmd() *cobra.Command {
	var (
		workspace string
		name      string
	)

	cmd := &cobra.Command{
		Use:   "fork <prompt-id>",
		Short: "Fork a prompt",
		Long:  "Creates a copy of a prompt in the specified workspace.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.ForkPromptRequest{
				PromptID:    args[0],
				WorkspaceID: workspace,
			}
			if name != "" {
				req.Name = &name
			}

			resp, err := c.PromptOrganization.ForkPrompt(cmd.Context(), req)
			if err != nil {
				return err
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				d := resp.GetData()
				fmt.Fprintf(w, "Forked prompt %s → %s %q\n", args[0], d.GetID(), d.GetName())
				return nil
			})
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "Target workspace ID (required)")
	cmd.MarkFlagRequired("workspace")
	cmd.Flags().StringVar(&name, "name", "", "Name for the forked prompt")

	return cmd
}

func init() {
	promptsCmd.AddCommand(newPromptsForkCmd())
}
