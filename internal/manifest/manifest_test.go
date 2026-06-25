package manifest

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func boolp(b bool) *bool { return &b }

// writeManifest is a test helper that writes a manifest file at the scope path
// rooted under root (for project/local) using a raw JSON body.
func writeProject(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, ".promptvm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeLocal(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, ".promptvm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.local.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveDefaultsWhenNoFiles(t *testing.T) {
	root := t.TempDir()
	// Point user config dir at an empty temp so the user scope is absent.
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	r, err := Resolve(root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !r.Enabled {
		t.Errorf("Enabled default = false, want true")
	}
	if r.Mode != "summary" {
		t.Errorf("Mode = %q, want summary", r.Mode)
	}
	if !reflect.DeepEqual(r.Events, DefaultEvents) {
		t.Errorf("Events = %v, want %v", r.Events, DefaultEvents)
	}
	if r.Directory != "captures" {
		t.Errorf("Directory = %q, want captures", r.Directory)
	}
}

func TestResolveLocalOverridesProject(t *testing.T) {
	root := t.TempDir()
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	writeProject(t, root, `{
	  "workspace": "ws_project",
	  "directory": "captures",
	  "capture": { "enabled": true, "mode": "summary", "events": ["SessionEnd", "PreCompact"] }
	}`)
	writeLocal(t, root, `{
	  "workspace": "ws_local",
	  "capture": { "mode": "transcript" }
	}`)

	r, err := Resolve(root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Workspace != "ws_local" {
		t.Errorf("Workspace = %q, want ws_local (local wins)", r.Workspace)
	}
	// mode set only in local → transcript
	if r.Mode != "transcript" {
		t.Errorf("Mode = %q, want transcript", r.Mode)
	}
	// events set only in project (local did not declare) → inherit project
	want := []string{"SessionEnd", "PreCompact"}
	if !reflect.DeepEqual(r.Events, want) {
		t.Errorf("Events = %v, want %v", r.Events, want)
	}
}

// TestEventsArrayReplaceNotConcat is the DX-5 contract: a more-specific scope
// that declares events REPLACES the broader scope (so a repo can drop
// PreCompact), never unions.
func TestEventsArrayReplaceNotConcat(t *testing.T) {
	root := t.TempDir()
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	writeProject(t, root, `{ "capture": { "events": ["SessionEnd", "PreCompact"] } }`)
	writeLocal(t, root, `{ "capture": { "events": ["SessionEnd"] } }`)

	r, err := Resolve(root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := []string{"SessionEnd"}
	if !reflect.DeepEqual(r.Events, want) {
		t.Fatalf("Events = %v, want %v (REPLACE, not concat)", r.Events, want)
	}
	if r.EventSelected("PreCompact") {
		t.Errorf("PreCompact still selected after local dropped it")
	}
}

func TestExcludePathsReplace(t *testing.T) {
	root := t.TempDir()
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	writeProject(t, root, `{ "capture": { "excludePaths": ["a/**", "b/**"] } }`)
	writeLocal(t, root, `{ "capture": { "excludePaths": ["c/**"] } }`)

	r, err := Resolve(root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := []string{"c/**"}
	if !reflect.DeepEqual(r.ExcludePaths, want) {
		t.Errorf("ExcludePaths = %v, want %v", r.ExcludePaths, want)
	}
}

func TestEnabledFalseDisables(t *testing.T) {
	root := t.TempDir()
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	writeProject(t, root, `{ "capture": { "enabled": false } }`)

	r, err := Resolve(root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Enabled {
		t.Errorf("Enabled = true, want false")
	}
}

func TestWriteAndReadRoundTrip(t *testing.T) {
	root := t.TempDir()
	m := &Manifest{
		Workspace: "ws_x",
		Directory: "captures",
		Capture: &Capture{
			Enabled: boolp(true),
			Events:  []string{"SessionEnd"},
			Mode:    "summary",
			Redact:  boolp(true),
		},
	}
	path, err := WriteScope(ScopeProject, root, m)
	if err != nil {
		t.Fatalf("WriteScope: %v", err)
	}
	if filepath.Base(path) != "config.json" {
		t.Errorf("project path basename = %q", filepath.Base(path))
	}
	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Workspace != "ws_x" || got.SchemaVersion != SchemaVersion {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestPathLocalIsGitignoredFile(t *testing.T) {
	root := t.TempDir()
	p, err := Path(ScopeLocal, root)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "config.local.json" {
		t.Errorf("local manifest basename = %q, want config.local.json", filepath.Base(p))
	}
}
