package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/AIEngineering26/promptvm-cli/internal/api"
	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	sdk "github.com/AIEngineering26/promptvm-go-sdk"
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

// createListingBody is the JSON body for POST /api/v1/marketplace/listings.
//
// Sent via the raw-HTTP Caller (not the generated SDK) so it can carry the
// skillId/hookId/directoryId source aliases the backend accepts in addition
// to promptId/collectionId. The backend maps skillId/hookId to the
// underlying promptId server-side. Skill/hook/collection listings are
// free-only (priceCents must be 0).
type createListingBody struct {
	PromptID     string   `json:"promptId,omitempty"`
	CollectionID string   `json:"collectionId,omitempty"`
	SkillID      string   `json:"skillId,omitempty"`
	HookID       string   `json:"hookId,omitempty"`
	DirectoryID  string   `json:"directoryId,omitempty"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	CategoryIDs  []string `json:"categoryIds,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	PriceCents   *int     `json:"priceCents,omitempty"`
	AccessType   string   `json:"accessType,omitempty"`
}

// createListingResult decodes the create response envelope.
type createListingResult struct {
	Data struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"data"`
}

// listingSource names a single exactly-one source flag on `listings create`.
type listingSource struct {
	flag string // CLI flag name, e.g. "skill"
	val  string // current flag value
}

// validateSingleSource returns the one set source, or an error if zero or
// more than one of --prompt/--collection/--skill/--hook/--directory was
// provided. The sources are mutually exclusive.
func validateSingleSource(sources []listingSource) (listingSource, error) {
	var set []listingSource
	for _, s := range sources {
		if s.val != "" {
			set = append(set, s)
		}
	}
	switch len(set) {
	case 1:
		return set[0], nil
	case 0:
		return listingSource{}, fmt.Errorf("a source is required: provide exactly one of --prompt, --collection, --skill, --hook, or --directory")
	default:
		names := make([]string, len(set))
		for i, s := range set {
			names[i] = "--" + s.flag
		}
		return listingSource{}, fmt.Errorf("exactly one source allowed, but %s were provided (--prompt, --collection, --skill, --hook, and --directory are mutually exclusive)", strings.Join(names, ", "))
	}
}

func newListingsCreateCmd() *cobra.Command {
	var (
		promptID    string
		collID      string
		skillID     string
		hookID      string
		directoryID string
		name        string
		description string
		categoryIDs []string
		tags        []string
		price       string
		accessType  string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a listing from a prompt, skill, hook, collection, or directory",
		Long: "Create a marketplace listing from exactly one source. Skills and hooks\n" +
			"are listed via --skill/--hook (mapped to the underlying prompt id\n" +
			"server-side). Skill, hook, and collection listings are free-only.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" || description == "" {
				return fmt.Errorf("--name and --description are required")
			}

			src, err := validateSingleSource([]listingSource{
				{"prompt", promptID},
				{"collection", collID},
				{"skill", skillID},
				{"hook", hookID},
				{"directory", directoryID},
			})
			if err != nil {
				return err
			}

			body := createListingBody{
				Title:       name,
				Description: description,
				CategoryIDs: categoryIDs,
				Tags:        tags,
				AccessType:  accessType,
			}
			switch src.flag {
			case "prompt":
				body.PromptID = src.val
			case "collection":
				body.CollectionID = src.val
			case "skill":
				body.SkillID = src.val
			case "hook":
				body.HookID = src.val
			case "directory":
				body.DirectoryID = src.val
			}
			if price != "free" && price != "" {
				var cents int
				if _, err := fmt.Sscanf(price, "%d", &cents); err != nil {
					return fmt.Errorf("invalid --price %q: expected integer cents or \"free\"", price)
				}
				body.PriceCents = &cents
			}

			caller, err := api.NewFromContext(cmd)
			if err != nil {
				return err
			}

			var resp createListingResult
			if err := caller.Post("/api/v1/marketplace/listings", body, &resp); err != nil {
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
	cmd.Flags().StringVar(&skillID, "skill", "", "Source skill ID (free-only)")
	cmd.Flags().StringVar(&hookID, "hook", "", "Source hook ID (free-only)")
	cmd.Flags().StringVar(&directoryID, "directory", "", "Source directory ID")
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

// claimResult decodes the claim response envelope. Read via the raw-HTTP
// Caller so it can surface the createdItems manifest the backend returns for
// skill/hook/collection claims (the generated SDK only models the legacy
// importedPromptId/importedCollectionId fields).
type claimResult struct {
	Data struct {
		PurchaseID           string             `json:"purchaseId"`
		ImportedPromptID     *string            `json:"importedPromptId"`
		ImportedCollectionID *string            `json:"importedCollectionId"`
		ClaimedVersionID     *string            `json:"claimedVersionId"`
		CreatedItems         *claimCreatedItems `json:"createdItems"`
	} `json:"data"`
}

// claimCreatedItems mirrors the backend createdItems manifest: per-kind
// arrays of copied items plus the destination collection id (for bundles).
type claimCreatedItems struct {
	Prompts      []claimCreatedItem `json:"prompts"`
	Skills       []claimCreatedItem `json:"skills"`
	Hooks        []claimCreatedItem `json:"hooks"`
	Resources    []claimCreatedItem `json:"resources"`
	CollectionID *string            `json:"collectionId"`
}

// claimCreatedItem captures the id fields a created item may carry. The
// backend uses newFileId for content kinds and newResourceId for files; we
// keep both and resolve the populated one when reporting.
type claimCreatedItem struct {
	NewFileID       *string `json:"newFileId"`
	NewResourceID   *string `json:"newResourceId"`
	SourceVersionID *string `json:"sourceVersionId"`
}

// formatClaimManifest renders a human-readable summary of what a claim
// imported, preferring the per-kind createdItems manifest and falling back
// to the legacy importedPromptId/importedCollectionId fields. The returned
// lines never include the leading "Claimed listing" line so it can be unit
// tested independently of the listing id.
func formatClaimManifest(r *claimResult) []string {
	var lines []string
	ci := r.Data.CreatedItems
	if ci != nil {
		var parts []string
		if n := len(ci.Prompts); n > 0 {
			parts = append(parts, pluralize(n, "prompt"))
		}
		if n := len(ci.Skills); n > 0 {
			parts = append(parts, pluralize(n, "skill"))
		}
		if n := len(ci.Hooks); n > 0 {
			parts = append(parts, pluralize(n, "hook"))
		}
		if n := len(ci.Resources); n > 0 {
			parts = append(parts, pluralize(n, "file"))
		}
		if len(parts) > 0 {
			line := "Imported: " + strings.Join(parts, ", ")
			if ci.CollectionID != nil && *ci.CollectionID != "" {
				line += fmt.Sprintf(" → collection %s", *ci.CollectionID)
			}
			lines = append(lines, line)
		} else if ci.CollectionID != nil && *ci.CollectionID != "" {
			lines = append(lines, fmt.Sprintf("Imported collection %s", *ci.CollectionID))
		}
	}

	// Fall back to legacy fields when no manifest was returned (older
	// prompt/collection listings).
	if len(lines) == 0 {
		if id := r.Data.ImportedPromptID; id != nil && *id != "" {
			lines = append(lines, fmt.Sprintf("Imported prompt: %s", *id))
		}
		if id := r.Data.ImportedCollectionID; id != nil && *id != "" {
			lines = append(lines, fmt.Sprintf("Imported collection: %s", *id))
		}
	}
	return lines
}

// pluralize returns "<n> <noun>" with an "s" suffix when n != 1.
func pluralize(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func newListingsClaimCmd() *cobra.Command {
	var workspace string

	cmd := &cobra.Command{
		Use:   "claim <id>",
		Short: "Claim a free listing (prompt, skill, hook, or collection) into a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			wsID, err := resolveWorkspaceForClaim(cmd, workspace)
			if err != nil {
				return err
			}

			caller, err := api.NewFromContext(cmd)
			if err != nil {
				return err
			}

			body := map[string]string{"workspaceId": wsID}
			var resp claimResult
			if err := caller.Post(fmt.Sprintf("/api/v1/marketplace/listings/%s/claim", args[0]), body, &resp); err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Claimed listing %s\n", args[0])
			for _, line := range formatClaimManifest(&resp) {
				fmt.Fprintln(cmd.OutOrStdout(), line)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "Target workspace ID (default: config defaults.workspace)")
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
