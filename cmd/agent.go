package cmd

import (
	"fmt"

	"github.com/AIEngineering26/promptvm-cli/internal/agentskill"
	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage the PromptVM agent skill for Claude Code / Codex",
	Long: "Install, inspect, and remove the bundled \"promptvm\" Agent Skill that teaches " +
		"Claude Code and Codex how to use PromptVM. The skill is installed automatically on " +
		"first run; set PROMPTVM_NO_AGENT_SKILL=1 to opt out.",
}

func init() {
	rootCmd.AddCommand(agentCmd)
}

// resolveScope maps the --scope flag to an agentskill.Scope.
func resolveScope(scope string) (agentskill.Scope, error) {
	switch scope {
	case "", "user":
		return agentskill.ScopeUser, nil
	case "project":
		return agentskill.ScopeProject, nil
	default:
		return "", fmt.Errorf("invalid --scope %q: must be user or project", scope)
	}
}

// resolveTargets maps the --target flag to a list of agentskill.Target.
func resolveTargets(target string) ([]agentskill.Target, error) {
	switch target {
	case "", "all":
		return agentskill.AllTargets(), nil
	case "claude", "codex":
		t, ok := agentskill.TargetByKey(target)
		if !ok {
			return nil, fmt.Errorf("unknown target %q", target)
		}
		return []agentskill.Target{t}, nil
	default:
		return nil, fmt.Errorf("invalid --target %q: must be claude, codex, or all", target)
	}
}
