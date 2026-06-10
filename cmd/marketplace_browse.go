package cmd

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	sdk "github.com/AIEngineering26/promptvm-go-sdk"
	"github.com/spf13/cobra"
)

var browseCmd = &cobra.Command{
	Use:   "browse",
	Short: "Browse and search marketplace listings",
}

func init() {
	marketplaceCmd.AddCommand(browseCmd)
	browseCmd.AddCommand(newBrowseSearchCmd())
	browseCmd.AddCommand(newBrowseFeaturedCmd())
	browseCmd.AddCommand(newBrowseCategoriesCmd())
}

func newBrowseSearchCmd() *cobra.Command {
	var (
		category string
		sort     string
		limit    string
		page     string
	)

	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search marketplace listings",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.ListMarketplaceListingsRequest{}
			if len(args) > 0 {
				req.Q = &args[0]
			}
			if category != "" {
				req.CategoryID = &category
			}
			if sort != "" {
				s := sdk.ListMarketplaceListingsRequestSort(sort)
				req.Sort = &s
			}
			if limit != "" {
				req.Limit = &limit
			}
			if page != "" {
				req.Page = &page
			}

			resp, err := c.MarketplaceBrowse.ListMarketplaceListings(cmd.Context(), req)
			if err != nil {
				return err
			}

			if len(resp.Data) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No listings found.")
				return nil
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				return output.Table(w, []string{"ID", "TITLE", "SELLER", "RATING", "PRICE"}, func(tw *tabwriter.Writer) {
					for _, item := range resp.Data {
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
							item.ID,
							item.Title,
							sellerID(item.SellerID, item.Seller),
							fmtRating(item.AvgRating),
							fmtPriceCents(item.PriceCents),
						)
					}
				})
			})
		},
	}

	cmd.Flags().StringVar(&category, "category", "", "Filter by category ID")
	cmd.Flags().StringVar(&sort, "sort", "", "Sort: popular, newest, top-rated")
	cmd.Flags().StringVar(&limit, "limit", "", "Results per page")
	cmd.Flags().StringVar(&page, "page", "", "Page number")
	return cmd
}

func newBrowseFeaturedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "featured",
		Short: "Show featured listings",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.MarketplaceBrowse.ListFeaturedMarketplaceListings(cmd.Context())
			if err != nil {
				return err
			}

			if len(resp.Data) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No featured listings.")
				return nil
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				return output.Table(w, []string{"ID", "TITLE", "SELLER", "RATING"}, func(tw *tabwriter.Writer) {
					for _, item := range resp.Data {
						seller := item.SellerID
						if item.Seller != nil && item.Seller.UserID != nil {
							seller = *item.Seller.UserID
						}
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
							item.ID, item.Title, seller, fmtRating(item.AvgRating),
						)
					}
				})
			})
		},
	}
}

func newBrowseCategoriesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "categories",
		Short: "List marketplace categories",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.MarketplaceBrowse.ListMarketplaceCategories(cmd.Context())
			if err != nil {
				return err
			}

			if len(resp.Data) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No categories found.")
				return nil
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				return output.Table(w, []string{"ID", "NAME", "SLUG"}, func(tw *tabwriter.Writer) {
					for _, cat := range resp.Data {
						fmt.Fprintf(tw, "%s\t%s\t%s\n", cat.ID, cat.Name, cat.Slug)
					}
				})
			})
		},
	}
}

// helpers shared across marketplace commands

func fmtRating(avgRating *string) string {
	if avgRating == nil || *avgRating == "" {
		return "-"
	}
	return *avgRating + "★"
}

func fmtPriceCents(cents int) string {
	if cents <= 0 {
		return "Free"
	}
	return fmt.Sprintf("$%.2f", float64(cents)/100)
}

type hasSeller interface {
	GetUserID() *string
}

func sellerID(fallback string, seller hasSeller) string {
	if seller != nil {
		if uid := seller.GetUserID(); uid != nil {
			return *uid
		}
	}
	return fallback
}
