package cmd

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/AIEngineering26/promptvm-cli/internal/hooks"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/spf13/cobra"
)

// installedHookRow holds display data for one installed hook.
type installedHookRow struct {
	Slug    string `json:"slug"`
	Version int    `json:"version"`
	Events  string `json:"events"`
	Status  string `json:"status"`
}

func newHooksListCmd() *cobra.Command {
	var scope string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed hooks",
		Long:  "Show hooks installed in the local Claude Code settings.json.",
		RunE: func(cmd *cobra.Command, args []string) error {
			sc := hooks.Scope(scope)

			settingsPath, err := hooks.SettingsFilePath(sc)
			if err != nil {
				return err
			}
			settings, err := hooks.ReadSettings(settingsPath)
			if err != nil {
				return err
			}

			tracker, err := hooks.LoadTracker(sc)
			if err != nil {
				return err
			}

			// Build rows from tracker (managed hooks).
			rows := make([]installedHookRow, 0)
			trackedSlugs := make(map[string]bool)

			for _, entry := range tracker.Hooks {
				rows = append(rows, installedHookRow{
					Slug:    entry.Slug,
					Version: entry.Version,
					Events:  strings.Join(entry.Events, ", "),
					Status:  "installed",
				})
				trackedSlugs[entry.Slug] = true
			}

			// Detect untracked (local) hooks — entries in settings hooks
			// that have a _slug not present in the tracker, or no _slug.
			settingsHooks := settings.Hooks()
			localSlugs := make(map[string]bool)
			for event, matchers := range settingsHooks {
				matcherList, ok := matchers.([]interface{})
				if !ok {
					continue
				}
				for i, m := range matcherList {
					mMap, ok := m.(map[string]interface{})
					if !ok {
						continue
					}
					slug, hasSlug := mMap["_slug"].(string)
					if hasSlug && trackedSlugs[slug] {
						continue // already listed as managed
					}
					label := fmt.Sprintf("(local:%s#%d)", event, i)
					if hasSlug {
						label = slug
					}
					if !localSlugs[label] {
						localSlugs[label] = true
						evts := event
						if hasSlug {
							// Gather all events for this slug.
							evts = gatherLocalEvents(settingsHooks, slug)
						}
						rows = append(rows, installedHookRow{
							Slug:    label,
							Version: 0,
							Events:  evts,
							Status:  "local",
						})
					}
				}
			}

			if len(rows) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No hooks installed.")
				return nil
			}

			return output.Print(cmd, rows, func(w io.Writer) error {
				return output.Table(w, []string{"SLUG", "VERSION", "EVENTS", "STATUS"}, func(tw *tabwriter.Writer) {
					for _, r := range rows {
						ver := "-"
						if r.Version > 0 {
							ver = fmt.Sprintf("v%d", r.Version)
						}
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
							r.Slug,
							ver,
							r.Events,
							r.Status,
						)
					}
				})
			})
		},
	}

	cmd.Flags().StringVar(&scope, "scope", "project", "Scope: project or user")

	return cmd
}

// gatherLocalEvents collects all event names that contain matchers with
// the given _slug value.
func gatherLocalEvents(hooksMap map[string]interface{}, slug string) string {
	var events []string
	for event, matchers := range hooksMap {
		matcherList, ok := matchers.([]interface{})
		if !ok {
			continue
		}
		for _, m := range matcherList {
			mMap, ok := m.(map[string]interface{})
			if !ok {
				continue
			}
			if s, ok := mMap["_slug"].(string); ok && s == slug {
				events = append(events, event)
				break
			}
		}
	}
	return strings.Join(events, ", ")
}

func init() {
	hooksCmd.AddCommand(newHooksListCmd())
}
