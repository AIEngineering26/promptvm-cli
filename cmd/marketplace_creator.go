package cmd

import (
	"fmt"
	"io"
	"text/tabwriter"

	sdk "github.com/AIEngineering26/promptvm-go-sdk"
	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/spf13/cobra"
)

var creatorCmd = &cobra.Command{
	Use:   "creator",
	Short: "Creator profile and dashboard",
}

func init() {
	marketplaceCmd.AddCommand(creatorCmd)
	creatorCmd.AddCommand(newCreatorProfileCmd())
	creatorCmd.AddCommand(newCreatorDashboardCmd())
	creatorCmd.AddCommand(newCreatorListingsCmd())
}

func newCreatorProfileCmd() *cobra.Command {
	var (
		bio     string
		website string
		create  bool
	)

	cmd := &cobra.Command{
		Use:   "profile",
		Short: "View or update your creator profile",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			// Create new profile
			if create {
				if bio == "" {
					return fmt.Errorf("--bio is required when creating a profile")
				}
				req := &sdk.CreateMarketplaceCreatorProfileRequest{Bio: bio}
				if website != "" {
					req.Website = &website
				}
				resp, err := c.MarketplaceCreator.CreateMarketplaceCreatorProfile(cmd.Context(), req)
				if err != nil {
					return err
				}
				if output.Format(cmd) != "table" {
					return output.Print(cmd, resp, nil)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Creator profile created: %s\n", resp.Data.ID)
				return nil
			}

			// Update profile
			if bio != "" || website != "" {
				req := &sdk.UpdateMyMarketplaceCreatorProfileRequest{}
				if bio != "" {
					req.Bio = &bio
				}
				if website != "" {
					req.Website = &website
				}
				resp, err := c.MarketplaceCreator.UpdateMyMarketplaceCreatorProfile(cmd.Context(), req)
				if err != nil {
					return err
				}
				if output.Format(cmd) != "table" {
					return output.Print(cmd, resp, nil)
				}
				fmt.Fprintln(cmd.OutOrStdout(), "Creator profile updated.")
				return nil
			}

			// View profile
			resp, err := c.MarketplaceCreator.GetMyMarketplaceCreatorProfile(cmd.Context())
			if err != nil {
				return err
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				d := resp.Data
				printField(cmd, "ID", d.ID)
				printField(cmd, "User ID", d.UserID)
				printField(cmd, "Bio", derefStr(d.Bio))
				printField(cmd, "Website", derefStr(d.Website))
				printField(cmd, "Verified", fmt.Sprintf("%v", d.IsVerified))
				printField(cmd, "Created", d.CreatedAt.Format("2006-01-02"))
				printField(cmd, "Updated", d.UpdatedAt.Format("2006-01-02"))
				return nil
			})
		},
	}

	cmd.Flags().StringVar(&bio, "bio", "", "Update bio")
	cmd.Flags().StringVar(&website, "website", "", "Update website URL")
	cmd.Flags().BoolVar(&create, "create", false, "Create a new creator profile")
	return cmd
}

func newCreatorDashboardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dashboard",
		Short: "View creator dashboard with listing analytics",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.MarketplaceCreatorDashboard.ListMarketplaceCreatorListings(cmd.Context(), &sdk.ListMarketplaceCreatorListingsRequest{})
			if err != nil {
				return err
			}

			if len(resp.Data) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No listings yet. Create one with `promptvm mp listings create`.")
				return nil
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				totalViews, totalDownloads := 0, 0

				err := output.Table(w, []string{"LISTING", "VIEWS", "DOWNLOADS", "RATING", "STATUS"}, func(tw *tabwriter.Writer) {
					for _, item := range resp.Data {
						views := intOrZero(item.ViewCount)
						downloads := intOrZero(item.PurchaseCount)
						totalViews += views
						totalDownloads += downloads

						fmt.Fprintf(tw, "%s\t%d\t%d\t%s\t%s\n",
							item.Title, views, downloads,
							fmtRating(item.AvgRating),
							string(item.Status),
						)
					}
				})
				if err != nil {
					return err
				}

				fmt.Fprintf(w, "\nTotal listings: %d\n", len(resp.Data))
				fmt.Fprintf(w, "Total views: %d\n", totalViews)
				fmt.Fprintf(w, "Total downloads: %d\n", totalDownloads)
				return nil
			})
		},
	}
}

func newCreatorListingsCmd() *cobra.Command {
	var (
		status string
		limit  string
		page   string
	)

	cmd := &cobra.Command{
		Use:   "listings",
		Short: "List your marketplace listings",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.ListMarketplaceCreatorListingsRequest{}
			if status != "" {
				s := sdk.ListMarketplaceCreatorListingsRequestStatus(status)
				req.Status = &s
			}
			if limit != "" {
				req.Limit = &limit
			}
			if page != "" {
				req.Page = &page
			}

			resp, err := c.MarketplaceCreatorDashboard.ListMarketplaceCreatorListings(cmd.Context(), req)
			if err != nil {
				return err
			}

			if len(resp.Data) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No listings found.")
				return nil
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				return output.Table(w, []string{"ID", "TITLE", "STATUS", "RATING", "PRICE"}, func(tw *tabwriter.Writer) {
					for _, item := range resp.Data {
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
							item.ID, item.Title, string(item.Status),
							fmtRating(item.AvgRating), fmtPriceCents(item.PriceCents),
						)
					}
				})
			})
		},
	}

	cmd.Flags().StringVar(&status, "status", "", "Filter: draft, active, inactive, archived")
	cmd.Flags().StringVar(&limit, "limit", "", "Results per page")
	cmd.Flags().StringVar(&page, "page", "", "Page number")
	return cmd
}

func intOrZero(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}
