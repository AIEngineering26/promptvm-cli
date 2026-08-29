package cmd

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/ioutil"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	sdk "github.com/AIEngineering26/promptvm-go-sdk"
	"github.com/spf13/cobra"
)

var versionsCmd = &cobra.Command{
	Use:   "versions",
	Short: "Manage prompt versions",
}

func newVersionsListCmd() *cobra.Command {
	var (
		limit  string
		cursor string
	)

	cmd := &cobra.Command{
		Use:   "list <prompt-id>",
		Short: "List versions of a prompt",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.ListPromptVersionsRequest{
				PromptID: args[0],
			}
			if limit != "" {
				req.Limit = &limit
			}
			if cursor != "" {
				req.Cursor = &cursor
			}

			resp, err := c.PromptVersions.ListPromptVersions(cmd.Context(), req)
			if err != nil {
				return err
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				return output.Table(w, []string{"VERSION", "ID", "CREATED", "CHANGE_NOTE"}, func(tw *tabwriter.Writer) {
					for _, v := range resp.Data {
						note := ""
						if cn := v.GetChangeNote(); cn != nil {
							note = *cn
						}
						fmt.Fprintf(tw, "v%d\t%s\t%s\t%s\n",
							v.GetVersionNumber(),
							v.GetID(),
							humanTime(v.GetCreatedAt()),
							note,
						)
					}
				})
			})
		},
	}

	cmd.Flags().StringVarP(&limit, "limit", "l", "", "Max results per page")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor")

	return cmd
}

func newVersionsGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <prompt-id> <version-id>",
		Short: "Get a specific version",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.PromptVersions.GetPromptVersion(cmd.Context(), &sdk.GetPromptVersionRequest{
				PromptID:  args[0],
				VersionID: args[1],
			})
			if err != nil {
				return err
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				d := resp.GetData()
				fmt.Fprintf(w, "Version:     v%d\n", d.GetVersionNumber())
				fmt.Fprintf(w, "ID:          %s\n", d.GetID())
				fmt.Fprintf(w, "Prompt:      %s\n", d.GetPromptID())
				fmt.Fprintf(w, "Created:     %s\n", humanTime(d.GetCreatedAt()))
				fmt.Fprintf(w, "Current:     %t\n", d.GetIsCurrentVersion())

				if cn := d.GetChangeNote(); cn != nil {
					fmt.Fprintf(w, "Change note: %s\n", *cn)
				}
				if vl := d.GetVersionLabel(); vl != nil {
					fmt.Fprintf(w, "Label:       %s\n", *vl)
				}

				fmt.Fprintf(w, "\nContent:\n---\n%s\n---\n", d.GetContent())
				return nil
			})
		},
	}
	return cmd
}

func newVersionsCreateCmd() *cobra.Command {
	var (
		message string
		label   string
	)

	var models []string

	cmd := &cobra.Command{
		Use:   "create <prompt-id>",
		Short: "Create a new version",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			content, err := ioutil.ReadContent(cmd)
			if err != nil {
				return err
			}

			req := &sdk.CreatePromptVersionRequest{
				PromptID: args[0],
				Content:  content,
			}
			if message != "" {
				req.ChangeNote = &message
			}
			if label != "" {
				req.VersionLabel = &label
			}
			// Omitted, this inherits the previous version's models; --models
			// replaces them. One call rather than create-then-set, so a failure
			// cannot leave a version whose models were never applied.
			if cmd.Flags().Changed("models") {
				req.ModelIDs = splitModelRefs(models)
			}

			resp, err := c.PromptVersions.CreatePromptVersion(cmd.Context(), req)
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}

			d := resp.GetData()
			fmt.Fprintf(cmd.OutOrStdout(), "Created version v%d for prompt %s\n", d.GetVersionNumber(), d.GetPromptID())
			// Echo what the models resolved to — the caller may have named them
			// by slug, and an omitted flag inherits a set they cannot see.
			if models := d.GetRecommendedModels(); len(models) > 0 {
				names := make([]string, 0, len(models))
				for _, m := range models {
					names = append(names, m.ProviderSlug+"/"+m.Slug)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Use with:  %s\n", strings.Join(names, ", "))
			}
			return nil
		},
		Args: cobra.ExactArgs(1),
	}

	cmd.Flags().String("content", "", "New version content (inline)")
	cmd.Flags().StringP("file", "f", "", "Read content from file (use - for stdin)")
	cmd.Flags().StringVarP(&message, "message", "m", "", "Version change note")
	cmd.Flags().StringVar(&label, "label", "", "Version label")
	cmd.Flags().StringSliceVar(&models, "models", nil, "Models this version is for, as provider/slug or id (default: inherit the previous version's)")

	return cmd
}

func init() {
	versionsCmd.AddCommand(newVersionsListCmd())
	versionsCmd.AddCommand(newVersionsGetCmd())
	versionsCmd.AddCommand(newVersionsCreateCmd())
	promptsCmd.AddCommand(versionsCmd)
}
