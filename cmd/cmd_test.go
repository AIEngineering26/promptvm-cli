package cmd

import (
	"strings"
	"testing"
)

// TestRootCommandWired ensures every top-level command we advertise has been
// registered on the root command. This catches accidental removal of `init()`
// registrations or missing AddCommand calls.
func TestRootCommandWired(t *testing.T) {
	want := []string{
		"auth",
		"profile",
		"config",
		"prompts",
		"workspaces",
		"orgs",
		"collections",
		"directories",
		"templates",
		"marketplace",
		"resources",
		"share",
		"apikeys",
		"contexts",
		"search",
		"completion",
		"version",
	}

	got := make(map[string]bool, len(rootCmd.Commands()))
	for _, c := range rootCmd.Commands() {
		got[c.Name()] = true
	}

	for _, name := range want {
		if !got[name] {
			t.Errorf("root command %q is not registered", name)
		}
	}
}

// TestPromptsSubcommands verifies all documented prompts subcommands exist.
func TestPromptsSubcommands(t *testing.T) {
	want := []string{
		"list", "create", "get", "update", "delete",
		"resolve", "export", "fork", "move", "rollback",
		"references", "dependents", "versions",
	}

	got := make(map[string]bool)
	for _, c := range promptsCmd.Commands() {
		got[c.Name()] = true
	}

	for _, name := range want {
		if !got[name] {
			t.Errorf("prompts subcommand %q missing", name)
		}
	}
}

// TestWorkspacesSubcommands verifies workspace CRUD + transfer/pin/unpin exist.
func TestWorkspacesSubcommands(t *testing.T) {
	want := []string{"list", "create", "get", "update", "delete", "transfer", "pin", "unpin"}
	got := make(map[string]bool)
	for _, c := range workspacesCmd.Commands() {
		got[c.Name()] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("workspaces subcommand %q missing", name)
		}
	}
}

// TestAliases checks that common short aliases resolve.
func TestAliases(t *testing.T) {
	cases := map[string]string{
		"workspaces":  "ws",
		"collections": "col",
		"directories": "dirs",
		"templates":   "tpl",
		"resources":   "res",
	}

	for long, short := range cases {
		c, _, err := rootCmd.Find([]string{short})
		if err != nil {
			t.Errorf("alias %q for %q not found: %v", short, long, err)
			continue
		}
		if c.Name() != long {
			t.Errorf("alias %q resolved to %q, want %q", short, c.Name(), long)
		}
	}
}

// TestVersionOutputIncludesVersion ensures the version command prints
// something non-empty and includes the "promptvm" identifier.
func TestVersionOutputIncludesVersion(t *testing.T) {
	buf := &strings.Builder{}
	versionCmd.SetOut(buf)
	if err := versionCmd.RunE(versionCmd, nil); err != nil {
		t.Fatalf("version RunE: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("version command produced no output")
	}
	if !strings.Contains(buf.String(), "promptvm") {
		t.Errorf("version output missing 'promptvm': %q", buf.String())
	}
}
