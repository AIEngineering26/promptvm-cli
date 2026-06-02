package cmd

import (
	"testing"
)

// TestAuthSubcommandsRegistered verifies all auth subcommands are wired
// onto the auth command tree.
func TestAuthSubcommandsRegistered(t *testing.T) {
	want := []string{"login", "logout", "status", "sessions"}
	got := make(map[string]bool)
	for _, c := range authCmd.Commands() {
		got[c.Name()] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("auth subcommand %q missing", name)
		}
	}
}

// TestAuthLoginFlags verifies that the login command exposes the flags
// the SSO/OAuth refactor introduced.
func TestAuthLoginFlags(t *testing.T) {
	want := []string{"api-key", "profile", "base-url", "device", "no-browser", "app-url"}
	for _, name := range want {
		if authLoginCmd.Flags().Lookup(name) == nil {
			t.Errorf("auth login missing flag --%s", name)
		}
	}
}

func TestDeriveAppURL(t *testing.T) {
	cases := []struct {
		in, out string
	}{
		{"https://dev-api.promptvm.ai", "https://dev-app.promptvm.ai"},
		{"https://staging-api.promptvm.ai", "https://staging-app.promptvm.ai"},
		{"https://api.promptvm.com", "https://app.promptvm.com"},
		{"https://api.staging.promptvm.com", "https://app.staging.promptvm.com"},
		{"https://api.promptvm.com/", "https://app.promptvm.com"},
		{"https://example.com", ""}, // unknown hostname → no derivation
		{"", ""},
	}
	for _, tc := range cases {
		got := deriveAppURL(tc.in)
		if got != tc.out {
			t.Errorf("deriveAppURL(%q) = %q, want %q", tc.in, got, tc.out)
		}
	}
}

func TestAuthSessionsSubcommands(t *testing.T) {
	want := []string{"list", "revoke"}
	got := make(map[string]bool)
	for _, c := range authSessionsCmd.Commands() {
		got[c.Name()] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("auth sessions subcommand %q missing", name)
		}
	}
}
