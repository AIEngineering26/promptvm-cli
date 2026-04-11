package cmd

import (
	"fmt"
	"io"
	"strings"

	sdk "github.com/AIEngineering26/promptvm-go-sdk"
	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/spf13/cobra"
)

var listingsCmd = &cobra.Command{
	Use:   "listings",
	Short: "Manage marketplace listings",
}

func init() {
	marketplaceCmd.AddCommand(listingsCmd)
	listingsCmd.AddCommand(newListingsGetCmd())
	listingsCmd.AddCommand(newListingsCreateCmd())
	listingsCmd.AddCommand(newListingsUpdateCmd())
	listingsCmd.AddCommand(newListingsDeleteCmd())
	listingsCmd.AddCommand(newListingsClaimCmd())
}

func newListingsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get listing details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.MarketplaceBrowse.GetMarketplaceListing(cmd.Context(), &sdk.GetMarketplaceListingRequest{
				ListingID: args[0],
			})
			if err != nil {
				return err
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				d := resp.GetData()
				if d == nil {
					fmt.Fprintln(w, "Listing not found.")
					return nil
				}

				printField(cmd, "ID", derefStr(d.ID))
				printField(cmd, "Title", derefStr(d.Title))
				printField(cmd, "Description", derefStr(d.Description))
				if d.Status != nil {
					printField(cmd, "Status", string(*d.Status))
				}
				printField(cmd, "Price", fmtPriceCentsPtr(d.PriceCents))

				if d.AvgRating != nil {
					count := 0
					if d.RatingCount != nil {
						count = *d.RatingCount
					}
					printField(cmd, "Rating", fmt.Sprintf("%s★ (%d ratings)", *d.AvgRating, count))
				}
				if d.PurchaseCount != nil {
					printField(cmd, "Downloads", fmt.Sprintf("%d", *d.PurchaseCount))
				}
				if len(d.Categories) > 0 {
					names := make([]string, len(d.Categories))
					for i, cat := range d.Categories {
						names[i] = cat.Name
					}
					printField(cmd, "Categories", strings.Join(names, ", "))
				}
				if len(d.Tags) > 0 {
					printField(cmd, "Tags", strings.Join(d.Tags, ", "))
				}
				if d.Seller != nil && d.Seller.UserID != nil {
					printField(cmd, "Seller", *d.Seller.UserID)
				}
				if d.CreatedAt != nil {
					printField(cmd, "Created", d.CreatedAt.Format("2006-01-02"))
				}
				if d.UpdatedAt != nil {
					printField(cmd, "Updated", humanTime(*d.UpdatedAt))
				}
				return nil
			})
		},
	}
}

func newListingsCreateCmd() *cobra.Command {
	var (
		promptID    string
		collID      string
		name        string
		description string
		categoryIDs []string
		tags        []string
		price       string
		accessType  string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a listing from a prompt or collection",
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" || description == "" {
				return fmt.Errorf("--name and --description are required")
			}

			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.CreateMarketplaceListingRequest{
				Title:       name,
				Description: description,
			}
			if promptID != "" {
				req.PromptID = &promptID
			}
			if collID != "" {
				req.CollectionID = &collID
			}
			if len(categoryIDs) > 0 {
				req.CategoryIDs = categoryIDs
			}
			if len(tags) > 0 {
				req.Tags = tags
			}
			if accessType != "" {
				at := sdk.CreateMarketplaceListingRequestAccessType(accessType)
				req.AccessType = &at
			}
			if price != "free" && price != "" {
				var cents int
				if _, err := fmt.Sscanf(price, "%d", &cents); err != nil {
					return fmt.Errorf("invalid --price %q: expected integer cents or \"free\"", price)
				}
				req.PriceCents = &cents
			}

			resp, err := c.MarketplaceListings.CreateMarketplaceListing(cmd.Context(), req)
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created listing %s %q\n", resp.Data.ID, resp.Data.Title)
			return nil
		},
	}

	cmd.Flags().StringVar(&promptID, "prompt", "", "Source prompt ID")
	cmd.Flags().StringVar(&collID, "collection", "", "Source collection ID")
	cmd.Flags().StringVar(&name, "name", "", "Listing title (required)")
	cmd.Flags().StringVar(&description, "description", "", "Listing description (required)")
	cmd.Flags().StringSliceVar(&categoryIDs, "category-ids", nil, "Category IDs")
	cmd.Flags().StringSliceVar(&tags, "tags", nil, "Tags (comma-separated)")
	cmd.Flags().StringVar(&price, "price", "free", "Price: free or cents amount")
	cmd.Flags().StringVar(&accessType, "access-type", "", "Access type: snapshot or living")
	return cmd
}

func newListingsUpdateCmd() *cobra.Command {
	var (
		name        string
		description string
		categoryIDs []string
		tags        []string
		status      string
		priceCents  int
		accessType  string
	)

	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a listing",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.UpdateMarketplaceListingRequest{ListingID: args[0]}
			if name != "" {
				req.Title = &name
			}
			if description != "" {
				req.Description = &description
			}
			if len(categoryIDs) > 0 {
				req.CategoryIDs = categoryIDs
			}
			if len(tags) > 0 {
				req.Tags = tags
			}
			if status != "" {
				s := sdk.UpdateMarketplaceListingRequestStatus(status)
				req.Status = &s
			}
			if cmd.Flags().Changed("price-cents") {
				req.PriceCents = &priceCents
			}
			if accessType != "" {
				at := sdk.UpdateMarketplaceListingRequestAccessType(accessType)
				req.AccessType = &at
			}

			resp, err := c.MarketplaceListings.UpdateMarketplaceListing(cmd.Context(), req)
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Updated listing %s\n", args[0])
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "New title")
	cmd.Flags().StringVar(&description, "description", "", "New description")
	cmd.Flags().StringSliceVar(&categoryIDs, "category-ids", nil, "Category IDs")
	cmd.Flags().StringSliceVar(&tags, "tags", nil, "Tags")
	cmd.Flags().StringVar(&status, "status", "", "Status: draft, active, inactive")
	cmd.Flags().IntVar(&priceCents, "price-cents", 0, "Price in cents")
	cmd.Flags().StringVar(&accessType, "access-type", "", "Access type: snapshot or living")
	return cmd
}

func newListingsDeleteCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Archive/delete a listing",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				if !output.Confirm(fmt.Sprintf("Archive listing %s?", args[0])) {
					fmt.Fprintln(cmd.OutOrStdout(), "Cancelled.")
					return nil
				}
			}

			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			if err := c.MarketplaceListings.ArchiveMarketplaceListing(cmd.Context(), &sdk.ArchiveMarketplaceListingRequest{
				ListingID: args[0],
			}); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Archived listing %s\n", args[0])
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func newListingsClaimCmd() *cobra.Command {
	var workspace string

	cmd := &cobra.Command{
		Use:   "claim <id>",
		Short: "Claim a free listing and import to workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			wsID, err := resolveWorkspaceForClaim(cmd, workspace)
			if err != nil {
				return err
			}

			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.MarketplaceListings.ClaimMarketplaceListing(cmd.Context(), &sdk.ClaimMarketplaceListingRequest{
				ListingID:   args[0],
				WorkspaceID: wsID,
			})
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Claimed listing %s\n", args[0])
			if resp.Data.ImportedPromptID != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Imported prompt: %s\n", *resp.Data.ImportedPromptID)
			}
			if resp.Data.ImportedCollectionID != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Imported collection: %s\n", *resp.Data.ImportedCollectionID)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "Target workspace ID (required)")
	return cmd
}

// resolveWorkspaceForClaim tries --workspace flag, then default workspace from config.
func resolveWorkspaceForClaim(cmd *cobra.Command, flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	return resolveDefaultWorkspace(cmd)
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func fmtPriceCentsPtr(cents *int) string {
	if cents == nil || *cents <= 0 {
		return "Free"
	}
	return fmt.Sprintf("$%.2f", float64(*cents)/100)
}
