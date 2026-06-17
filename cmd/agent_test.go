package cmd

import "testing"

// TestAgentSubcommands verifies the agent command tree is wired up.
func TestAgentSubcommands(t *testing.T) {
	want := []string{"install", "uninstall", "status"}
	got := make(map[string]bool)
	for _, c := range agentCmd.Commands() {
		got[c.Name()] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("agent subcommand %q missing", name)
		}
	}
}

// TestAgentRegistered ensures `agent` is a top-level command.
func TestAgentRegistered(t *testing.T) {
	if _, _, err := rootCmd.Find([]string{"agent"}); err != nil {
		t.Fatalf("agent command not registered: %v", err)
	}
}

func TestResolveScope(t *testing.T) {
	cases := map[string]bool{"": true, "user": true, "project": true, "bogus": false}
	for in, ok := range cases {
		_, err := resolveScope(in)
		if ok && err != nil {
			t.Errorf("resolveScope(%q) unexpected error: %v", in, err)
		}
		if !ok && err == nil {
			t.Errorf("resolveScope(%q) expected error", in)
		}
	}
}

func TestResolveTargets(t *testing.T) {
	if ts, err := resolveTargets(""); err != nil || len(ts) != 2 {
		t.Errorf("resolveTargets(\"\") = %v, %v", ts, err)
	}
	if ts, err := resolveTargets("all"); err != nil || len(ts) != 2 {
		t.Errorf("resolveTargets(all) = %v, %v", ts, err)
	}
	if ts, err := resolveTargets("claude"); err != nil || len(ts) != 1 {
		t.Errorf("resolveTargets(claude) = %v, %v", ts, err)
	}
	if _, err := resolveTargets("bogus"); err == nil {
		t.Error("resolveTargets(bogus) expected error")
	}
}
