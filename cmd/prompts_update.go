package cmd

import (
	"fmt"
	"strings"

	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	sdk "github.com/AIEngineering26/promptvm-go-sdk"
	"github.com/spf13/cobra"
)

func newPromptsUpdateCmd() *cobra.Command {
	var (
		name        string
		description string
		tags        string
		status      string
		isPublic    bool
		directory   string
	)

	cmd := &cobra.Command{
		Use:   "update <prompt-id>",
		Short: "Update prompt metadata",
		Long:  "Updates name, description, status, tags, or isPublic. Does not create a new version.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.UpdatePromptRequest{
				PromptID: args[0],
			}
			if name != "" {
				req.Name = &name
			}
			if description != "" {
				req.Description = &description
			}
			if tags != "" {
				req.Tags = strings.Split(tags, ",")
			}
			if status != "" {
				s, err := sdk.NewUpdatePromptRequestStatusFromString(status)
				if err != nil {
					return err
				}
				req.Status = &s
			}
			if cmd.Flags().Changed("public") {
				req.IsPublic = &isPublic
			}
			if directory != "" {
				req.DirectoryID = &directory
			}

			resp, err := c.Prompts.UpdatePrompt(cmd.Context(), req)
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Updated prompt %s\n", args[0])
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "New name")
	cmd.Flags().StringVar(&description, "description", "", "New description")
	cmd.Flags().StringVar(&tags, "tags", "", "Comma-separated tags")
	cmd.Flags().StringVar(&status, "status", "", "Status (draft|published)")
	cmd.Flags().BoolVar(&isPublic, "public", false, "Make prompt public")
	cmd.Flags().StringVar(&directory, "directory", "", "New directory ID")

	return cmd
}

func init() {
	promptsCmd.AddCommand(newPromptsUpdateCmd())
}
