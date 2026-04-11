package config

import (
	"os"
	"path/filepath"
	"testing"
)

func setupTestDir(t *testing.T) (string, func()) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "promptvm")
	orig := dirOverride
	dirOverride = dir
	return dir, func() {
		dirOverride = orig
	}
}

func TestLoadDefaultConfig(t *testing.T) {
	_, cleanup := setupTestDir(t)
	defer cleanup()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.ActiveProfile != "default" {
		t.Errorf("ActiveProfile = %q, want %q", cfg.ActiveProfile, "default")
	}
	if cfg.Defaults.Output != "table" {
		t.Errorf("Defaults.Output = %q, want %q", cfg.Defaults.Output, "table")
	}
}

func TestSaveAndLoadConfig(t *testing.T) {
	_, cleanup := setupTestDir(t)
	defer cleanup()

	cfg := &Config{
		ActiveProfile: "staging",
		Defaults: Defaults{
			Output:    "json",
			NoColor:   true,
			Workspace: "ws_123",
		},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.ActiveProfile != "staging" {
		t.Errorf("ActiveProfile = %q, want %q", loaded.ActiveProfile, "staging")
	}
	if loaded.Defaults.Output != "json" {
		t.Errorf("Defaults.Output = %q, want %q", loaded.Defaults.Output, "json")
	}
	if !loaded.Defaults.NoColor {
		t.Error("Defaults.NoColor = false, want true")
	}
	if loaded.Defaults.Workspace != "ws_123" {
		t.Errorf("Defaults.Workspace = %q, want %q", loaded.Defaults.Workspace, "ws_123")
	}
}

func TestGetSet(t *testing.T) {
	cfg := &Config{
		ActiveProfile: "default",
		Defaults:      Defaults{Output: "table"},
	}

	val, err := cfg.Get("defaults.output")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if val != "table" {
		t.Errorf("Get(defaults.output) = %q, want %q", val, "table")
	}

	if err := cfg.Set("defaults.output", "json"); err != nil {
		t.Fatalf("Set() error: %v", err)
	}
	val, _ = cfg.Get("defaults.output")
	if val != "json" {
		t.Errorf("After Set, Get(defaults.output) = %q, want %q", val, "json")
	}

	// Invalid output format
	if err := cfg.Set("defaults.output", "xml"); err == nil {
		t.Error("Set(defaults.output, xml) should fail")
	}

	// Unknown key
	if _, err := cfg.Get("unknown.key"); err == nil {
		t.Error("Get(unknown.key) should fail")
	}
}

func TestProfileCRUD(t *testing.T) {
	dir, cleanup := setupTestDir(t)
	defer cleanup()

	p := &Profile{
		Name:         "test",
		APIKey:       "pvk_live_abc123456789",
		BaseURL:      "https://api.promptvm.com",
		Environment:  "live",
		Organization: "org_123",
	}

	// Save
	if err := SaveProfile(p); err != nil {
		t.Fatalf("SaveProfile() error: %v", err)
	}

	// Verify file permissions
	path := filepath.Join(dir, "profiles", "test.yaml")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("file perm = %o, want 0600", perm)
	}

	// Load
	loaded, err := LoadProfile("test")
	if err != nil {
		t.Fatalf("LoadProfile() error: %v", err)
	}
	if loaded.APIKey != p.APIKey {
		t.Errorf("APIKey = %q, want %q", loaded.APIKey, p.APIKey)
	}

	// List
	profiles, err := ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles() error: %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("len(profiles) = %d, want 1", len(profiles))
	}

	// Delete
	if err := DeleteProfile("test"); err != nil {
		t.Fatalf("DeleteProfile() error: %v", err)
	}

	// Verify deleted
	if _, err := LoadProfile("test"); err == nil {
		t.Error("LoadProfile after delete should fail")
	}
}

func TestMaskAPIKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"short", "****"},
		{"pvk_live_abc123456789", "pvk_live****456789"},
	}
	for _, tt := range tests {
		got := MaskAPIKey(tt.input)
		if got != tt.want {
			t.Errorf("MaskAPIKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSetActiveProfile(t *testing.T) {
	_, cleanup := setupTestDir(t)
	defer cleanup()

	// Save a profile first
	p := &Profile{
		Name:        "staging",
		APIKey:      "pvk_test_xyz",
		BaseURL:     "https://api.staging.promptvm.com",
		Environment: "test",
	}
	if err := SaveProfile(p); err != nil {
		t.Fatalf("SaveProfile() error: %v", err)
	}

	cfg := &Config{ActiveProfile: "default", Defaults: Defaults{Output: "table"}}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Switch to staging
	if err := cfg.SetActiveProfile("staging"); err != nil {
		t.Fatalf("SetActiveProfile() error: %v", err)
	}

	// Reload and verify
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.ActiveProfile != "staging" {
		t.Errorf("ActiveProfile = %q, want %q", loaded.ActiveProfile, "staging")
	}

	// Non-existent profile
	if err := cfg.SetActiveProfile("nonexistent"); err == nil {
		t.Error("SetActiveProfile(nonexistent) should fail")
	}
}
