package cmd

import (
	"fmt"
	"io"

	"github.com/AIEngineering26/promptvm-cli/internal/agentskill"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/spf13/cobra"
)

type agentStatusView struct {
	Skill            string                     `json:"skill"`
	BundledVersion   int                        `json:"bundled_version"`
	BundledChecksum  string                     `json:"bundled_checksum"`
	Status           string                     `json:"status"`
	InstalledVersion int                        `json:"installed_version,omitempty"`
	UpdateAvailable  bool                       `json:"update_available"`
	Targets          []agentskill.TrackedTarget `json:"targets,omitempty"`
	InstalledAt      string                     `json:"installed_at,omitempty"`
}

func newAgentStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the promptvm agent skill status",
		Long:  "Reports the bundled skill version/checksum and what is currently installed.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			tracker, err := agentskill.LoadTracker()
			if err != nil {
				return err
			}

			view := agentStatusView{
				Skill:           agentskill.Name,
				BundledVersion:  agentskill.Version,
				BundledChecksum: agentskill.Checksum(),
				Status:          "not-installed",
			}
			if tracker != nil {
				view.Status = tracker.Status
				view.InstalledVersion = tracker.Version
				view.Targets = tracker.Targets
				view.InstalledAt = tracker.InstalledAt
				view.UpdateAvailable = tracker.Status == agentskill.StatusInstalled &&
					agentskill.Version > tracker.Version
			}

			return output.Print(cmd, view, func(w io.Writer) error {
				fmt.Fprintf(w, "Bundled:  %s v%d (%s)\n", view.Skill, view.BundledVersion, shortSum(view.BundledChecksum))
				fmt.Fprintf(w, "Status:   %s\n", view.Status)
				if view.Status == agentskill.StatusInstalled {
					fmt.Fprintf(w, "Installed: v%d", view.InstalledVersion)
					if view.InstalledAt != "" {
						fmt.Fprintf(w, " (%s)", view.InstalledAt)
					}
					fmt.Fprintln(w)
					for _, t := range view.Targets {
						fmt.Fprintf(w, "  %s → %s\n", t.Key, t.Path)
					}
					if view.UpdateAvailable {
						fmt.Fprintln(w, "Update available — run `promptvm agent install --force`")
					}
				}
				return nil
			})
		},
	}

	return cmd
}

// shortSum truncates a checksum for human-readable display.
func shortSum(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func init() {
	agentCmd.AddCommand(newAgentStatusCmd())
}
