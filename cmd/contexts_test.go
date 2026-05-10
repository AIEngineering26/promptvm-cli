package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestContextsCommandRegistered verifies the top-level `contexts` command
// is wired onto the root command.
func TestContextsCommandRegistered(t *testing.T) {
	got := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "contexts" {
			got = true
			break
		}
	}
	if !got {
		t.Fatal("root command 'contexts' is not registered")
	}
}

// TestContextsListSubcommand verifies the `list` subcommand under contexts.
func TestContextsListSubcommand(t *testing.T) {
	got := false
	for _, c := range contextsCmd.Commands() {
		if c.Name() == "list" {
			got = true
			break
		}
	}
	if !got {
		t.Fatal("contexts subcommand 'list' missing")
	}
}

// TestContextsListHelpMentionsKinds covers US-001 acceptance: help text
// must describe the prompt and skill kinds.
func TestContextsListHelpMentionsKinds(t *testing.T) {
	help := contextsCmd.Long
	for _, must := range []string{"prompt", "skill"} {
		if !strings.Contains(help, must) {
			t.Errorf("contexts long help missing %q", must)
		}
	}
}

// canned upstream payload — matches the Fern-generated
// ListContextKindsResponse shape exactly so the SDK unmarshal succeeds.
const fakeContextKindsBody = `{
  "kinds": [
    {
      "name": "prompt",
      "description": "Reusable LLM prompt with versions and variables.",
      "default_is_public": false,
      "metadata_schema": {},
      "content_spec": {},
      "file_spec": {}
    },
    {
      "name": "skill",
      "description": "Packaged agent skill (instructions plus assets) for distribution.",
      "default_is_public": true,
      "metadata_schema": {},
      "content_spec": {},
      "file_spec": {}
    }
  ],
  "agent_skills_version": "v1"
}`

// startContextsKindsServer spins up an httptest server that returns the
// canned payload for GET /v1/contexts/kinds and 404s everything else.
// Returns the base URL.
func startContextsKindsServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "contexts/types") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// withTestEnv wires test credentials + a base URL through the env vars the
// CLI's credential resolver inspects. Called as a t.Helper so the calling
// test gets clean teardown.
func withTestEnv(t *testing.T, baseURL string) {
	t.Helper()
	t.Setenv("PROMPTVM_PUBLIC_KEY", "pk_test000000000000000000000000000000000000")
	t.Setenv("PROMPTVM_SECRET_KEY", "sk_test111111111111111111111111111111111111")
	t.Setenv("PROMPTVM_BASE_URL", baseURL)
	// Don't let the user's profile/config bleed into the test.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

// TestContextsList_HappyPath_JSON exercises the JSON output path end to end:
// stub SDK server, real cobra command, verify the output parses and has the
// expected kinds. Covers US-001 + US-006.
func TestContextsList_HappyPath_JSON(t *testing.T) {
	srv := startContextsKindsServer(t, 200, fakeContextKindsBody)
	withTestEnv(t, srv.URL)

	cmd := newContextsListCmd()
	cmd.SetContext(context.Background())
	// Required for output.Format to read --output without panicking.
	cmd.Flags().StringP("output", "o", "json", "Output format")
	cmd.Flags().Bool("compact", false, "compact json")
	cmd.Flags().Bool("no-header", false, "no header")
	cmd.Flags().Bool("wide", false, "wide")

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("JSON output did not parse: %v\n%s", err, out.String())
	}
	kinds, ok := decoded["kinds"].([]any)
	if !ok || len(kinds) != 2 {
		t.Fatalf("expected 2 kinds in payload, got %v", decoded["kinds"])
	}
}

// TestContextsList_Table verifies the table renderer emits the documented
// columns. Covers US-001 (default table) + US-006 (output formatting test).
func TestContextsList_Table(t *testing.T) {
	srv := startContextsKindsServer(t, 200, fakeContextKindsBody)
	withTestEnv(t, srv.URL)

	cmd := newContextsListCmd()
	cmd.SetContext(context.Background())
	// Default --output is table.
	cmd.Flags().StringP("output", "o", "table", "Output format")
	cmd.Flags().Bool("compact", false, "compact json")
	cmd.Flags().Bool("no-header", false, "no header")
	cmd.Flags().Bool("wide", false, "wide")

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	body := out.String()
	for _, col := range []string{"NAME", "DEFAULT_PUBLIC", "DESCRIPTION"} {
		if !strings.Contains(body, col) {
			t.Errorf("table output missing column %q:\n%s", col, body)
		}
	}
	for _, kind := range []string{"prompt", "skill"} {
		if !strings.Contains(body, kind) {
			t.Errorf("table output missing kind row %q:\n%s", kind, body)
		}
	}
}

// TestContextsList_SDKErrorPropagates exercises FR-3: SDK errors must
// surface to the caller.
func TestContextsList_SDKErrorPropagates(t *testing.T) {
	srv := startContextsKindsServer(t, 500, `{"error":"boom"}`)
	withTestEnv(t, srv.URL)

	cmd := newContextsListCmd()
	cmd.SetContext(context.Background())
	cmd.Flags().StringP("output", "o", "json", "Output format")
	cmd.Flags().Bool("compact", false, "compact json")
	cmd.Flags().Bool("no-header", false, "no header")
	cmd.Flags().Bool("wide", false, "wide")

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.RunE(cmd, nil); err == nil {
		t.Fatal("expected SDK error to propagate, got nil")
	}
}

// TestContextsList_RejectsArgs covers the "malformed input" branch — the
// list subcommand takes no positional args.
func TestContextsList_RejectsArgs(t *testing.T) {
	cmd := newContextsListCmd()
	if err := cmd.Args(cmd, []string{"unexpected"}); err == nil {
		t.Error("expected NoArgs validator to reject extra positional arg")
	}
}
