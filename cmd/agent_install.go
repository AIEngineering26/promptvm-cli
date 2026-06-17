package cmd

import (
	"fmt"
	"io"
	"time"

	"github.com/AIEngineering26/promptvm-cli/internal/agentskill"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newAgentInstallCmd() *cobra.Command {
	var (
		scope  string
		target string
		force  bool
		dryRun bool
	)

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the promptvm agent skill",
		Long:  "Writes the bundled promptvm Agent Skill into the Claude Code and/or Codex skills directories.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sc, err := resolveScope(scope)
			if err != nil {
				return err
			}
			targets, err := resolveTargets(target)
			if err != nil {
				return err
			}

			if dryRun {
				return runAgentInstallDryRun(cmd, sc, targets)
			}

			installed, err := agentskill.Install(sc, targets, force)
			if err != nil {
				return err
			}

			tracker := &agentskill.Tracker{
				Name:        agentskill.Name,
				Version:     agentskill.Version,
				Checksum:    agentskill.Checksum(),
				Status:      agentskill.StatusInstalled,
				InstalledAt: time.Now().UTC().Format(time.RFC3339),
			}
			for _, it := range installed {
				tracker.Targets = append(tracker.Targets, agentskill.TrackedTarget(it))
			}
			if err := tracker.Save(); err != nil {
				return fmt.Errorf("saving marker: %w", err)
			}

			return output.Print(cmd, installed, func(w io.Writer) error {
				fmt.Fprintf(w, "Installed %q skill (v%d):\n", agentskill.Name, agentskill.Version)
				for _, it := range installed {
					fmt.Fprintf(w, "  %s → %s\n", it.Key, it.Path)
				}
				return nil
			})
		},
	}

	cmd.Flags().StringVar(&scope, "scope", "user", "Scope: user or project")
	cmd.Flags().StringVar(&target, "target", "all", "Target: claude, codex, or all")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing skill folder")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview paths without writing files")

	return cmd
}

// agentInstallPlan is the dry-run shape for a single target.
type agentInstallPlan struct {
	Target string   `json:"target"`
	Dir    string   `json:"dir"`
	Files  []string `json:"files"`
}

func runAgentInstallDryRun(cmd *cobra.Command, sc agentskill.Scope, targets []agentskill.Target) error {
	files, err := agentskill.Files()
	if err != nil {
		return err
	}
	plans := make([]agentInstallPlan, 0, len(targets))
	for _, t := range targets {
		dir, err := t.DestDir(sc)
		if err != nil {
			return err
		}
		plans = append(plans, agentInstallPlan{Target: t.Key, Dir: dir, Files: files})
	}

	return output.Print(cmd, plans, func(w io.Writer) error {
		for _, p := range plans {
			fmt.Fprintf(w, "[dry-run] Would install %q skill (v%d) to %s\n", agentskill.Name, agentskill.Version, p.Dir)
			for _, f := range p.Files {
				fmt.Fprintf(w, "  %s/%s\n", agentskill.Name, f)
			}
		}
		return nil
	})
}

func init() {
	agentCmd.AddCommand(newAgentInstallCmd())
}
