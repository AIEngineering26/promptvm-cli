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

	"github.com/AIEngineering26/promptvm-cli/internal/capture"
	"github.com/AIEngineering26/promptvm-cli/internal/hooks"
	"github.com/AIEngineering26/promptvm-cli/internal/manifest"
	"github.com/AIEngineering26/promptvm-cli/internal/spool"
)

// TestSyncCommandRegistered verifies the `sync` group is wired onto root with
// all five subcommands (DX-1: group is "sync", not "context").
func TestSyncCommandRegistered(t *testing.T) {
	var found bool
	for _, c := range rootCmd.Commands() {
		if c.Name() == "sync" {
			found = true
		}
	}
	if !found {
		t.Fatal("root command 'sync' is not registered")
	}

	want := []string{"init", "run", "status", "push", "export"}
	got := map[string]bool{}
	for _, c := range syncCmd.Commands() {
		got[c.Name()] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("sync subcommand %q missing", name)
		}
	}
}

func TestScopeMappings(t *testing.T) {
	cases := []struct {
		scope    string
		manifest manifest.Scope
		hooks    hooks.Scope
	}{
		{"local", manifest.ScopeLocal, hooks.ScopeLocal},
		{"project", manifest.ScopeProject, hooks.ScopeProject},
		{"user", manifest.ScopeUser, hooks.ScopeUser},
	}
	for _, c := range cases {
		m, err := scopeToManifest(c.scope)
		if err != nil || m != c.manifest {
			t.Errorf("scopeToManifest(%q) = %v, %v", c.scope, m, err)
		}
		h, err := scopeToHooks(c.scope)
		if err != nil || h != c.hooks {
			t.Errorf("scopeToHooks(%q) = %v, %v", c.scope, h, err)
		}
	}
	if _, err := scopeToManifest("bogus"); err == nil {
		t.Error("expected error for bogus scope")
	}
}

func TestWithSessionStartAlwaysIncludesReconcile(t *testing.T) {
	got := withSessionStart([]string{"SessionEnd", "PreCompact"})
	if !contains(got, "SessionStart") {
		t.Errorf("SessionStart not added: %v", got)
	}
	// Idempotent: SessionStart already present is not duplicated.
	got2 := withSessionStart([]string{"SessionStart", "SessionEnd"})
	n := 0
	for _, e := range got2 {
		if e == "SessionStart" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("SessionStart duplicated: %v", got2)
	}
}

func TestPickDefaultWorkspace(t *testing.T) {
	items := []workspaceItem{
		{ID: "ws_a"},
		{ID: "ws_b", IsDefault: true},
	}
	if pickDefaultWorkspace(items) != "ws_b" {
		t.Errorf("expected ws_b (isDefault)")
	}
	if pickDefaultWorkspace([]workspaceItem{{ID: "ws_only"}}) != "ws_only" {
		t.Errorf("expected first when none default")
	}
	if pickDefaultWorkspace(nil) != "" {
		t.Errorf("expected empty for no workspaces")
	}
}

func TestParseEvents(t *testing.T) {
	got := parseEvents("SessionEnd, PreCompact ,")
	if len(got) != 2 || got[0] != "SessionEnd" || got[1] != "PreCompact" {
		t.Errorf("parseEvents = %v", got)
	}
	if parseEvents("  ") != nil {
		t.Errorf("blank should be nil")
	}
}

// TestSyncRunNoOpWhenEventNotSelected verifies the uploader is a no-op (no spool
// write, no upload) when the event is not in the manifest capture set.
func TestSyncRunNoOpWhenEventNotSelected(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	repoRoot := t.TempDir()
	writeManifestFile(t, repoRoot, `{
	  "workspace": "ws_1",
	  "capture": { "enabled": true, "events": ["SessionEnd"] }
	}`)

	in := HookInput{
		SessionID:     "sess-x",
		Cwd:           repoRoot,
		HookEventName: "PreCompact", // not selected
	}
	cmd := newSyncRunCmd()
	cmd.SetContext(context.Background())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	processHook(cmd, in, "", "", false)

	n, _ := spool.Count()
	if n != 0 {
		t.Errorf("expected no spool write for unselected event, got %d", n)
	}
}

// TestSyncRunDisabledIsNoOp verifies capture.enabled=false short-circuits.
func TestSyncRunDisabledIsNoOp(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	repoRoot := t.TempDir()
	writeManifestFile(t, repoRoot, `{ "workspace": "ws_1", "capture": { "enabled": false, "events": ["SessionEnd"] } }`)

	tr := writeTranscript(t, repoRoot, "sess-y")
	in := HookInput{SessionID: "sess-y", TranscriptPath: tr, Cwd: repoRoot, HookEventName: "SessionEnd"}
	cmd := newSyncRunCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	processHook(cmd, in, "", "", false)
	n, _ := spool.Count()
	if n != 0 {
		t.Errorf("disabled capture should not spool, got %d", n)
	}
}

// TestSyncRunSpoolsWhenNoCredential verifies a selected event with no stored
// capture credential lands in the spool (never blocks, never errors).
func TestSyncRunSpoolsWhenNoCredential(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	repoRoot := t.TempDir()
	writeManifestFile(t, repoRoot, `{ "workspace": "ws_nocred", "capture": { "enabled": true, "events": ["SessionEnd"], "mode": "summary" } }`)
	tr := writeTranscript(t, repoRoot, "sess-z")

	in := HookInput{SessionID: "sess-z", TranscriptPath: tr, Cwd: repoRoot, HookEventName: "SessionEnd"}
	cmd := newSyncRunCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	processHook(cmd, in, "", "", false)

	entries, _ := spool.List()
	if len(entries) != 1 {
		t.Fatalf("expected 1 spooled entry, got %d", len(entries))
	}
	if entries[0].Payload == nil || entries[0].Payload.WorkspaceID != "ws_nocred" {
		t.Errorf("spool entry not self-contained: %+v", entries[0])
	}
}

// TestSyncRunDryRunPrintsPayload verifies --dry-run resolves + prints without
// spooling or uploading.
func TestSyncRunDryRunPrintsPayload(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	repoRoot := t.TempDir()
	writeManifestFile(t, repoRoot, `{ "workspace": "ws_dry", "capture": { "enabled": true, "events": ["SessionEnd"] } }`)
	tr := writeTranscript(t, repoRoot, "sess-dry")

	in := HookInput{SessionID: "sess-dry", TranscriptPath: tr, Cwd: repoRoot, HookEventName: "SessionEnd"}
	cmd := newSyncRunCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	processHook(cmd, in, "", "", true)

	if !strings.Contains(out.String(), "ws_dry") {
		t.Errorf("dry-run did not print payload: %s", out.String())
	}
	if n, _ := spool.Count(); n != 0 {
		t.Errorf("dry-run should not spool, got %d", n)
	}
}

// TestSyncRunRedactsSecretsBeforeEgress is the SEC-3/FR-12 contract: a secret in
// the transcript must not appear in the built payload.
func TestSyncRunRedactsSecretsBeforeEgress(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	repoRoot := t.TempDir()
	writeManifestFile(t, repoRoot, `{ "workspace": "ws_r", "capture": { "enabled": true, "events": ["SessionEnd"], "redact": true } }`)

	tr := filepath.Join(repoRoot, "t.jsonl")
	body := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"my key is AKIAIOSFODNN7EXAMPLE ok"}]}}` + "\n"
	if err := os.WriteFile(tr, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	resolved, _ := manifest.Resolve(repoRoot)
	in := HookInput{SessionID: "sess-r", TranscriptPath: tr, Cwd: repoRoot, HookEventName: "SessionEnd"}
	req := buildRequest(in, repoRoot, "ws_r", capture.ModeSummary, resolved)
	if strings.Contains(req.Summary, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("secret leaked into payload summary: %q", req.Summary)
	}
}

// --- helpers ---

func writeManifestFile(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, ".promptvm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeTranscript(t *testing.T, root, session string) string {
	t.Helper()
	p := filepath.Join(root, session+".jsonl")
	body := `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"do work"}]}}` + "\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// TestSyncStatusJSON exercises status output end-to-end with a stub manifest.
func TestSyncStatusJSON(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	// status resolves at cwd; run inside a temp dir with a manifest.
	repoRoot := t.TempDir()
	writeManifestFile(t, repoRoot, `{ "workspace": "ws_s", "capture": { "enabled": true, "events": ["SessionEnd"], "mode": "summary" } }`)
	chdir(t, repoRoot)

	cmd := newSyncStatusCmd()
	cmd.SetContext(context.Background())
	cmd.Flags().StringP("output", "o", "json", "")
	cmd.Flags().Bool("compact", false, "")
	cmd.Flags().Bool("no-header", false, "")
	cmd.Flags().Bool("wide", false, "")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	var st syncStatus
	if err := json.Unmarshal(out.Bytes(), &st); err != nil {
		t.Fatalf("status json: %v\n%s", err, out.String())
	}
	if st.Workspace != "ws_s" || !st.Enabled {
		t.Errorf("unexpected status: %+v", st)
	}
}

// TestSyncExportWritesManagedBlock verifies export pulls promoted captures and
// writes the managed block (CEO-1) via a stub server.
func TestSyncExportWritesManagedBlock(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/contexts/sessions") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"c1","summary":"fixed login bug","branch":"main"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	t.Setenv("PROMPTVM_PUBLIC_KEY", "pk_test000000000000000000000000000000000000")
	t.Setenv("PROMPTVM_SECRET_KEY", "sk_test111111111111111111111111111111111111")
	t.Setenv("PROMPTVM_BASE_URL", srv.URL)

	dir := t.TempDir()
	target := filepath.Join(dir, "CLAUDE.md")

	cmd := newSyncExportCmd()
	cmd.SetContext(context.Background())
	cmd.Flags().StringP("output", "o", "table", "")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	_ = cmd.Flags().Set("workspace", "ws_x")
	_ = cmd.Flags().Set("file", target)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("export RunE: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "fixed login bug") {
		t.Errorf("managed block missing capture summary: %s", string(data))
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

// TestSyncInitWritesManifestHookGitignoreAndCredential covers the init happy
// path end-to-end for local scope against a stub backend.
func TestSyncInitWritesManifestHookGitignoreAndCredential(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/api-keys"):
			_, _ = w.Write([]byte(`{"publicKey":"pk_cap","secretKey":"sk_cap","scopes":["capture"]}`))
		case strings.Contains(r.URL.Path, "/me/workspaces"):
			_, _ = w.Write([]byte(`{"data":[{"id":"ws_default","isDefault":true}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	t.Setenv("PROMPTVM_PUBLIC_KEY", "pk_test000000000000000000000000000000000000")
	t.Setenv("PROMPTVM_SECRET_KEY", "sk_test111111111111111111111111111111111111")
	t.Setenv("PROMPTVM_BASE_URL", srv.URL)

	repo := t.TempDir()
	for _, args := range [][]string{{"init"}, {"remote", "add", "origin", "git@github.com:acme/widgets.git"}} {
		c := exec.Command("git", args...)
		c.Dir = repo
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	chdir(t, repo)

	cmd := newSyncInitCmd()
	cmd.SetContext(context.Background())
	cmd.Flags().StringP("output", "o", "table", "")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	_ = cmd.Flags().Set("scope", "local")
	_ = cmd.Flags().Set("workspace", "ws_demo")
	_ = cmd.Flags().Set("yes", "true")
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("init RunE: %v\n%s", err, out.String())
	}

	// Manifest written at .promptvm/config.local.json.
	mpath, _ := manifest.Path(manifest.ScopeLocal, repo)
	m, err := manifest.Read(mpath)
	if err != nil || m == nil {
		t.Fatalf("manifest not written: %v", err)
	}
	if m.Workspace != "ws_demo" {
		t.Errorf("manifest workspace = %q", m.Workspace)
	}

	// Hook written into .claude/settings.local.json with SessionStart reconcile.
	spath, _ := hooks.SettingsFilePath(hooks.ScopeLocal)
	s, err := hooks.ReadSettings(spath)
	if err != nil {
		t.Fatal(err)
	}
	events := s.CaptureEventsInstalled()
	if !contains(events, "SessionEnd") || !contains(events, "SessionStart") {
		t.Errorf("installed events = %v, want SessionEnd + SessionStart", events)
	}

	// gitignore updated for the local manifest.
	gi, _ := os.ReadFile(filepath.Join(repo, ".gitignore"))
	if !strings.Contains(string(gi), ".promptvm/config.local.json") {
		t.Errorf(".gitignore missing local manifest: %s", string(gi))
	}

	// Capture credential stored.
	cred, err := capture.LoadCredential("ws_demo")
	if err != nil || cred == nil || cred.PublicKey != "pk_cap" {
		t.Errorf("capture credential not stored: %+v err=%v", cred, err)
	}

	// Idempotent: re-running does not duplicate the hook matcher group.
	cmd2 := newSyncInitCmd()
	cmd2.SetContext(context.Background())
	cmd2.Flags().StringP("output", "o", "table", "")
	cmd2.SetOut(&bytes.Buffer{})
	cmd2.SetErr(&bytes.Buffer{})
	_ = cmd2.Flags().Set("scope", "local")
	_ = cmd2.Flags().Set("workspace", "ws_demo")
	_ = cmd2.Flags().Set("yes", "true")
	if err := cmd2.RunE(cmd2, nil); err != nil {
		t.Fatalf("second init: %v", err)
	}
	s2, _ := hooks.ReadSettings(spath)
	if list, ok := s2.Hooks()["SessionEnd"].([]interface{}); ok && len(list) != 1 {
		t.Errorf("SessionEnd duplicated after re-init: %d groups", len(list))
	}
}
