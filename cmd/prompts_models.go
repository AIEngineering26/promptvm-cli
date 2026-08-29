package cmd

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	sdk "github.com/AIEngineering26/promptvm-go-sdk"
	sdkclient "github.com/AIEngineering26/promptvm-go-sdk/client"
	"github.com/spf13/cobra"
)

// "Use with" on a resource — the models a prompt, skill or hook is written for.
//
// Stored against a VERSION, not the resource, because that is what a
// marketplace listing pins at publish. These commands default to the current
// version, which is what a creator means by "this prompt's models"; --version
// addresses an older one.
var promptsModelsCmd = &cobra.Command{
	Use:     "models",
	Aliases: []string{"use-with"},
	Short:   "Manage the models a resource is recommended for",
	Long: "Reads and replaces \"Use with\" on a resource's current version.\n\n" +
		"Models are named as provider/slug (see `promptvm marketplace models`)\n" +
		"or by id. Works for prompts, skills and hooks — all three are versioned\n" +
		"resources and take the same id.",
}

func init() {
	promptsCmd.AddCommand(promptsModelsCmd)
	promptsModelsCmd.AddCommand(newPromptModelsListCmd())
	promptsModelsCmd.AddCommand(newPromptModelsSetCmd())
	promptsModelsCmd.AddCommand(newPromptModelsClearCmd())
}

// resolveVersionID turns an explicit --version into itself, or finds the
// resource's current version.
//
// The lookup exists because "Use with" is addressed by version ID and the
// resource read shapes surface a version NUMBER. Skill and hook responses now
// carry versionId too, but a prompt id is the one thing every caller has, so
// resolving from it keeps one code path for all three kinds.
func resolveVersionID(cmd *cobra.Command, c *sdkclient.Client, promptID, explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}

	resp, err := c.PromptVersions.ListPromptVersions(cmd.Context(), &sdk.ListPromptVersionsRequest{
		PromptID: promptID,
	})
	if err != nil {
		return "", err
	}
	for _, v := range resp.Data {
		if v.IsCurrentVersion {
			return v.ID, nil
		}
	}
	if len(resp.Data) > 0 {
		return resp.Data[0].ID, nil
	}
	return "", fmt.Errorf("%s has no versions yet", promptID)
}

// splitModelRefs accepts both repeated flags and comma-separated values, so
// --models a,b and --models a --models b mean the same thing. Order is kept:
// it becomes the display order on the listing.
func splitModelRefs(values []string) []string {
	var out []string
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

var uuidRef = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// validateModelRefs rejects a malformed reference before it reaches the API.
//
// The server enforces the same shape, but its answer is a schema violation
// carrying the raw regex — which is what someone who typed `--models
// claude-opus-5` would otherwise read. A slug without its provider is the
// mistake people will actually make, since model slugs are unique only per
// provider, so it gets its own sentence.
func validateModelRefs(refs []string) error {
	for _, r := range refs {
		if uuidRef.MatchString(r) {
			continue
		}
		provider, slug, found := strings.Cut(r, "/")
		if found && provider != "" && slug != "" {
			continue
		}
		if !found {
			return fmt.Errorf(
				"%q is missing its provider — models are named provider/slug, e.g. anthropic/%s.\n"+
					"Run `promptvm marketplace models` to see the catalog", r, r)
		}
		return fmt.Errorf("%q is not a model reference; use provider/slug or a model id", r)
	}
	return nil
}

func printModelTable(cmd *cobra.Command, resp any, rows []struct{ Slug, Name, Provider string }) error {
	if len(rows) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No models set.")
		return nil
	}
	return output.Print(cmd, resp, func(w io.Writer) error {
		return output.Table(w, []string{"SLUG", "NAME", "PROVIDER"}, func(tw *tabwriter.Writer) {
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Slug, r.Name, r.Provider)
			}
		})
	})
}

func newPromptModelsListCmd() *cobra.Command {
	var version string

	cmd := &cobra.Command{
		Use:   "list <prompt-id>",
		Short: "Show the models a resource is recommended for",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}
			versionID, err := resolveVersionID(cmd, c, args[0], version)
			if err != nil {
				return err
			}

			resp, err := c.PromptVersions.GetVersionRecommendedModels(cmd.Context(),
				&sdk.GetVersionRecommendedModelsRequest{PromptID: args[0], VersionID: versionID})
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
	cmd.Flags().StringVar(&version, "version", "", "Version ID (default: the current version)")
	return cmd
}

func newPromptModelsSetCmd() *cobra.Command {
	var (
		version string
		models  []string
	)

	cmd := &cobra.Command{
		Use:   "set <prompt-id>",
		Short: "Replace the models a resource is recommended for",
		Long: "Replace-all: whatever is listed becomes the complete set, in the\n" +
			"order given. Up to 10. An unknown or retired model is rejected rather\n" +
			"than skipped, so a typo fails loudly instead of silently narrowing\n" +
			"the selection.",
		Args: cobra.ExactArgs(1),
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
			versionID, err := resolveVersionID(cmd, c, args[0], version)
			if err != nil {
				return err
			}

			resp, err := c.PromptVersions.SetVersionRecommendedModels(cmd.Context(),
				&sdk.SetVersionRecommendedModelsRequest{
					PromptID: args[0], VersionID: versionID, ModelIDs: refs,
				})
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
	cmd.Flags().StringVar(&version, "version", "", "Version ID (default: the current version)")
	cmd.Flags().StringSliceVar(&models, "models", nil, "Models as provider/slug or id (repeatable or comma-separated)")
	return cmd
}

func newPromptModelsClearCmd() *cobra.Command {
	var version string

	cmd := &cobra.Command{
		Use:   "clear <prompt-id>",
		Short: "Remove every recommended model from a resource",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}
			versionID, err := resolveVersionID(cmd, c, args[0], version)
			if err != nil {
				return err
			}

			if _, err := c.PromptVersions.SetVersionRecommendedModels(cmd.Context(),
				&sdk.SetVersionRecommendedModelsRequest{
					PromptID: args[0], VersionID: versionID, ModelIDs: []string{},
				}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Cleared recommended models on %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&version, "version", "", "Version ID (default: the current version)")
	return cmd
}
