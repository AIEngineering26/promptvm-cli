package cmd

import (
	"fmt"
	"strings"

	sdk "github.com/AIEngineering26/promptvm-go-sdk"
	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/ioutil"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newPromptsCreateCmd() *cobra.Command {
	var (
		name        string
		workspace   string
		description string
		tags        string
		directory   string
		kind        string
		status      string
		isPublic    bool
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new prompt",
		Long:  "Creates a new prompt with an initial version (v1).",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			content, err := ioutil.ReadContent(cmd)
			if err != nil {
				return err
			}

			req := &sdk.CreatePromptRequest{
				Name:        name,
				WorkspaceID: workspace,
				Content:     content,
			}
			if description != "" {
				req.Description = &description
			}
			if tags != "" {
				req.Tags = strings.Split(tags, ",")
			}
			if directory != "" {
				req.DirectoryID = &directory
			}
			if kind != "" {
				k, err := sdk.NewCreatePromptRequestKindFromString(kind)
				if err != nil {
					return err
				}
				req.Kind = &k
			}
			if status != "" {
				s, err := sdk.NewCreatePromptRequestStatusFromString(status)
				if err != nil {
					return err
				}
				req.Status = &s
			}
			if cmd.Flags().Changed("public") {
				req.IsPublic = &isPublic
			}

			resp, err := c.Prompts.CreatePrompt(cmd.Context(), req)
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}

			d := resp.GetData()
			vn := ""
			if cv := d.GetCurrentVersion(); cv != nil {
				if n := cv.GetVersionNumber(); n != nil {
					vn = fmt.Sprintf(" (v%d)", *n)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created prompt %s %q%s\n", d.GetID(), d.GetName(), vn)
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Prompt name (required)")
	cmd.MarkFlagRequired("name")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Target workspace ID (required)")
	cmd.MarkFlagRequired("workspace")
	cmd.Flags().String("content", "", "Prompt content (inline)")
	cmd.Flags().StringP("file", "f", "", "Read content from file (use - for stdin)")
	cmd.Flags().StringVar(&description, "description", "", "Prompt description")
	cmd.Flags().StringVar(&tags, "tags", "", "Comma-separated tags")
	cmd.Flags().StringVar(&directory, "directory", "", "Target directory ID")
	cmd.Flags().StringVar(&kind, "kind", "", "Prompt kind (template|instance)")
	cmd.Flags().StringVar(&status, "status", "", "Initial status (draft|published)")
	cmd.Flags().BoolVar(&isPublic, "public", false, "Make prompt public")

	return cmd
}

func init() {
	promptsCmd.AddCommand(newPromptsCreateCmd())
}
