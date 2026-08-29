package cmd

import (
	"fmt"

	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	sdk "github.com/AIEngineering26/promptvm-go-sdk"
	"github.com/spf13/cobra"
)

// Suggested models on a LISTING, which is a different thing from the resource's.
//
// A listing inherits the resource version's models when it is first published
// and owns them from then on: editing the resource afterwards never rewrites
// what a live listing claims publicly. These commands edit that pinned copy.
//
// Note there is no --models flag on `listings create`. First activation copies
// the version's models in with ON CONFLICT DO NOTHING, so anything set before
// then would be merged with rather than replaced by the inherited set. Setting
// them after publish is the only order with one obvious result.
var listingModelsCmd = &cobra.Command{
	Use:     "models",
	Aliases: []string{"use-with"},
	Short:   "Manage the models a listing is recommended for",
	Long: "A listing's models are copied from the resource version when it is\n" +
		"first published, and are the listing's own from then on.\n\n" +
		"Requires a logged-in session (`promptvm auth login`): listing edits are\n" +
		"user actions and do not accept API keys.",
}

func init() {
	listingsCmd.AddCommand(listingModelsCmd)
	listingModelsCmd.AddCommand(newListingModelsSetCmd())
	listingModelsCmd.AddCommand(newListingModelsClearCmd())
}

func newListingModelsSetCmd() *cobra.Command {
	var models []string

	cmd := &cobra.Command{
		Use:   "set <listing-id>",
		Short: "Replace the models a listing is recommended for",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			refs := splitModelRefs(models)
			if len(refs) == 0 {
				return fmt.Errorf("--models is required; use `models clear` to remove them all")
			}
			if err := validateModelRefs(refs); err != nil {
				return err
			}

			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.Marketplace.SetListingRecommendedModels(cmd.Context(),
				&sdk.SetListingRecommendedModelsRequest{ListingID: args[0], ModelIDs: refs})
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}
			var rows []struct{ Slug, Name, Provider string }
			for _, m := range resp.GetData() {
				rows = append(rows, struct{ Slug, Name, Provider string }{
					m.ProviderSlug + "/" + m.Slug, m.Name, m.ProviderName,
				})
			}
			return printModelTable(cmd, resp, rows)
		},
	}
	cmd.Flags().StringSliceVar(&models, "models", nil, "Models as provider/slug or id (repeatable or comma-separated)")
	return cmd
}

func newListingModelsClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clear <listing-id>",
		Short: "Remove every recommended model from a listing",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}
			if _, err := c.Marketplace.SetListingRecommendedModels(cmd.Context(),
				&sdk.SetListingRecommendedModelsRequest{ListingID: args[0], ModelIDs: []string{}}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Cleared recommended models on listing %s\n", args[0])
			return nil
		},
	}
}
