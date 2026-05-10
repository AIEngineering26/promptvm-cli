package cmd

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/spf13/cobra"
)

var contextsCmd = &cobra.Command{
	Use:   "contexts",
	Short: "Inspect supported context kinds",
	Long: `Inspect the catalogue of context kinds the platform stores.

Context kinds are the typed slots PromptVM accepts for stored content. The
two stable kinds today are:

  prompt   — a reusable LLM prompt with versions, variables, and resolution
             semantics. Default is private.
  skill    — a packaged agent skill (instructions + assets) the platform can
             distribute. Default is public.

The catalogue is global and stable: agents can build typed adapters from
this response without consulting the docs.`,
}

func init() {
	rootCmd.AddCommand(contextsCmd)
	contextsCmd.AddCommand(newContextsListCmd())
}

func newContextsListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List supported context kinds",
		Long: `Calls GET /v1/contexts/kinds and renders the catalogue.

Default output is a table with name, defaultIsPublic, and a truncated
description. Use -o json or -o yaml to get the full payload (including the
metadata, content, and file specs) for scripting.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.Contexts.ListContextKinds(cmd.Context())
			if err != nil {
				return err
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				return output.Table(w, []string{"NAME", "DEFAULT_PUBLIC", "DESCRIPTION"}, func(tw *tabwriter.Writer) {
					for _, k := range resp.GetKinds() {
						fmt.Fprintf(tw, "%s\t%t\t%s\n",
							string(k.GetName()),
							k.GetDefaultIsPublic(),
							truncate(k.GetDescription(), 80),
						)
					}
				})
			})
		},
	}
	return cmd
}
