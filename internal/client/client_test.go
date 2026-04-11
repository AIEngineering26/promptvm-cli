package client

import (
	"testing"

	"github.com/spf13/cobra"
)

func newRootCmdWithFlags() *cobra.Command {
	root := &cobra.Command{Use: "promptvm"}
	root.Flags().String("api-key", "", "")
	root.Flags().String("base-url", "", "")
	return root
}

func TestResolveAPIKey_Flag(t *testing.T) {
	t.Setenv("PROMPTVM_API_KEY", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	cmd := newRootCmdWithFlags()
	_ = cmd.Flags().Set("api-key", "flag_key")
	got, err := resolveToken(cmd)
	if err != nil {
		t.Fatalf("resolveToken: %v", err)
	}
	if got != "flag_key" {
		t.Errorf("resolveToken = %q, want flag_key", got)
	}
}

func TestResolveAPIKey_Env(t *testing.T) {
	t.Setenv("PROMPTVM_API_KEY", "env_key")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	cmd := newRootCmdWithFlags()
	got, err := resolveToken(cmd)
	if err != nil {
		t.Fatalf("resolveToken: %v", err)
	}
	if got != "env_key" {
		t.Errorf("resolveToken = %q, want env_key", got)
	}
}

func TestResolveAPIKey_Missing(t *testing.T) {
	t.Setenv("PROMPTVM_API_KEY", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	cmd := newRootCmdWithFlags()
	if _, err := resolveToken(cmd); err == nil {
		t.Error("expected error when no API key is configured")
	}
}

func TestResolveBaseURL_Default(t *testing.T) {
	t.Setenv("PROMPTVM_BASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	cmd := newRootCmdWithFlags()
	got := resolveBaseURL(cmd)
	if got != defaultBaseURL {
		t.Errorf("resolveBaseURL = %q, want %q", got, defaultBaseURL)
	}
}

func TestResolveBaseURL_FlagBeatsEnv(t *testing.T) {
	t.Setenv("PROMPTVM_BASE_URL", "http://env")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	cmd := newRootCmdWithFlags()
	_ = cmd.Flags().Set("base-url", "http://flag")
	got := resolveBaseURL(cmd)
	if got != "http://flag" {
		t.Errorf("resolveBaseURL = %q, want flag", got)
	}
}
