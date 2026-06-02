package cmd

import (
	"fmt"

	"github.com/AIEngineering26/promptvm-cli/internal/hooks"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newHooksUninstallCmd() *cobra.Command {
	var (
		scope string
		yes   bool
	)

	cmd := &cobra.Command{
		Use:   "uninstall <slug>",
		Short: "Uninstall a managed hook",
		Long:  "Remove a PromptVM-managed hook from the local Claude Code settings.json and tracker.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			sc := hooks.Scope(scope)

			// Load tracker.
			tracker, err := hooks.LoadTracker(sc)
			if err != nil {
				return err
			}

			entry := tracker.Get(slug)
			if entry == nil {
				return fmt.Errorf("hook %q is not installed (not found in tracker)", slug)
			}

			// Confirm.
			if !yes {
				if !output.Confirm(fmt.Sprintf("Uninstall hook %q (v%d)?", slug, entry.Version)) {
					fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
			}

			// Load settings.
			settingsPath, err := hooks.SettingsFilePath(sc)
			if err != nil {
				return err
			}
			settings, err := hooks.ReadSettings(settingsPath)
			if err != nil {
				return err
			}

			// Remove the hook's event entries from settings.
			settings.RemoveHook(tracker, slug)

			// Write settings.
			if err := settings.Write(settingsPath); err != nil {
				return fmt.Errorf("saving settings: %w", err)
			}

			// Remove from tracker and save.
			tracker.Remove(slug)
			if err := tracker.Save(); err != nil {
				return fmt.Errorf("saving tracker: %w", err)
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, map[string]string{"slug": slug, "status": "uninstalled"}, nil)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Uninstalled hook %q\n", slug)
			return nil
		},
	}

	cmd.Flags().StringVar(&scope, "scope", "project", "Scope: project or user")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation")

	return cmd
}

func init() {
	hooksCmd.AddCommand(newHooksUninstallCmd())
}
