package cmd

import (
	"fmt"
	"io"

	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	sdk "github.com/AIEngineering26/promptvm-go-sdk"
	"github.com/spf13/cobra"
)

func newPromptsMoveCmd() *cobra.Command {
	var (
		workspace string
		directory string
	)

	cmd := &cobra.Command{
		Use:   "move <prompt-id>",
		Short: "Move prompt to different location",
		Long:  "Moves a prompt to a different directory and/or workspace.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.MovePromptRequest{
				PromptID: args[0],
			}
			if workspace != "" {
				req.WorkspaceID = &workspace
			}
			if directory != "" {
				req.DirectoryID = &directory
			}

			resp, err := c.PromptOrganization.MovePrompt(cmd.Context(), req)
			if err != nil {
				return err
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				d := resp.GetData()
				fmt.Fprintf(w, "Moved prompt %s to workspace %s\n", d.GetID(), d.GetWorkspaceID())
				return nil
			})
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "Target workspace ID")
	cmd.Flags().StringVar(&directory, "directory", "", "Target directory ID")

	return cmd
}

func init() {
	promptsCmd.AddCommand(newPromptsMoveCmd())
}
