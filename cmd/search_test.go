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

// TestSearchCommandRegistered verifies the top-level `search` command is
// wired onto root.
func TestSearchCommandRegistered(t *testing.T) {
	got := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "search" {
			got = true
			break
		}
	}
	if !got {
		t.Fatal("root command 'search' is not registered")
	}
}

// TestSearchFlags covers US-002: the documented flag surface must exist.
func TestSearchFlags(t *testing.T) {
	cmd := newSearchCmd()
	for _, name := range []string{"org", "workspace", "kind", "limit"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("search command missing flag --%s", name)
		}
	}
}

// canned upstream payload — matches the Fern-generated
// SearchOrganizationResponse shape exactly.
const fakeSearchBody = `{
  "query": "parity",
  "ranking": "keyword",
  "took_ms": 4,
  "total_estimate": 2,
  "results": [
    {
      "kind": "prompt",
      "id": "pmt_aaa",
      "title": "Parity Prompt One",
      "description": "first hit",
      "workspace_id": "ws_111",
      "directory_id": null,
      "updated_at": "2026-05-09T12:00:00Z",
      "score": 0.91,
      "highlights": []
    },
    {
      "kind": "prompt",
      "id": "pmt_bbb",
      "title": "Parity Prompt Two",
      "workspace_id": "ws_222",
      "directory_id": null,
      "updated_at": "2026-05-09T12:00:00Z",
      "score": 0.42,
      "highlights": []
    }
  ],
  "next_cursor": null
}`

func startSearchServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/search") {
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

// runSearchCmd is the shared scaffold the search-tests use to invoke the
// cobra command directly. It seeds env-vars + flags so the resolver and
// output formatter both see what they expect.
func runSearchCmd(t *testing.T, srvURL, format string, args []string) (string, error) {
	t.Helper()
	withTestEnv(t, srvURL)

	cmd := newSearchCmd()
	cmd.SetContext(context.Background())
	cmd.Flags().StringP("output", "o", format, "Output format")
	cmd.Flags().Bool("compact", false, "compact json")
	cmd.Flags().Bool("no-header", false, "no header")
	// `--wide` is now declared natively on the search command (F7
	// review follow-up); the test harness no longer needs to register
	// a stand-in.

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.ParseFlags(args); err != nil {
		return out.String(), err
	}
	if err := cmd.Args(cmd, cmd.Flags().Args()); err != nil {
		return out.String(), err
	}
	if err := cmd.RunE(cmd, cmd.Flags().Args()); err != nil {
		return out.String(), err
	}
	return out.String(), nil
}

// TestSearch_HappyPath_JSON exercises the JSON output path against a stub
// SDK server. Covers US-002 + US-006 (json parses).
func TestSearch_HappyPath_JSON(t *testing.T) {
	srv := startSearchServer(t, 200, fakeSearchBody)

	out, err := runSearchCmd(t, srv.URL, "json", []string{"--org", "org_test", "parity"})
	if err != nil {
		t.Fatalf("RunE: %v\n%s", err, out)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("JSON output did not parse: %v\n%s", err, out)
	}
	results, ok := decoded["results"].([]any)
	if !ok || len(results) != 2 {
		t.Fatalf("expected 2 results, got %v", decoded["results"])
	}
}

// TestSearch_Table verifies the table renderer emits the documented
// columns including the SDK's actual `score` field name. Covers US-002
// "match score exactly".
func TestSearch_Table(t *testing.T) {
	srv := startSearchServer(t, 200, fakeSearchBody)

	out, err := runSearchCmd(t, srv.URL, "table", []string{"--org", "org_test", "parity"})
	if err != nil {
		t.Fatalf("RunE: %v\n%s", err, out)
	}

	for _, col := range []string{"NAME", "KIND", "WORKSPACE", "SCORE", "ID"} {
		if !strings.Contains(out, col) {
			t.Errorf("table output missing column %q:\n%s", col, out)
		}
	}
	for _, want := range []string{"Parity Prompt One", "pmt_aaa", "ws_111", "0.9100"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q:\n%s", want, out)
		}
	}
}

// TestSearch_MissingOrg covers the error case in US-002: no --org and no
// profile-default org should produce a clear error naming both options.
func TestSearch_MissingOrg(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PROMPTVM_PUBLIC_KEY", "pk_test000000000000000000000000000000000000")
	t.Setenv("PROMPTVM_SECRET_KEY", "sk_test111111111111111111111111111111111111")

	cmd := newSearchCmd()
	cmd.SetContext(context.Background())
	cmd.Flags().StringP("output", "o", "table", "Output format")
	cmd.Flags().Bool("compact", false, "compact json")
	cmd.Flags().Bool("no-header", false, "no header")

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.ParseFlags([]string{"hello"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	err := cmd.RunE(cmd, cmd.Flags().Args())
	if err == nil {
		t.Fatal("expected error when neither --org nor profile org is set")
	}
	msg := err.Error()
	if !strings.Contains(msg, "--org") || !strings.Contains(msg, "organization") {
		t.Errorf("error should mention --org and organization, got: %v", err)
	}
}

// TestSearch_MalformedKind verifies an invalid --kind flag short-circuits.
func TestSearch_MalformedKind(t *testing.T) {
	srv := startSearchServer(t, 200, fakeSearchBody)
	_, err := runSearchCmd(t, srv.URL, "json", []string{
		"--org", "org_test", "--kind", "not-a-kind", "parity",
	})
	if err == nil {
		t.Fatal("expected error for malformed --kind")
	}
}

// TestSearch_SDKErrorPropagates exercises FR-3.
func TestSearch_SDKErrorPropagates(t *testing.T) {
	srv := startSearchServer(t, 500, `{"error":"boom"}`)
	_, err := runSearchCmd(t, srv.URL, "json", []string{"--org", "org_test", "parity"})
	if err == nil {
		t.Fatal("expected SDK error to propagate, got nil")
	}
}

// TestSearch_RequiresQuery covers the "missing required argument" branch.
func TestSearch_RequiresQuery(t *testing.T) {
	cmd := newSearchCmd()
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Error("expected ExactArgs(1) to reject missing query argument")
	}
}

// TestSearch_WideSkipsTitleTruncation pins the --wide flag's behaviour.
// Default rendering caps the NAME column at 60 chars; --wide must
// surface the full title verbatim so users don't confuse two results
// that share a 60-char prefix.
func TestSearch_WideSkipsTitleTruncation(t *testing.T) {
	const longBody = `{
	  "query": "long",
	  "ranking": "keyword",
	  "took_ms": 1,
	  "total_estimate": 1,
	  "results": [
	    {
	      "kind": "prompt",
	      "id": "pmt_long",
	      "title": "A title that is deliberately longer than sixty characters so we can see truncation in action",
	      "workspace_id": "ws_long",
	      "directory_id": null,
	      "updated_at": "2026-05-09T12:00:00Z",
	      "score": 0.5,
	      "highlights": []
	    }
	  ],
	  "next_cursor": null
	}`
	srv := startSearchServer(t, 200, longBody)

	// Default (no --wide): title is truncated.
	defaultOut, err := runSearchCmd(t, srv.URL, "table", []string{"--org", "org_test", "long"})
	if err != nil {
		t.Fatalf("default RunE: %v\n%s", err, defaultOut)
	}
	if strings.Contains(defaultOut, "truncation in action") {
		t.Errorf("default output should truncate the title, but the tail is present:\n%s", defaultOut)
	}

	// With --wide, the full title is present.
	wideOut, err := runSearchCmd(t, srv.URL, "table", []string{"--org", "org_test", "--wide", "long"})
	if err != nil {
		t.Fatalf("--wide RunE: %v\n%s", err, wideOut)
	}
	if !strings.Contains(wideOut, "truncation in action") {
		t.Errorf("--wide output should include the full title, got:\n%s", wideOut)
	}
}
