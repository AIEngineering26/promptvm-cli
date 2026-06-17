package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AIEngineering26/promptvm-cli/internal/agentskill"
	"github.com/spf13/cobra"
)

// withTempEnv redirects HOME / config / CODEX_HOME to temp dirs so agent
// command tests never touch the developer's real home directories. It returns
// the temp home. The suite-wide TestMain opt-out (PROMPTVM_NO_AGENT_SKILL=1)
// stays in effect unless a test clears it explicitly.
func withTempEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)                            // Windows
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".cfg")) // Linux config.Dir()
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))     // absolute → honored
	return home
}

func runAgentCmd(t *testing.T, newCmd func() *cobra.Command, args ...string) (string, error) {
	t.Helper()
	c := newCmd()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetArgs(args)
	err := c.Execute()
	return out.String(), err
}

func TestAgentInstallDryRunWritesNothing(t *testing.T) {
	withTempEnv(t)
	out, err := runAgentCmd(t, newAgentInstallCmd, "--dry-run")
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !strings.Contains(out, "[dry-run] Would install") {
		t.Errorf("missing dry-run summary: %q", out)
	}
	if ex, _ := agentskill.Exists(); ex {
		t.Error("dry-run must not write the marker")
	}
}

func TestAgentStatusNotInstalled(t *testing.T) {
	withTempEnv(t)
	out, err := runAgentCmd(t, newAgentStatusCmd)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, agentskill.StatusNotInstalled) {
		t.Errorf("expected not-installed status, got %q", out)
	}
}

func TestAgentStatusUpdateAvailable(t *testing.T) {
	withTempEnv(t)
	tr := &agentskill.Tracker{
		Name:    agentskill.Name,
		Version: agentskill.Version - 1, // older than bundled
		Status:  agentskill.StatusInstalled,
		Targets: []agentskill.TrackedTarget{{Key: "claude", Path: "/x/promptvm"}},
	}
	if err := tr.Save(); err != nil {
		t.Fatal(err)
	}
	out, err := runAgentCmd(t, newAgentStatusCmd)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "Update available") {
		t.Errorf("expected update-available notice, got %q", out)
	}
}

func TestAgentUninstallNothing(t *testing.T) {
	withTempEnv(t)
	out, err := runAgentCmd(t, newAgentUninstallCmd, "--yes")
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !strings.Contains(out, "No installed promptvm agent skill") {
		t.Errorf("expected no-op message, got %q", out)
	}
}

func TestAgentUninstallRemovesAndClears(t *testing.T) {
	withTempEnv(t)
	installed := agentskill.InstallBestEffort(agentskill.ScopeUser, agentskill.AllTargets())
	if len(installed) == 0 {
		t.Fatal("setup install produced no targets")
	}
	tr := &agentskill.Tracker{Name: agentskill.Name, Version: agentskill.Version, Status: agentskill.StatusInstalled}
	for _, it := range installed {
		tr.Targets = append(tr.Targets, agentskill.TrackedTarget(it))
	}
	if err := tr.Save(); err != nil {
		t.Fatal(err)
	}

	out, err := runAgentCmd(t, newAgentUninstallCmd, "--yes")
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !strings.Contains(out, "Removed promptvm agent skill") {
		t.Errorf("expected removal summary, got %q", out)
	}
	if ex, _ := agentskill.Exists(); ex {
		t.Error("marker should be cleared after uninstall")
	}
	for _, it := range installed {
		if _, err := os.Stat(it.Path); !os.IsNotExist(err) {
			t.Errorf("folder %s should be gone, err=%v", it.Path, err)
		}
	}
}

func TestMaybeAutoInstallHappyPathIdempotent(t *testing.T) {
	withTempEnv(t)
	t.Setenv("PROMPTVM_NO_AGENT_SKILL", "") // re-enable for this test

	maybeAutoInstallAgentSkill(promptsCmd) // a non-skipped top-level command

	ex, err := agentskill.Exists()
	if err != nil || !ex {
		t.Fatalf("marker should exist after auto-install: %v", err)
	}
	tr, _ := agentskill.LoadTracker()
	if tr == nil || tr.Status != agentskill.StatusInstalled || len(tr.Targets) != 2 {
		t.Fatalf("unexpected tracker after auto-install: %+v", tr)
	}
	for _, target := range tr.Targets {
		if _, err := os.Stat(filepath.Join(target.Path, "SKILL.md")); err != nil {
			t.Errorf("%s skill not written: %v", target.Key, err)
		}
	}

	// Second run is idempotent: marker present → early return, no change.
	maybeAutoInstallAgentSkill(promptsCmd)
	tr2, _ := agentskill.LoadTracker()
	if len(tr2.Targets) != 2 || tr2.InstalledAt != tr.InstalledAt {
		t.Errorf("second auto-install run mutated state: %+v", tr2)
	}
}

func TestMaybeAutoInstallSkipsSkippedCommands(t *testing.T) {
	withTempEnv(t)
	t.Setenv("PROMPTVM_NO_AGENT_SKILL", "")

	for _, c := range []*cobra.Command{versionCmd, completionCmd, agentCmd} {
		maybeAutoInstallAgentSkill(c)
		if ex, _ := agentskill.Exists(); ex {
			t.Errorf("command %q should not trigger auto-install", c.Name())
			_ = agentskill.Clear()
		}
	}
}

func TestMaybeAutoInstallOptOut(t *testing.T) {
	withTempEnv(t)
	t.Setenv("PROMPTVM_NO_AGENT_SKILL", "1")

	maybeAutoInstallAgentSkill(promptsCmd)
	if ex, _ := agentskill.Exists(); ex {
		t.Error("opt-out env must prevent auto-install (no marker)")
	}
}

func TestTopLevelName(t *testing.T) {
	// Real wired tree: a deeply-nested subcommand resolves to its top-level family.
	c, _, err := rootCmd.Find([]string{"prompts", "get"})
	if err != nil {
		t.Fatalf("find prompts get: %v", err)
	}
	if got := topLevelName(c); got != "prompts" {
		t.Errorf("topLevelName(prompts get) = %q, want prompts", got)
	}
	// agent install (two deep) → "agent".
	ai, _, err := rootCmd.Find([]string{"agent", "install"})
	if err != nil {
		t.Fatalf("find agent install: %v", err)
	}
	if got := topLevelName(ai); got != "agent" {
		t.Errorf("topLevelName(agent install) = %q, want agent", got)
	}
	// The root command returns its own name.
	if got := topLevelName(rootCmd); got != rootCmd.Name() {
		t.Errorf("topLevelName(root) = %q, want %q", got, rootCmd.Name())
	}
}
