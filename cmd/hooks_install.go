package cmd

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/AIEngineering26/promptvm-cli/internal/api"
	"github.com/AIEngineering26/promptvm-cli/internal/hooks"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/spf13/cobra"
)

// hooksInstallDetail is the JSON shape returned by GET /api/v1/hooks/s/{slug}.
type hooksInstallDetail struct {
	Slug         string                 `json:"slug"`
	ContentKind  string                 `json:"content_kind"`
	Version      int                    `json:"version"`
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	Events       []string               `json:"events"`
	HandlerTypes []string               `json:"handler_types"`
	Config       map[string]interface{} `json:"config"`
	Tags         []string               `json:"tags"`
}

type hooksInstallResponse struct {
	Data hooksInstallDetail `json:"data"`
}

func newHooksInstallCmd() *cobra.Command {
	var (
		scope   string
		version int
		dryRun  bool
		force   bool
	)

	cmd := &cobra.Command{
		Use:   "install <slug>",
		Short: "Install a hook from the PromptVM registry",
		Long:  "Fetches a hook by slug and merges its configuration into your local Claude Code settings.json.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]

			caller, err := api.NewFromContext(cmd)
			if err != nil {
				return err
			}

			// Fetch hook by public slug endpoint.
			path := fmt.Sprintf("/api/v1/hooks/s/%s", slug)
			if version > 0 {
				path = fmt.Sprintf("%s?version=%d", path, version)
			}

			var resp hooksInstallResponse
			if err := caller.Get(path, &resp); err != nil {
				return err
			}

			hook := resp.Data
			if len(hook.Config) == 0 {
				return fmt.Errorf("hook %q has no config to install", slug)
			}

			sc := hooks.Scope(scope)

			// Load current settings.
			settingsPath, err := hooks.SettingsFilePath(sc)
			if err != nil {
				return err
			}
			settings, err := hooks.ReadSettings(settingsPath)
			if err != nil {
				return err
			}

			// Load tracker.
			tracker, err := hooks.LoadTracker(sc)
			if err != nil {
				return err
			}

			// Check if already installed.
			if existing := tracker.Get(slug); existing != nil && !force {
				return fmt.Errorf("hook %q is already installed (v%d); use --force to overwrite", slug, existing.Version)
			}

			// Inject _slug metadata into each matcher so we can track ownership.
			for eventName, matchers := range hook.Config {
				matcherList, ok := matchers.([]interface{})
				if !ok {
					continue
				}
				for _, m := range matcherList {
					if mMap, ok := m.(map[string]interface{}); ok {
						mMap["_slug"] = slug
					}
				}
				hook.Config[eventName] = matcherList
			}

			// Merge hook config into settings (force removes old entries first).
			settings.MergeHook(hook.Config, slug, force)

			// Compute checksum of the config.
			checksum := hooks.Checksum(hook.Config)

			if dryRun {
				return output.Print(cmd, resp, func(w io.Writer) error {
					fmt.Fprintf(w, "[dry-run] Would install hook %q (v%d) into %s\n", slug, hook.Version, settingsPath)
					for eventName, matchers := range hook.Config {
						count := 0
						if ml, ok := matchers.([]interface{}); ok {
							count = len(ml)
						}
						fmt.Fprintf(w, "  Event: %s (%d matchers)\n", eventName, count)
					}
					return nil
				})
			}

			// Write settings.
			if err := settings.Write(settingsPath); err != nil {
				return fmt.Errorf("saving settings: %w", err)
			}

			// Update tracker.
			tracker.Add(hooks.TrackedHook{
				Slug:        slug,
				Version:     hook.Version,
				SourceURL:   fmt.Sprintf("promptvm.ai/s/%s", slug),
				InstalledAt: time.Now().UTC().Format(time.RFC3339),
				Events:      hook.Events,
				Checksum:    checksum,
			})
			if err := tracker.Save(); err != nil {
				return fmt.Errorf("saving tracker: %w", err)
			}

			// Print summary.
			return output.Print(cmd, resp, func(w io.Writer) error {
				fmt.Fprintf(w, "Installed hook %q (v%d) into %s\n", slug, hook.Version, settingsPath)
				eventSummaries := make([]string, 0, len(hook.Config))
				for eventName, matchers := range hook.Config {
					count := 0
					if ml, ok := matchers.([]interface{}); ok {
						count = len(ml)
					}
					eventSummaries = append(eventSummaries, fmt.Sprintf("%s (%d matcher)", eventName, count))
				}
				fmt.Fprintf(w, "  Events: %s\n", strings.Join(eventSummaries, ", "))
				fmt.Fprintf(w, "  Source: promptvm.ai/s/%s\n", slug)
				return nil
			})
		},
	}

	cmd.Flags().StringVar(&scope, "scope", "project", "Scope: project or user")
	cmd.Flags().IntVar(&version, "version", 0, "Specific version to install (default: latest)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without writing files")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite if already installed")

	return cmd
}

func init() {
	hooksCmd.AddCommand(newHooksInstallCmd())
}
