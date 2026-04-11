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

func init() {
	marketplaceCmd.AddCommand(newSubscribeCmd())
	marketplaceCmd.AddCommand(newUnsubscribeCmd())
	marketplaceCmd.AddCommand(newRateCmd())
	marketplaceCmd.AddCommand(newCommentCmd())
	marketplaceCmd.AddCommand(newCommentsCmd())
	marketplaceCmd.AddCommand(newFollowCmd())
	marketplaceCmd.AddCommand(newUnfollowCmd())
	marketplaceCmd.AddCommand(newFollowingCmd())
	marketplaceCmd.AddCommand(newFeedCmd())
}

// --- subscribe / unsubscribe ---

func newSubscribeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "subscribe <creator-user-id>",
		Short: "Subscribe to a creator",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.MarketplaceSubscriptions.SubscribeToMarketplaceCreator(cmd.Context(), &sdk.SubscribeToMarketplaceCreatorRequest{
				CreatorUserID: args[0],
			})
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Subscribed to creator %s (subscription: %s)\n", args[0], resp.Data.ID)
			return nil
		},
	}
}

func newUnsubscribeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unsubscribe <creator-user-id>",
		Short: "Unsubscribe from a creator",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			if err := c.MarketplaceSubscriptions.UnsubscribeFromMarketplaceCreator(cmd.Context(), &sdk.UnsubscribeFromMarketplaceCreatorRequest{
				CreatorUserID: args[0],
			}); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Unsubscribed from creator %s\n", args[0])
			return nil
		},
	}
}

// --- rate ---

func newRateCmd() *cobra.Command {
	var (
		stars  int
		review string
	)

	cmd := &cobra.Command{
		Use:   "rate <listing-id>",
		Short: "Rate a listing",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if stars < 1 || stars > 5 {
				return fmt.Errorf("--stars is required and must be 1-5")
			}

			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.PostAPIV1MarketplaceListingsListingIDRatingsRequest{
				ListingID: args[0],
				Score:     stars,
			}
			if review != "" {
				req.Review = &review
			}

			resp, err := c.MarketplaceRatings.CreateARatingOnAPurchasedListing(cmd.Context(), req)
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Rating submitted: %d★\n", stars)
			return nil
		},
	}

	cmd.Flags().IntVar(&stars, "stars", 0, "Rating score 1-5 (required)")
	cmd.Flags().StringVar(&review, "review", "", "Optional text review")
	return cmd
}

// --- comment / comments ---

func newCommentCmd() *cobra.Command {
	var (
		message  string
		parentID string
	)

	cmd := &cobra.Command{
		Use:   "comment <listing-id>",
		Short: "Post a comment on a listing",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if message == "" {
				return fmt.Errorf("--message is required")
			}

			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.PostAPIV1MarketplaceListingsListingIDCommentsRequest{
				ListingID: args[0],
				Content:   message,
			}
			if parentID != "" {
				req.ParentID = &parentID
			}

			resp, err := c.MarketplaceComments.CreateACommentOrReplyOnAListing(cmd.Context(), req)
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Comment posted.")
			return nil
		},
	}

	cmd.Flags().StringVar(&message, "message", "", "Comment text (required)")
	cmd.Flags().StringVar(&parentID, "parent", "", "Parent comment ID for replies")
	return cmd
}

func newCommentsCmd() *cobra.Command {
	var (
		limit int
		page  int
	)

	cmd := &cobra.Command{
		Use:   "comments <listing-id>",
		Short: "View comments on a listing",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.GetAPIV1MarketplaceListingsListingIDCommentsRequest{
				ListingID: args[0],
			}
			if limit > 0 {
				req.Limit = &limit
			}
			if page > 0 {
				req.Page = &page
			}

			resp, err := c.MarketplaceComments.ListThreadedCommentsOnAListing(cmd.Context(), req)
			if err != nil {
				return err
			}

			if len(resp.Data) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No comments yet.")
				return nil
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				for _, comment := range resp.Data {
					author := "unknown"
					if comment.Author != nil && comment.Author.DisplayName != nil {
						author = *comment.Author.DisplayName
					}
					seller := ""
					if comment.IsSeller {
						seller = " [seller]"
					}
					fmt.Fprintf(w, "@%s%s  %s\n", author, seller, comment.CreatedAt.Format("2006-01-02 15:04"))
					fmt.Fprintf(w, "  %s\n", comment.Content)

					for _, reply := range comment.Replies {
						replyAuthor := "unknown"
						if reply.Author != nil && reply.Author.DisplayName != nil {
							replyAuthor = *reply.Author.DisplayName
						}
						replySeller := ""
						if reply.IsSeller {
							replySeller = " [seller]"
						}
						fmt.Fprintf(w, "  └─ @%s%s  %s\n", replyAuthor, replySeller, reply.CreatedAt.Format("2006-01-02 15:04"))
						fmt.Fprintf(w, "     %s\n", reply.Content)
					}
					fmt.Fprintln(w)
				}

				if resp.Meta != nil && resp.Meta.Total > 0 {
					pages := (resp.Meta.Total + resp.Meta.Limit - 1) / resp.Meta.Limit
					fmt.Fprintf(w, "%d comments (page %d of %d)\n", resp.Meta.Total, resp.Meta.Page, pages)
				}
				return nil
			})
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Results per page")
	cmd.Flags().IntVar(&page, "page", 0, "Page number")
	return cmd
}

// --- follow / unfollow / following / feed ---

func newFollowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "follow <creator-user-id>",
		Short: "Follow a creator",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.MarketplaceSocial.FollowACreator(cmd.Context(), &sdk.PostAPIV1MarketplaceCreatorCreatorUserIDFollowRequest{
				CreatorUserID: args[0],
			})
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Now following creator %s\n", args[0])
			return nil
		},
	}
}

func newUnfollowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unfollow <creator-user-id>",
		Short: "Unfollow a creator",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			if err := c.MarketplaceSocial.UnfollowACreator(cmd.Context(), &sdk.DeleteAPIV1MarketplaceCreatorCreatorUserIDFollowRequest{
				CreatorUserID: args[0],
			}); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Unfollowed creator %s\n", args[0])
			return nil
		},
	}
}

func newFollowingCmd() *cobra.Command {
	var (
		limit int
		page  int
	)

	cmd := &cobra.Command{
		Use:   "following",
		Short: "List creators you follow",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.GetAPIV1MarketplaceCreatorMeFollowingRequest{}
			if limit > 0 {
				req.Limit = &limit
			}
			if page > 0 {
				req.Page = &page
			}

			resp, err := c.MarketplaceSocial.ListCreatorsIFollow(cmd.Context(), req)
			if err != nil {
				return err
			}

			if len(resp.Data) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Not following any creators yet.")
				return nil
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				return output.Table(w, []string{"USER ID", "DISPLAY NAME", "FOLLOWED"}, func(tw *tabwriter.Writer) {
					for _, item := range resp.Data {
						name := "-"
						if item.DisplayName != nil {
							name = *item.DisplayName
						}
						fmt.Fprintf(tw, "%s\t%s\t%s\n", item.UserID, name, item.FollowedAt.Format("2006-01-02"))
					}
				})
			})
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Results per page")
	cmd.Flags().IntVar(&page, "page", 0, "Page number")
	return cmd
}

func newFeedCmd() *cobra.Command {
	var (
		limit int
		page  int
	)

	cmd := &cobra.Command{
		Use:   "feed",
		Short: "Show listings from creators you follow",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.GetAPIV1MarketplaceCreatorMeFeedRequest{}
			if limit > 0 {
				req.Limit = &limit
			}
			if page > 0 {
				req.Page = &page
			}

			resp, err := c.MarketplaceSocial.FollowedCreatorListingFeed(cmd.Context(), req)
			if err != nil {
				return err
			}

			if len(resp.Data) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No listings in your feed. Follow some creators first!")
				return nil
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				return output.Table(w, []string{"ID", "TITLE", "SELLER", "RATING", "PRICE"}, func(tw *tabwriter.Writer) {
					for _, item := range resp.Data {
						seller := item.SellerID
						if item.Seller != nil && item.Seller.UserID != nil {
							seller = *item.Seller.UserID
						}
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
							item.ID, item.Title, seller,
							fmtRating(item.AvgRating), fmtPriceCents(item.PriceCents),
						)
					}
				})
			})
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Results per page")
	cmd.Flags().IntVar(&page, "page", 0, "Page number")
	return cmd
}
