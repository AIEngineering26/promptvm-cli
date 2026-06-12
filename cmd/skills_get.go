package cmd

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/AIEngineering26/promptvm-cli/internal/api"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newSkillsGetCmd() *cobra.Command {
	var raw bool

	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Show a skill",
		Long:  "Shows a skill's frontmatter summary and file manifest.\nUse --raw to print the literal SKILL.md to stdout.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			caller, err := api.NewFromContext(cmd)
			if err != nil {
				return err
			}

			var resp skillResponse
			if err := caller.Get("/api/v1/skills/"+args[0], &resp); err != nil {
				return err
			}
			d := resp.Data

			if raw {
				// Verbatim SKILL.md bytes — no decoration, no trailing newline.
				_, err := fmt.Fprint(cmd.OutOrStdout(), d.RawSkillMD)
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}

			printField(cmd, "ID", d.ID)
			printField(cmd, "Slug", d.Slug)
			printField(cmd, "Name", d.Name)
			printField(cmd, "Description", d.Description)
			printField(cmd, "When to use", d.WhenToUse)
			printField(cmd, "Status", d.Status)
			printField(cmd, "Public", d.IsPublic)
			printField(cmd, "Workspace", d.WorkspaceID)
			if len(d.Tags) > 0 {
				printField(cmd, "Tags", strings.Join(d.Tags, ", "))
			}
			printField(cmd, "Updated", humanTimePtr(d.UpdatedAt))

			w := cmd.OutOrStdout()
			if len(d.Files) == 0 {
				fmt.Fprintln(w, "\nNo bundled files.")
				return nil
			}
			fmt.Fprintf(w, "\nFiles (%d):\n", len(d.Files))
			return output.Table(w, []string{"PATH", "SIZE", "TYPE", "RESOURCE"}, func(tw *tabwriter.Writer) {
				for _, f := range d.Files {
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
						f.Path, resHumanBytes(f.SizeBytes), f.MimeType, f.ResourceID)
				}
			})
		},
	}

	cmd.Flags().BoolVar(&raw, "raw", false, "Print the literal SKILL.md to stdout")

	return cmd
}

func init() {
	skillsCmd.AddCommand(newSkillsGetCmd())
}
