package cmd

import (
	"fmt"
	"io"

	"github.com/AIEngineering26/promptvm-cli/internal/agentskill"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newAgentUninstallCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the installed promptvm agent skill",
		Long:  "Removes the promptvm Agent Skill folders the CLI installed and clears the tracker.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			tracker, err := agentskill.LoadTracker()
			if err != nil {
				return err
			}
			if tracker == nil || tracker.Status != agentskill.StatusInstalled || len(tracker.Targets) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No installed promptvm agent skill to remove.")
				return nil
			}

			if !yes {
				if !output.Confirm("Remove the promptvm agent skill from all installed targets?") {
					fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
			}

			paths := make([]string, 0, len(tracker.Targets))
			for _, t := range tracker.Targets {
				paths = append(paths, t.Path)
			}
			if err := agentskill.Uninstall(paths); err != nil {
				return err
			}
			if err := agentskill.Clear(); err != nil {
				return err
			}

			return output.Print(cmd, map[string]string{"status": "uninstalled"}, func(w io.Writer) error {
				fmt.Fprintln(w, "Removed promptvm agent skill:")
				for _, t := range tracker.Targets {
					fmt.Fprintf(w, "  %s → %s\n", t.Key, t.Path)
				}
				return nil
			})
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation")

	return cmd
}

func init() {
	agentCmd.AddCommand(newAgentUninstallCmd())
}
