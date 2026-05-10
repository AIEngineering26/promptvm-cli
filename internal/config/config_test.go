package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

func TestValidateProfileName(t *testing.T) {
	valid := []string{"default", "staging", "prod-1", "user.one", "team_a", "abc123"}
	for _, name := range valid {
		if err := ValidateProfileName(name); err != nil {
			t.Errorf("ValidateProfileName(%q) unexpected error: %v", name, err)
		}
	}
	invalid := []string{"", ".", "..", "../escape", "team/a", "team\\a", "spaces here", "weird*name"}
	for _, name := range invalid {
		if err := ValidateProfileName(name); err == nil {
			t.Errorf("ValidateProfileName(%q) expected error, got nil", name)
		}
	}
}

func TestSaveProfile_InvalidName(t *testing.T) {
	_, cleanup := setupTestDir(t)
	defer cleanup()

	p := &Profile{Name: "../escape", APIKey: "pvk_live_xxxxxxxxx"}
	if err := SaveProfile(p); err == nil {
		t.Error("SaveProfile with traversal name should fail")
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

// TestLoadProfile_MigratesLegacyAPIKey covers PRD F3 §US-003: when a
// profile file on disk uses the legacy single-string `api_key: pk:sk`
// shape, LoadProfile splits it into `public_key` + `secret_key` on
// first load and rewrites the file in-place.
func TestLoadProfile_MigratesLegacyAPIKey(t *testing.T) {
	dir, cleanup := setupTestDir(t)
	defer cleanup()

	const (
		pk = "pk_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		sk = "sk_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)

	// Hand-craft a legacy-shape profile YAML on disk.
	profilePath := filepath.Join(dir, "profiles", "legacy.yaml")
	if err := os.MkdirAll(filepath.Dir(profilePath), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacyYAML := []byte(`name: legacy
api_key: ` + pk + ":" + sk + `
base_url: https://api.promptvm.com
environment: live
`)
	if err := os.WriteFile(profilePath, legacyYAML, 0600); err != nil {
		t.Fatalf("write legacy profile: %v", err)
	}

	loaded, err := LoadProfile("legacy")
	if err != nil {
		t.Fatalf("LoadProfile: %v", err)
	}
	if loaded.PublicKey != pk {
		t.Errorf("PublicKey = %q, want %q", loaded.PublicKey, pk)
	}
	if loaded.SecretKey != sk {
		t.Errorf("SecretKey = %q, want %q", loaded.SecretKey, sk)
	}
	if loaded.APIKey != "" {
		t.Errorf("legacy APIKey field should be cleared after migration, got %q", loaded.APIKey)
	}

	// Verify the file on disk was rewritten in dual-key form.
	rewritten, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("read after migrate: %v", err)
	}
	got := string(rewritten)
	if !strings.Contains(got, "public_key: "+pk) {
		t.Errorf("rewritten profile missing public_key field: %s", got)
	}
	if !strings.Contains(got, "secret_key: "+sk) {
		t.Errorf("rewritten profile missing secret_key field: %s", got)
	}
	if strings.Contains(got, "api_key:") && !strings.Contains(got, "api_key: \"\"") {
		// The yaml encoder will omit the empty field thanks to
		// `omitempty`; but be defensive.
		if !strings.Contains(got, "api_key: \n") {
			// allow only if it really is omitted — make sure the original
			// pk:sk string isn't still present as a value.
			if strings.Contains(got, pk+":"+sk) {
				t.Errorf("rewritten profile still contains legacy combined api_key: %s", got)
			}
		}
	}

	// Re-load: a second migration must not be triggered.
	loaded2, err := LoadProfile("legacy")
	if err != nil {
		t.Fatalf("LoadProfile second pass: %v", err)
	}
	if loaded2.APIKey != "" {
		t.Errorf("APIKey reappeared after second load: %q", loaded2.APIKey)
	}

	// File permissions stay 0600 on POSIX.
	info, err := os.Stat(profilePath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("file perm after migration = %o, want 0600", perm)
	}
}

// TestLoadProfile_MigrationFailureIsNonFatal covers PRD F3 §US-003: if
// the rewrite cannot be performed (read-only directory, etc.), the
// in-memory profile still carries the split values and the user gets a
// warning but their session continues.
func TestLoadProfile_MigrationFailureIsNonFatal(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("read-only directory test does not work as root")
	}
	dir, cleanup := setupTestDir(t)
	defer cleanup()

	const (
		pk = "pk_cccccccccccccccccccccccccccccccccccccccc"
		sk = "sk_dddddddddddddddddddddddddddddddddddddddd"
	)

	profilesPath := filepath.Join(dir, "profiles")
	if err := os.MkdirAll(profilesPath, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	profilePath := filepath.Join(profilesPath, "ro.yaml")
	legacyYAML := []byte(`name: ro
api_key: ` + pk + ":" + sk + "\n")
	if err := os.WriteFile(profilePath, legacyYAML, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Make the parent directory read-only so the atomic-write rename
	// step fails. The temp file creation itself fails first, which is
	// what triggers the warning path.
	if err := os.Chmod(profilesPath, 0500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(profilesPath, 0700) })

	var buf bytes.Buffer
	prev := migrationWarnWriter
	migrationWarnWriter = &buf
	t.Cleanup(func() { migrationWarnWriter = prev })

	loaded, err := LoadProfile("ro")
	if err != nil {
		t.Fatalf("LoadProfile should succeed despite write failure: %v", err)
	}
	if loaded.PublicKey != pk || loaded.SecretKey != sk {
		t.Errorf("in-memory split lost: pk=%q sk=%q", loaded.PublicKey, loaded.SecretKey)
	}
	if !strings.Contains(buf.String(), "Warning") {
		t.Errorf("expected migration-failure warning, got %q", buf.String())
	}
}

// TestAtomicWriteFile verifies the helper writes data and applies the
// requested permissions, never leaving partial content visible at the
// final path.
func TestAtomicWriteFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "secret.yaml")
	payload := []byte("secret_key: sk_xxxxxxxx\n")

	if err := atomicWriteFile(target, payload, 0600); err != nil {
		t.Fatalf("atomicWriteFile: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("payload mismatch: got %q want %q", got, payload)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("perm = %o, want 0600", perm)
	}

	// No leftover .tmp files in the directory.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("leftover temp file %q", e.Name())
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
