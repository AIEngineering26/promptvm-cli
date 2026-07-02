package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AIEngineering26/promptvm-cli/internal/api"
	"github.com/AIEngineering26/promptvm-cli/internal/capture"
	"github.com/AIEngineering26/promptvm-cli/internal/config"
	"github.com/AIEngineering26/promptvm-cli/internal/manifest"
	"github.com/AIEngineering26/promptvm-cli/internal/spool"
	"github.com/spf13/cobra"
)

const testWsUUID = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"

func TestIsUUID(t *testing.T) {
	cases := map[string]bool{
		testWsUUID:                        true,
		strings.ToUpper(testWsUUID):       true,
		"demo":                            false,
		"My Workspace":                    false,
		"3f2504e04f8941d39a0c0305e82c330": false, // no dashes
		"":                                false,
	}
	for in, want := range cases {
		if got := isUUID(in); got != want {
			t.Errorf("isUUID(%q) = %t, want %t", in, got, want)
		}
	}
}

// workspacesStub serves GET /api/v1/me/workspaces with a fixed set.
func workspacesStub(t *testing.T) *api.Caller {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/me/workspaces"):
			_, _ = w.Write([]byte(`{"data":[
				{"id":"` + testWsUUID + `","name":"Demo Workspace","slug":"demo","isDefault":true},
				{"id":"7c9e6679-7425-40de-944b-e07fc1f90ae7","name":"Ops","slug":"ops","isDefault":false},
				{"id":"550e8400-e29b-41d4-a716-446655440000","name":"Dup","slug":"dup-a","isDefault":false},
				{"id":"6ba7b810-9dad-11d1-80b4-00c04fd430c8","name":"Dup","slug":"dup-b","isDefault":false}
			]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &api.Caller{BaseURL: srv.URL, PublicKey: "pk_t", SecretKey: "sk_t"}
}

// TestNormalizeWorkspace covers the contract: names/slugs are resolved to
// UUIDs case-insensitively; ambiguity and misses are errors that list the
// available workspaces; UUIDs pass through.
func TestNormalizeWorkspace(t *testing.T) {
	caller := workspacesStub(t)

	// UUID passthrough (with name backfill).
	id, name, err := normalizeWorkspace(caller, testWsUUID)
	if err != nil || id != testWsUUID || name != "Demo Workspace" {
		t.Errorf("uuid passthrough: %q %q %v", id, name, err)
	}

	// Slug match.
	id, _, err = normalizeWorkspace(caller, "demo")
	if err != nil || id != testWsUUID {
		t.Errorf("slug: %q %v", id, err)
	}

	// Case-insensitive name match.
	id, _, err = normalizeWorkspace(caller, "demo workspace")
	if err != nil || id != testWsUUID {
		t.Errorf("name (case-insensitive): %q %v", id, err)
	}

	// Not found → error listing available workspaces.
	_, _, err = normalizeWorkspace(caller, "nope")
	if err == nil || !strings.Contains(err.Error(), "Demo Workspace") {
		t.Errorf("not-found error should list workspaces: %v", err)
	}

	// Ambiguous name → error.
	_, _, err = normalizeWorkspace(caller, "Dup")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("ambiguous error expected: %v", err)
	}

	// A UUID absent from a successfully fetched listing (e.g. a stale
	// defaults.workspace from another org) → error, not silent trust.
	_, _, err = normalizeWorkspace(caller, "11111111-2222-4333-8444-555555555555")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("unknown UUID should error listing workspaces: %v", err)
	}
}

// TestMintFallbackToWriteScope: a backend 400 scope-enum rejection of
// scopes:["capture"] falls back to a scopes:["write"] key and stores it.
func TestMintFallbackToWriteScope(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	var gotScopes [][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api-keys") {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Name        string   `json:"name"`
			Scopes      []string `json:"scopes"`
			WorkspaceID string   `json:"workspaceId"`
		}
		dec := decodeJSONBody(t, r, &body)
		_ = dec
		gotScopes = append(gotScopes, body.Scopes)
		w.Header().Set("Content-Type", "application/json")
		if len(body.Scopes) == 1 && body.Scopes[0] == "capture" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"bad_request","message":"body/scopes/0 must be equal to one of the allowed values"}`))
			return
		}
		if !strings.Contains(body.Name, "fallback write scope") {
			t.Errorf("fallback key name = %q, want the documented fallback name", body.Name)
		}
		// Write keys are not workspace-bound; newer backends 400 a non-capture
		// mint that carries workspaceId.
		if body.WorkspaceID != "" {
			t.Errorf("fallback mint carried workspaceId %q, want none", body.WorkspaceID)
		}
		_, _ = w.Write([]byte(`{"publicKey":"pk_fb","secretKey":"sk_fb"}`))
	}))
	t.Cleanup(srv.Close)

	caller := &api.Caller{BaseURL: srv.URL, PublicKey: "pk_t", SecretKey: "sk_t"}
	cmd := &cobra.Command{}
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)

	status := mintAndStoreCredential(cmd, caller, testWsUUID)
	if status != credStoredFallback {
		t.Fatalf("status = %q, want %q (stderr: %s)", status, credStoredFallback, errBuf.String())
	}
	if len(gotScopes) != 2 || gotScopes[0][0] != "capture" || gotScopes[1][0] != "write" {
		t.Errorf("mint attempts = %v, want capture then write", gotScopes)
	}
	if !strings.Contains(errBuf.String(), "BROADER") {
		t.Errorf("expected a broader-than-intended warning, got: %s", errBuf.String())
	}
	cred, err := capture.LoadCredential(testWsUUID)
	if err != nil || cred == nil || cred.PublicKey != "pk_fb" {
		t.Errorf("fallback credential not stored: %+v %v", cred, err)
	}
	if cred != nil && cred.Scope != capture.ScopeWrite {
		t.Errorf("fallback credential scope = %q, want %q (doctor uses it to swap in a capture key later)", cred.Scope, capture.ScopeWrite)
	}
}

// TestMintReusesExistingCredential: re-running init/setup must NOT mint a new
// key when a capture credential is already stored for the workspace.
func TestMintReusesExistingCredential(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	mints := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mints++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"publicKey":"pk_dup","secretKey":"sk_dup"}`))
	}))
	t.Cleanup(srv.Close)

	if _, err := capture.SaveCredential(testWsUUID, capture.Credential{PublicKey: "pk_keep", SecretKey: "sk_keep", Scope: capture.ScopeCapture}); err != nil {
		t.Fatal(err)
	}

	caller := &api.Caller{BaseURL: srv.URL, PublicKey: "pk_t", SecretKey: "sk_t"}
	cmd := &cobra.Command{}
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)

	status := mintAndStoreCredential(cmd, caller, testWsUUID)
	if status != credReused {
		t.Fatalf("status = %q, want %q", status, credReused)
	}
	if mints != 0 {
		t.Errorf("minted %d key(s), want 0 (existing credential must be reused)", mints)
	}
	cred, _ := capture.LoadCredential(testWsUUID)
	if cred == nil || cred.PublicKey != "pk_keep" {
		t.Errorf("stored credential replaced: %+v", cred)
	}
}

// TestMintSwapsWriteScopeFallback: when a write-scope fallback credential is
// stored and the backend now accepts the capture scope, init/doctor swap in a
// least-privilege capture key.
func TestMintSwapsWriteScopeFallback(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"publicKey":"pk_cap","secretKey":"sk_cap"}`))
	}))
	t.Cleanup(srv.Close)

	if _, err := capture.SaveCredential(testWsUUID, capture.Credential{PublicKey: "pk_old", SecretKey: "sk_old", Scope: capture.ScopeWrite}); err != nil {
		t.Fatal(err)
	}

	caller := &api.Caller{BaseURL: srv.URL, PublicKey: "pk_t", SecretKey: "sk_t"}
	cmd := &cobra.Command{}
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)

	status := mintAndStoreCredential(cmd, caller, testWsUUID)
	if status != credSwapped {
		t.Fatalf("status = %q, want %q (stderr: %s)", status, credSwapped, errBuf.String())
	}
	cred, _ := capture.LoadCredential(testWsUUID)
	if cred == nil || cred.PublicKey != "pk_cap" || cred.Scope != capture.ScopeCapture {
		t.Errorf("capture credential not swapped in: %+v", cred)
	}
	if !strings.Contains(errBuf.String(), "pk_old") || !strings.Contains(errBuf.String(), "revoke") {
		t.Errorf("expected a revoke-the-old-key note naming pk_old, got: %s", errBuf.String())
	}
}

// TestMintFailurePrintsLoudRemediation: a non-scope failure prints the
// multi-line remediation and reports failed.
func TestMintFailurePrintsLoudRemediation(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	}))
	t.Cleanup(srv.Close)

	caller := &api.Caller{BaseURL: srv.URL, PublicKey: "pk_t", SecretKey: "sk_t"}
	cmd := &cobra.Command{}
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)

	status := mintAndStoreCredential(cmd, caller, testWsUUID)
	if status != credFailed {
		t.Fatalf("status = %q, want %q", status, credFailed)
	}
	out := errBuf.String()
	for _, want := range []string{"Could not mint a capture credential", "promptvm sync doctor", "spool"} {
		if !strings.Contains(out, want) {
			t.Errorf("loud error missing %q:\n%s", want, out)
		}
	}
}

// TestSyncRunFallsBackToConfigDefaultAndRecordsReason: a manifest without a
// workspace uses config defaults.workspace and the spool entry carries a
// reason explaining both the fallback and why it spooled.
func TestSyncRunFallsBackToConfigDefaultAndRecordsReason(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Defaults.Workspace = testWsUUID
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	repoRoot := t.TempDir()
	writeManifestFile(t, repoRoot, `{ "capture": { "enabled": true, "events": ["SessionEnd"], "mode": "summary" } }`)
	tr := writeTranscript(t, repoRoot, "sess-fb")

	in := HookInput{SessionID: "sess-fb", TranscriptPath: tr, Cwd: repoRoot, HookEventName: "SessionEnd"}
	cmd := newSyncRunCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	processHook(cmd, in, "", "", false)

	entries, _ := spool.List()
	if len(entries) != 1 {
		t.Fatalf("expected 1 spooled entry, got %d", len(entries))
	}
	e := entries[0]
	if e.WorkspaceID != testWsUUID {
		t.Errorf("spool workspace = %q, want config default %q", e.WorkspaceID, testWsUUID)
	}
	if !strings.Contains(e.Reason, "defaults.workspace") || !strings.Contains(e.Reason, "no capture credential") {
		t.Errorf("spool reason = %q, want fallback + no-credential explanation", e.Reason)
	}
}

// TestSyncInitAnchorsSettingsAtRepoRoot: init run from a repo SUBDIRECTORY
// must write the manifest AND the Claude settings at the repo root.
func TestSyncInitAnchorsSettingsAtRepoRoot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/api-keys"):
			_, _ = w.Write([]byte(`{"publicKey":"pk_cap","secretKey":"sk_cap"}`))
		case strings.HasSuffix(r.URL.Path, "/me/workspaces"):
			_, _ = w.Write([]byte(`{"data":[{"id":"` + testWsUUID + `","name":"Demo","slug":"demo","isDefault":true}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("PROMPTVM_PUBLIC_KEY", "pk_test000000000000000000000000000000000000")
	t.Setenv("PROMPTVM_SECRET_KEY", "sk_test111111111111111111111111111111111111")
	t.Setenv("PROMPTVM_BASE_URL", srv.URL)

	repo := t.TempDir()
	c := exec.Command("git", "init")
	c.Dir = repo
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	// git resolves the tempdir through symlinks (macOS /var → /private/var);
	// use the same resolution for assertions.
	repoResolved, err := filepath.EvalSymlinks(repo)
	if err != nil {
		repoResolved = repo
	}
	sub := filepath.Join(repo, "nested", "dir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, sub)

	cmd := newSyncInitCmd()
	cmd.SetContext(context.Background())
	cmd.Flags().StringP("output", "o", "table", "")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	_ = cmd.Flags().Set("scope", "project")
	_ = cmd.Flags().Set("workspace", testWsUUID)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("init RunE: %v\n%s", err, out.String())
	}

	// Manifest at the repo root, not the subdir.
	if _, err := os.Stat(filepath.Join(repoResolved, ".promptvm", "config.json")); err != nil {
		t.Errorf("manifest not at repo root: %v", err)
	}
	// Settings anchored at the repo root too (the fixed behavior).
	if _, err := os.Stat(filepath.Join(repoResolved, ".claude", "settings.json")); err != nil {
		t.Errorf("settings.json not at repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sub, ".claude", "settings.json")); err == nil {
		t.Error("settings.json wrongly written in the subdirectory")
	}

	// The default (non-interactive) path must not have prompted: output shows
	// the checklist summary.
	if !strings.Contains(out.String(), "Verify: promptvm sync status") {
		t.Errorf("missing checklist summary:\n%s", out.String())
	}
}

// TestSyncStatusShowsManifestsCredentialAndNextHint verifies the new
// diagnostics lines.
func TestSyncStatusShowsManifestsCredentialAndNextHint(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	repoRoot := t.TempDir()
	writeManifestFile(t, repoRoot, `{ "workspace": "`+testWsUUID+`", "capture": { "enabled": true, "events": ["SessionEnd"] } }`)
	chdir(t, repoRoot)

	cmd := newSyncStatusCmd()
	cmd.SetContext(context.Background())
	cmd.Flags().StringP("output", "o", "table", "")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "manifests:") || !strings.Contains(s, "(found)") || !strings.Contains(s, "(absent)") {
		t.Errorf("manifest consultation lines missing:\n%s", s)
	}
	if !strings.Contains(s, "credential file:") {
		t.Errorf("credential file path missing:\n%s", s)
	}
	// No credential stored → the Next hint points at doctor.
	if !strings.Contains(s, "Next:") || !strings.Contains(s, "sync doctor") {
		t.Errorf("state-specific Next hint missing:\n%s", s)
	}
}

// TestSyncDoctorNormalizesWorkspaceAndRenamesCredential: doctor rewrites a
// slug manifest workspace to the UUID and moves <slug>.env → <uuid>.env.
func TestSyncDoctorNormalizesWorkspaceAndRenamesCredential(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	var ingested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/me/workspaces"):
			_, _ = w.Write([]byte(`{"data":[{"id":"` + testWsUUID + `","name":"Demo","slug":"demo","isDefault":true}]}`))
		case strings.HasSuffix(r.URL.Path, "/api-keys"):
			_, _ = w.Write([]byte(`{"publicKey":"pk_new","secretKey":"sk_new"}`))
		case strings.HasSuffix(r.URL.Path, "/contexts/sessions"):
			var req capture.IngestRequest
			decodeJSONBody(t, r, &req)
			ingested = append(ingested, req.WorkspaceID)
			_, _ = w.Write([]byte(`{"status":"accepted","captureId":"cap-1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("PROMPTVM_PUBLIC_KEY", "pk_test000000000000000000000000000000000000")
	t.Setenv("PROMPTVM_SECRET_KEY", "sk_test111111111111111111111111111111111111")
	t.Setenv("PROMPTVM_BASE_URL", srv.URL)

	repoRoot := t.TempDir()
	writeManifestFile(t, repoRoot, `{ "workspace": "demo", "capture": { "enabled": true, "events": ["SessionEnd"] } }`)
	chdir(t, repoRoot)

	// A credential stored under the slug name must be renamed to the UUID.
	if _, err := capture.SaveCredential("demo", capture.Credential{PublicKey: "pk_old", SecretKey: "sk_old"}); err != nil {
		t.Fatal(err)
	}

	// A capture spooled while the manifest still held the slug must be rekeyed
	// to the UUID during normalization — otherwise it can never flush (its
	// credential file was just renamed to <uuid>.env).
	payload := &capture.IngestRequest{
		WorkspaceID:     "demo",
		ClaudeSessionID: "sess-legacy",
		Source:          "claude-code",
		CaptureMode:     capture.Mode("summary"),
		Summary:         "legacy spooled capture",
	}
	payload.ContentHash = payload.ComputeContentHash()
	if _, err := spool.Add(&spool.Entry{
		ClaudeSessionID: "sess-legacy",
		WorkspaceID:     "demo",
		CaptureMode:     payload.CaptureMode,
		ContentHash:     payload.ContentHash,
		Payload:         payload,
	}); err != nil {
		t.Fatal(err)
	}

	cmd := newSyncDoctorCmd()
	cmd.SetContext(context.Background())
	cmd.Flags().StringP("output", "o", "table", "")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("doctor RunE: %v\n%s", err, out.String())
	}

	// Manifest rewritten to the UUID.
	mPath := filepath.Join(repoRoot, ".promptvm", "config.json")
	m, err := manifest.Read(mPath)
	if err != nil || m == nil {
		t.Fatalf("manifest: %v", err)
	}
	if m.Workspace != testWsUUID {
		t.Errorf("manifest workspace = %q, want %q", m.Workspace, testWsUUID)
	}
	// Credential now lives under the UUID filename.
	cred, err := capture.LoadCredential(testWsUUID)
	if err != nil || cred == nil || cred.PublicKey != "pk_old" {
		t.Errorf("credential not renamed to the UUID: %+v %v", cred, err)
	}
	if !strings.Contains(out.String(), "fixed") {
		t.Errorf("doctor output missing a fixed check:\n%s", out.String())
	}
	// The spooled capture was rekeyed to the UUID and flushed.
	if len(ingested) != 1 || ingested[0] != testWsUUID {
		t.Errorf("spooled capture not rekeyed+flushed under the UUID: ingested=%v", ingested)
	}
	if entries, _ := spool.List(); len(entries) != 0 {
		t.Errorf("spool not empty after doctor: %d entries remain", len(entries))
	}
}

// TestSetupPrintAgentPrompt pins the canonical copy-paste agent prompt block.
func TestSetupPrintAgentPrompt(t *testing.T) {
	cmd := newSetupCmd()
	cmd.SetContext(context.Background())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	_ = cmd.Flags().Set("print-agent-prompt", "true")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	s := out.String()
	for _, want := range []string{
		"Set up PromptVM for me in this environment:",
		"npm install -g @promptvm/cli",
		"promptvm auth login --device",
		"promptvm setup --yes",
		"promptvm sync status",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("agent prompt missing %q:\n%s", want, s)
		}
	}
}

// TestMCPCommandsRegistered verifies the new command families are wired.
func TestMCPCommandsRegistered(t *testing.T) {
	names := map[string]bool{}
	for _, c := range rootCmd.Commands() {
		names[c.Name()] = true
	}
	if !names["mcp"] {
		t.Error("mcp command not registered")
	}
	if !names["setup"] {
		t.Error("setup command not registered")
	}
	sub := map[string]bool{}
	for _, c := range mcpCmd.Commands() {
		sub[c.Name()] = true
	}
	for _, want := range []string{"install", "print"} {
		if !sub[want] {
			t.Errorf("mcp subcommand %q missing", want)
		}
	}
}

// TestMCPPrintSnippets checks `mcp print` emits both client snippets in the
// contract formats without writing anything.
func TestMCPPrintSnippets(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	t.Setenv("PROMPTVM_BASE_URL", "https://dev-api.promptvm.ai")

	cmd := newMCPPrintCmd()
	cmd.SetContext(context.Background())
	cmd.Flags().StringP("output", "o", "table", "")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "claude mcp add --transport http promptvm https://dev-mcp.promptvm.ai/mcp") {
		t.Errorf("claude snippet missing/wrong:\n%s", s)
	}
	if !strings.Contains(s, "[mcp_servers.promptvm]") || !strings.Contains(s, `url = "https://dev-mcp.promptvm.ai/mcp"`) {
		t.Errorf("codex snippet missing/wrong:\n%s", s)
	}
}

// decodeJSONBody decodes a request body into v.
func decodeJSONBody(t *testing.T, r *http.Request, v any) bool {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		t.Fatalf("decoding request body: %v", err)
	}
	return true
}
