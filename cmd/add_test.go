package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AIEngineering26/promptvm-cli/internal/prompt"
	"github.com/spf13/cobra"
)

func TestParseAddRef(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"bare slug", "pdf-toolkit", "pdf-toolkit", false},
		{"creator slug", "acme/pdf-toolkit", "acme/pdf-toolkit", false},
		{"trims whitespace", "  pdf-toolkit  ", "pdf-toolkit", false},
		{"legacy file slug", "pdf-21ffa77d", "pdf-21ffa77d", false},
		{"empty", "", "", true},
		{"too many segments", "a/b/c", "", true},
		{"empty creator", "/pdf", "", true},
		{"empty slug", "acme/", "", true},
		{"path traversal slug", "../../etc", "", true},
		{"dot segment", "..", "", true},
		{"absolute path", "/etc/passwd", "", true},
		{"backslash traversal", "..\\windows", "", true},
		{"uppercase rejected", "PDF-Toolkit", "", true},
		{"traversal in creator", "../etc/pdf", "", true},
		{"leading hyphen rejected", "-pdf", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref, err := parseAddRef(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if ref != tc.want {
				t.Errorf("got %q, want %q", ref, tc.want)
			}
		})
	}
}

func TestResolvePath(t *testing.T) {
	cases := []struct {
		ref, want string
	}{
		{"pdf", "/api/v1/marketplace/resolve?ref=pdf"},
		{"acme/pdf", "/api/v1/marketplace/resolve?ref=acme%2Fpdf"},
	}
	for _, tc := range cases {
		if got := resolvePath(tc.ref); got != tc.want {
			t.Errorf("resolvePath(%q) = %q, want %q", tc.ref, got, tc.want)
		}
	}
}

func TestResolveClaudeRoot(t *testing.T) {
	if got, err := resolveClaudeRoot("user", "/override"); err != nil || got != "/override" {
		t.Errorf("override should win: %q %v", got, err)
	}
	if got, err := resolveClaudeRoot("project", ""); err != nil || got != ".claude" {
		t.Errorf("project scope = %q %v", got, err)
	}
	if _, err := resolveClaudeRoot("bogus", ""); err == nil {
		t.Error("bogus scope should error")
	}
	home, _ := os.UserHomeDir()
	if got, err := resolveClaudeRoot("user", ""); err != nil || got != filepath.Join(home, ".claude") {
		t.Errorf("user scope = %q %v", got, err)
	}
}

// newTestAddCmd wires a standalone add command with the persistent flags the
// resolver reads, so it can be exercised without the full root command.
func newTestAddCmd() *cobra.Command {
	cmd := newAddCmd()
	cmd.Flags().String("base-url", "", "base url")
	cmd.Flags().String("public-key", "", "")
	cmd.Flags().String("secret-key", "", "")
	cmd.Flags().String("api-key", "", "")
	return cmd
}

// resolveFixture describes one unified-resolve response the fake server serves.
type resolveFixture struct {
	kind    string
	name    string
	content map[string]interface{}
}

// fakeServer returns a test server exposing the unified resolve endpoint plus a
// file-download endpoint and the install-counter POST. Fixtures are keyed by the
// url-decoded ?ref= value. It records whether the install counter was hit.
func fakeServer(t *testing.T, fixtures map[string]resolveFixture) (*httptest.Server, *bool) {
	t.Helper()
	counterHit := false
	mux := http.NewServeMux()
	var baseURL string

	mux.HandleFunc("/api/v1/marketplace/resolve", func(w http.ResponseWriter, r *http.Request) {
		ref := r.URL.Query().Get("ref")
		fx, ok := fixtures[ref]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"NOT_FOUND","ref":"` + ref + `"}`))
			return
		}
		if fx.kind == "__ambiguous__" {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"AMBIGUOUS_REF","ref":"` + ref + `","candidates":["acme/pdf","promptvm/pdf"]}`))
			return
		}
		// Rewrite skill/agent file downloadUrls to point back at this server.
		content := fx.content
		body := map[string]interface{}{
			"ref":         fx.name,
			"kind":        fx.kind,
			"listingId":   "listing-1",
			"fileId":      "file-1",
			"name":        fx.name,
			"resolvedVia": "bare_name",
			"creator":     map[string]interface{}{"username": "acme", "displayName": "Acme"},
			"content":     content,
		}
		_ = baseURL
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	})
	mux.HandleFunc("/dl", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello"))
	})
	mux.HandleFunc("/api/v1/marketplace/resolve/install", func(w http.ResponseWriter, r *http.Request) {
		counterHit = true
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	baseURL = srv.URL
	t.Cleanup(srv.Close)
	return srv, &counterHit
}

// runAdd builds a fresh add command pointed at srv and runs it, treating dir as
// the .claude root override.
func runAdd(t *testing.T, srv *httptest.Server, claudeRoot string, args ...string) (string, error) {
	t.Helper()
	cmd := newTestAddCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	full := append([]string{"--base-url", srv.URL, "--skills-dir", claudeRoot}, args...)
	cmd.SetArgs(full)
	err := cmd.Execute()
	return out.String(), err
}

func skillFixture(baseURL string) resolveFixture {
	return resolveFixture{
		kind: "skill",
		name: "found",
		content: map[string]interface{}{
			"raw_skill_md": "---\nname: found\n---\nbody",
			"files": []map[string]interface{}{
				{"path": "a.txt", "downloadUrl": baseURL + "/dl", "sizeBytes": 5},
			},
		},
	}
}

func TestAddInstallsSkill(t *testing.T) {
	dir := t.TempDir()
	// The skill fixture's bundled-file downloadUrl must point at the same test
	// server, so build the fixtures lazily via a shared holder the handler reads.
	fixtures := map[string]resolveFixture{}
	srv, _ := fakeServer(t, fixtures)
	fixtures["found"] = skillFixture(srv.URL)

	out, err := runAdd(t, srv, dir, "found")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if !strings.Contains(out, `Installed skill "found"`) {
		t.Errorf("output missing success line: %s", out)
	}
	md, err := os.ReadFile(filepath.Join(dir, "skills", "found", "SKILL.md"))
	if err != nil || !strings.Contains(string(md), "name: found") {
		t.Errorf("SKILL.md not written: %v %q", err, md)
	}
	a, _ := os.ReadFile(filepath.Join(dir, "skills", "found", "a.txt"))
	if string(a) != "hello" {
		t.Errorf("bundled file = %q", a)
	}
	// Generic install tracker records the skill.
	tr, _ := os.ReadFile(filepath.Join(dir, ".promptvm-installs.json"))
	if !strings.Contains(string(tr), `"kind": "skill"`) {
		t.Errorf("install tracker missing skill entry: %s", tr)
	}
}

func TestAddInstallsAgent(t *testing.T) {
	dir := t.TempDir()
	fixtures := map[string]resolveFixture{
		"my-agent": {kind: "agent", name: "my-agent", content: map[string]interface{}{
			"raw_agent_md": "---\nname: my-agent\n---\nagent body",
			"body":         "agent body",
			"files":        []map[string]interface{}{},
		}},
	}
	srv, _ := fakeServer(t, fixtures)
	out, err := runAdd(t, srv, dir, "my-agent")
	if err != nil {
		t.Fatalf("add agent: %v\n%s", err, out)
	}
	md, err := os.ReadFile(filepath.Join(dir, "agents", "my-agent.md"))
	if err != nil || !strings.Contains(string(md), "agent body") {
		t.Errorf("agent .md not written: %v %q", err, md)
	}
}

func TestAddInstallsCommand(t *testing.T) {
	dir := t.TempDir()
	fixtures := map[string]resolveFixture{
		"my-cmd": {kind: "command", name: "my-cmd", content: map[string]interface{}{
			"raw_command_md": "---\ndescription: x\n---\ncommand body",
			"body":           "command body",
			"files":          []map[string]interface{}{},
		}},
	}
	srv, _ := fakeServer(t, fixtures)
	out, err := runAdd(t, srv, dir, "my-cmd")
	if err != nil {
		t.Fatalf("add command: %v\n%s", err, out)
	}
	md, err := os.ReadFile(filepath.Join(dir, "commands", "my-cmd.md"))
	if err != nil || !strings.Contains(string(md), "command body") {
		t.Errorf("command .md not written: %v %q", err, md)
	}
}

func TestAddInstallsPrompt(t *testing.T) {
	dir := t.TempDir()
	fixtures := map[string]resolveFixture{
		"my-prompt": {kind: "prompt", name: "my-prompt", content: map[string]interface{}{
			"content": "you are a helpful assistant",
		}},
	}
	srv, _ := fakeServer(t, fixtures)
	out, err := runAdd(t, srv, dir, "my-prompt")
	if err != nil {
		t.Fatalf("add prompt: %v\n%s", err, out)
	}
	md, err := os.ReadFile(filepath.Join(dir, "prompts", "my-prompt.md"))
	if err != nil || !strings.Contains(string(md), "helpful assistant") {
		t.Errorf("prompt .md not written: %v %q", err, md)
	}
}

func TestAddPromptStdout(t *testing.T) {
	dir := t.TempDir()
	fixtures := map[string]resolveFixture{
		"my-prompt": {kind: "prompt", name: "my-prompt", content: map[string]interface{}{
			"content": "printed body",
		}},
	}
	srv, _ := fakeServer(t, fixtures)
	out, err := runAdd(t, srv, dir, "my-prompt", "--stdout")
	if err != nil {
		t.Fatalf("add prompt --stdout: %v\n%s", err, out)
	}
	if !strings.Contains(out, "printed body") {
		t.Errorf("stdout missing body: %s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "prompts", "my-prompt.md")); !os.IsNotExist(err) {
		t.Error("--stdout must not write a file")
	}
}

func TestAddInstallsHook(t *testing.T) {
	dir := t.TempDir()
	fixtures := map[string]resolveFixture{
		"my-hook": {kind: "hook", name: "my-hook", content: map[string]interface{}{
			"config": map[string]interface{}{
				"PreToolUse": []map[string]interface{}{
					{"matcher": "Bash", "hooks": []map[string]interface{}{{"type": "command", "command": "echo hi"}}},
				},
			},
			"events": []string{"PreToolUse"},
		}},
	}
	srv, _ := fakeServer(t, fixtures)
	out, err := runAdd(t, srv, dir, "my-hook")
	if err != nil {
		t.Fatalf("add hook: %v\n%s", err, out)
	}
	settings, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil || !strings.Contains(string(settings), "PreToolUse") {
		t.Errorf("settings.json not merged: %v %q", err, settings)
	}
	if !strings.Contains(string(settings), "_slug") {
		t.Errorf("hook matcher missing _slug ownership: %s", settings)
	}
}

func TestAddInstallsSettings(t *testing.T) {
	dir := t.TempDir()
	// Pre-existing settings with a user key that must be preserved.
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"env":{"KEEP":"1"}}`), 0o644)
	fixtures := map[string]resolveFixture{
		"my-settings": {kind: "settings", name: "my-settings", content: map[string]interface{}{
			"settings": map[string]interface{}{
				"env":        map[string]interface{}{"ADDED": "2"},
				"statusLine": map[string]interface{}{"type": "command"},
			},
		}},
	}
	srv, _ := fakeServer(t, fixtures)
	out, err := runAdd(t, srv, dir, "my-settings")
	if err != nil {
		t.Fatalf("add settings: %v\n%s", err, out)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var parsed map[string]interface{}
	_ = json.Unmarshal(data, &parsed)
	env, _ := parsed["env"].(map[string]interface{})
	if env["KEEP"] != "1" || env["ADDED"] != "2" {
		t.Errorf("deep-merge lost/failed keys: %v", parsed)
	}
	if _, ok := parsed["statusLine"]; !ok {
		t.Errorf("new top-level key not merged: %v", parsed)
	}
}

func TestAddSettingsPreservesConflictWithoutForce(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"model":"mine"}`), 0o644)
	fixtures := map[string]resolveFixture{
		"s": {kind: "settings", name: "s", content: map[string]interface{}{
			"settings": map[string]interface{}{"model": "theirs"},
		}},
	}
	srv, _ := fakeServer(t, fixtures)
	if _, err := runAdd(t, srv, dir, "s"); err != nil {
		t.Fatalf("add: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	if !strings.Contains(string(data), `"mine"`) {
		t.Errorf("conflict key should be preserved without --force: %s", data)
	}
	// With --force it is overwritten.
	if _, err := runAdd(t, srv, dir, "s", "--force"); err != nil {
		t.Fatalf("add --force: %v", err)
	}
	data, _ = os.ReadFile(filepath.Join(dir, "settings.json"))
	if !strings.Contains(string(data), `"theirs"`) {
		t.Errorf("--force should overwrite conflict key: %s", data)
	}
}

func TestAddInstallsMCP(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".claude")
	fixtures := map[string]resolveFixture{
		"my-mcp": {kind: "mcp", name: "my-mcp", content: map[string]interface{}{
			"config": map[string]interface{}{
				"schema_version": "1",
				"name":           "my-mcp",
				"type":           "http",
				"url":            "https://example.com/mcp",
			},
		}},
	}
	srv, _ := fakeServer(t, fixtures)
	out, err := runAdd(t, srv, dir, "my-mcp")
	if err != nil {
		t.Fatalf("add mcp: %v\n%s", err, out)
	}
	// .mcp.json lands at the parent of .claude (the project root).
	data, err := os.ReadFile(filepath.Join(filepath.Dir(dir), ".mcp.json"))
	if err != nil {
		t.Fatalf("reading .mcp.json: %v", err)
	}
	var doc map[string]interface{}
	_ = json.Unmarshal(data, &doc)
	servers, _ := doc["mcpServers"].(map[string]interface{})
	entry, _ := servers["my-mcp"].(map[string]interface{})
	if entry["url"] != "https://example.com/mcp" {
		t.Errorf("mcp url not written: %v", doc)
	}
	if _, leaked := entry["schema_version"]; leaked {
		t.Errorf("registry-only schema_version leaked into .mcp.json: %v", entry)
	}
}

func TestAddFiresInstallCounter(t *testing.T) {
	dir := t.TempDir()
	fixtures := map[string]resolveFixture{
		"my-prompt": {kind: "prompt", name: "my-prompt", content: map[string]interface{}{"content": "x"}},
	}
	srv, counterHit := fakeServer(t, fixtures)
	if _, err := runAdd(t, srv, dir, "my-prompt"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if !*counterHit {
		t.Error("install counter should be hit on successful install")
	}
}

func TestAddDryRunSkipsInstallCounter(t *testing.T) {
	dir := t.TempDir()
	fixtures := map[string]resolveFixture{
		"my-prompt": {kind: "prompt", name: "my-prompt", content: map[string]interface{}{"content": "x"}},
	}
	srv, counterHit := fakeServer(t, fixtures)
	if _, err := runAdd(t, srv, dir, "my-prompt", "--dry-run"); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if *counterHit {
		t.Error("dry-run must not hit the install counter")
	}
}

func TestAddNotFound(t *testing.T) {
	srv, _ := fakeServer(t, map[string]resolveFixture{})
	out, err := runAdd(t, srv, t.TempDir(), "missing")
	if err == nil {
		t.Fatal("expected not-found error")
	}
	if !strings.Contains(err.Error(), `"missing" not found on the marketplace`) {
		t.Errorf("wrong error: %v (out: %s)", err, out)
	}
}

func TestAddAmbiguous(t *testing.T) {
	fixtures := map[string]resolveFixture{
		"pdf": {kind: "__ambiguous__", name: "pdf"},
	}
	srv, _ := fakeServer(t, fixtures)
	_, err := runAdd(t, srv, t.TempDir(), "pdf")
	if err == nil {
		t.Fatal("expected ambiguous error")
	}
	if !strings.Contains(err.Error(), "Did you mean: acme/pdf, promptvm/pdf?") {
		t.Errorf("ambiguous message missing candidates: %v", err)
	}
}

func TestAddDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	fixtures := map[string]resolveFixture{
		"my-prompt": {kind: "prompt", name: "my-prompt", content: map[string]interface{}{"content": "x"}},
	}
	srv, _ := fakeServer(t, fixtures)
	out, err := runAdd(t, srv, dir, "my-prompt", "--dry-run")
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !strings.Contains(out, "Dry-run:") {
		t.Errorf("missing dry-run summary: %s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "prompts")); !os.IsNotExist(err) {
		t.Error("dry-run must not create the prompts folder")
	}
}

func TestAddCollisionNonTTYAborts(t *testing.T) {
	dir := t.TempDir()
	fixtures := map[string]resolveFixture{
		"my-prompt": {kind: "prompt", name: "my-prompt", content: map[string]interface{}{"content": "x"}},
	}
	srv, _ := fakeServer(t, fixtures)
	// Pre-create the target.
	_ = os.MkdirAll(filepath.Join(dir, "prompts"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "prompts", "my-prompt.md"), []byte("old"), 0o644)

	orig := isTTYFunc
	isTTYFunc = func() bool { return false }
	defer func() { isTTYFunc = orig }()

	_, err := runAdd(t, srv, dir, "my-prompt")
	if err == nil || !strings.Contains(err.Error(), `already exists. Pass --force`) {
		t.Errorf("want already-exists error, got %v", err)
	}
}

func TestAddCollisionForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	fixtures := map[string]resolveFixture{
		"my-prompt": {kind: "prompt", name: "my-prompt", content: map[string]interface{}{"content": "new"}},
	}
	srv, _ := fakeServer(t, fixtures)
	_ = os.MkdirAll(filepath.Join(dir, "prompts"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "prompts", "my-prompt.md"), []byte("old"), 0o644)

	if _, err := runAdd(t, srv, dir, "my-prompt", "--force"); err != nil {
		t.Fatalf("force: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "prompts", "my-prompt.md"))
	if string(data) != "new\n" && string(data) != "new" {
		t.Errorf("force should overwrite with new content, got %q", data)
	}
}

func TestAddCollisionPromptDenied(t *testing.T) {
	dir := t.TempDir()
	fixtures := map[string]resolveFixture{
		"my-prompt": {kind: "prompt", name: "my-prompt", content: map[string]interface{}{"content": "x"}},
	}
	srv, _ := fakeServer(t, fixtures)
	_ = os.MkdirAll(filepath.Join(dir, "prompts"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "prompts", "my-prompt.md"), []byte("old"), 0o644)

	origTTY := isTTYFunc
	origConfirm := confirmOverwriteFunc
	isTTYFunc = func() bool { return true }
	confirmOverwriteFunc = func(string) (bool, error) { return false, nil }
	defer func() { isTTYFunc = origTTY; confirmOverwriteFunc = origConfirm }()

	_, err := runAdd(t, srv, dir, "my-prompt")
	if err == nil || err.Error() != "Installation cancelled." {
		t.Errorf("want 'Installation cancelled.', got %v", err)
	}
}

func TestAddCollisionPromptCancelledTreatedAsDenial(t *testing.T) {
	dir := t.TempDir()
	fixtures := map[string]resolveFixture{
		"my-prompt": {kind: "prompt", name: "my-prompt", content: map[string]interface{}{"content": "x"}},
	}
	srv, _ := fakeServer(t, fixtures)
	_ = os.MkdirAll(filepath.Join(dir, "prompts"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "prompts", "my-prompt.md"), []byte("old"), 0o644)

	origTTY := isTTYFunc
	origConfirm := confirmOverwriteFunc
	isTTYFunc = func() bool { return true }
	confirmOverwriteFunc = func(string) (bool, error) { return false, prompt.ErrCancelled }
	defer func() { isTTYFunc = origTTY; confirmOverwriteFunc = origConfirm }()

	_, err := runAdd(t, srv, dir, "my-prompt")
	if err == nil || err.Error() != "Installation cancelled." {
		t.Errorf("want 'Installation cancelled.', got %v", err)
	}
}

func TestAddUnsupportedKind(t *testing.T) {
	fixtures := map[string]resolveFixture{
		"weird": {kind: "capture", name: "weird", content: map[string]interface{}{}},
	}
	srv, _ := fakeServer(t, fixtures)
	_, err := runAdd(t, srv, t.TempDir(), "weird")
	if err == nil || !strings.Contains(err.Error(), "Unsupported content kind") {
		t.Errorf("want unsupported-kind error, got %v", err)
	}
}
