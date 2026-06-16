package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/AIEngineering26/promptvm-cli/internal/agentskill"
	"github.com/spf13/cobra"
)

// skipAutoInstallCommands are top-level command names for which we never trigger
// the first-run skill install — to avoid double-installing (agent) and to keep
// machine-facing output clean (version, completion, help).
var skipAutoInstallCommands = map[string]bool{
	"agent":      true,
	"version":    true,
	"completion": true,
	"help":       true,
}

// maybeAutoInstallAgentSkill installs the bundled promptvm agent skill on the
// very first CLI invocation (opt-out via PROMPTVM_NO_AGENT_SKILL).
//
// It is best-effort: it never blocks or fails the user's actual command. The
// env opt-out short-circuits before any filesystem access, which also keeps the
// test suite hermetic.
func maybeAutoInstallAgentSkill(cmd *cobra.Command) {
	if os.Getenv("PROMPTVM_NO_AGENT_SKILL") != "" {
		return
	}
	// Never let auto-install panic the CLI.
	defer func() { _ = recover() }()

	if skipAutoInstallCommands[topLevelName(cmd)] {
		return
	}

	// The marker's presence (any status) makes this idempotent.
	exists, err := agentskill.Exists()
	if err != nil || exists {
		return
	}

	installed, err := agentskill.Install(agentskill.ScopeUser, agentskill.AllTargets(), false)
	if err != nil {
		return // non-fatal
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
		return
	}

	keys := make([]string, 0, len(installed))
	for _, it := range installed {
		keys = append(keys, it.Key)
	}
	fmt.Fprintf(os.Stderr,
		"✓ Installed the promptvm agent skill for %s. "+
			"Manage with `promptvm agent`; remove with `promptvm agent uninstall`; "+
			"disable with PROMPTVM_NO_AGENT_SKILL=1.\n",
		strings.Join(keys, ", "))
}

// topLevelName returns the name of the top-level command (the child of root)
// that owns cmd, or cmd's own name when cmd is the root.
func topLevelName(cmd *cobra.Command) string {
	c := cmd
	for c.Parent() != nil {
		if c.Parent().Parent() == nil {
			return c.Name()
		}
		c = c.Parent()
	}
	return cmd.Name()
}
