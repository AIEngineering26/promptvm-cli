package cmd

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/spf13/cobra"
)

// The catalog of models a resource or listing can be recommended for — the
// suggested-models vocabulary.
//
// Rows lead with `provider/slug` rather than the id. That is the portable form:
// model ids are gen_random_uuid() per environment, so a uuid copied out of here
// into a script stops working the moment it runs somewhere else. Every command
// that takes a model accepts either, and the API resolves both.
func newMarketplaceModelsCmd() *cobra.Command {
	var (
		provider string
		modality string
	)

	cmd := &cobra.Command{
		Use:     "models",
		Aliases: []string{"ai-models"},
		Short:   "List the models resources can be recommended for",
		Long: "Lists the active suggested-models catalog, grouped by provider.\n\n" +
			"The SLUG column is what --models flags and --model filters accept.",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.MarketplaceBrowse.ListMarketplaceAiModels(cmd.Context())
			if err != nil {
				return err
			}

			type row struct{ slug, name, provider, modality string }
			var rows []row
			for _, p := range resp.GetData() {
				if provider != "" && !strings.EqualFold(p.Slug, provider) {
					continue
				}
				for _, m := range p.Models {
					if modality != "" && !strings.EqualFold(m.Modality, modality) {
						continue
					}
					rows = append(rows, row{
						slug:     p.Slug + "/" + m.Slug,
						name:     m.Name,
						provider: p.Name,
						modality: m.Modality,
					})
				}
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}

			if len(rows) == 0 {
				// Say which filter emptied it — an unfiltered empty catalog and
				// a filter that matched nothing are different problems.
				if provider != "" || modality != "" {
					fmt.Fprintln(cmd.OutOrStdout(), "No models match that filter.")
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "No models available.")
				}
				return nil
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				return output.Table(w, []string{"SLUG", "NAME", "PROVIDER", "MODALITY"}, func(tw *tabwriter.Writer) {
					for _, r := range rows {
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.slug, r.name, r.provider, r.modality)
					}
				})
			})
		},
	}

	cmd.Flags().StringVar(&provider, "provider", "", "Only this provider (slug, e.g. anthropic)")
	cmd.Flags().StringVar(&modality, "modality", "", "Only this modality: text, image, video, audio")
	return cmd
}

func init() {
	marketplaceCmd.AddCommand(newMarketplaceModelsCmd())
}
