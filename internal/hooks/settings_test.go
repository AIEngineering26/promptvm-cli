package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// ReadSettings
// ---------------------------------------------------------------------------

func TestReadSettings_MissingFile(t *testing.T) {
	s, err := ReadSettings(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hooks := s.Hooks()
	if len(hooks) != 0 {
		t.Fatalf("expected empty hooks, got %v", hooks)
	}
}

func TestReadSettings_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	content := `{
  "permissions": {"allow": ["Read"]},
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "echo hi"}], "_slug": "test-hook"}
    ]
  },
  "customKey": 42
}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := ReadSettings(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify hooks are present.
	hooks := s.Hooks()
	if _, ok := hooks["PreToolUse"]; !ok {
		t.Fatal("expected PreToolUse event in hooks")
	}

	// Verify other keys are preserved.
	if s.raw["customKey"] == nil {
		t.Fatal("expected customKey to be preserved")
	}
	if s.raw["permissions"] == nil {
		t.Fatal("expected permissions to be preserved")
	}
}

// ---------------------------------------------------------------------------
// MergeHook
// ---------------------------------------------------------------------------

func TestMergeHook_AddsNewEvents(t *testing.T) {
	s := &Settings{raw: make(map[string]interface{})}

	fragment := map[string]interface{}{
		"PreToolUse": []interface{}{
			map[string]interface{}{
				"matcher": "Bash",
				"hooks":   []interface{}{map[string]interface{}{"type": "command", "command": "echo test"}},
				"_slug":   "my-hook",
			},
		},
	}

	s.MergeHook(fragment, "my-hook", false)

	hooks := s.Hooks()
	matchers, ok := hooks["PreToolUse"].([]interface{})
	if !ok {
		t.Fatalf("expected PreToolUse to be a slice, got %T", hooks["PreToolUse"])
	}
	if len(matchers) != 1 {
		t.Fatalf("expected 1 matcher, got %d", len(matchers))
	}
}

func TestMergeHook_AppendsWithoutDuplicating(t *testing.T) {
	s := &Settings{raw: map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{
				map[string]interface{}{
					"matcher": "Bash",
					"hooks":   []interface{}{map[string]interface{}{"type": "command", "command": "echo existing"}},
					"_slug":   "existing-hook",
				},
			},
		},
	}}

	fragment := map[string]interface{}{
		"PreToolUse": []interface{}{
			map[string]interface{}{
				"matcher": "Read",
				"hooks":   []interface{}{map[string]interface{}{"type": "command", "command": "echo new"}},
				"_slug":   "new-hook",
			},
		},
	}
	s.MergeHook(fragment, "new-hook", false)

	hooks := s.Hooks()
	matchers, _ := hooks["PreToolUse"].([]interface{})
	if len(matchers) != 2 {
		t.Fatalf("expected 2 matchers after append, got %d", len(matchers))
	}

	// Merge the same fragment again — should not duplicate.
	s.MergeHook(fragment, "new-hook", false)
	hooks = s.Hooks()
	matchers, _ = hooks["PreToolUse"].([]interface{})
	if len(matchers) != 2 {
		t.Fatalf("expected 2 matchers after dedup, got %d", len(matchers))
	}
}

func TestMergeHook_ForceReplacesEntries(t *testing.T) {
	s := &Settings{raw: map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{
				map[string]interface{}{
					"matcher": "Bash",
					"hooks":   []interface{}{map[string]interface{}{"type": "command", "command": "echo v1"}},
					"_slug":   "my-hook",
				},
				map[string]interface{}{
					"matcher": "Read",
					"hooks":   []interface{}{map[string]interface{}{"type": "command", "command": "echo other"}},
					"_slug":   "other-hook",
				},
			},
		},
	}}

	// Force-replace my-hook with a new version.
	fragment := map[string]interface{}{
		"PreToolUse": []interface{}{
			map[string]interface{}{
				"matcher": "Bash",
				"hooks":   []interface{}{map[string]interface{}{"type": "command", "command": "echo v2"}},
				"_slug":   "my-hook",
			},
		},
	}
	s.MergeHook(fragment, "my-hook", true)

	hooks := s.Hooks()
	matchers, _ := hooks["PreToolUse"].([]interface{})
	if len(matchers) != 2 {
		t.Fatalf("expected 2 matchers (other-hook + new my-hook), got %d", len(matchers))
	}

	// Verify the remaining my-hook has v2.
	found := false
	for _, m := range matchers {
		mMap := m.(map[string]interface{})
		if mMap["_slug"] == "my-hook" {
			hooksArr := mMap["hooks"].([]interface{})
			cmd := hooksArr[0].(map[string]interface{})["command"]
			if cmd != "echo v2" {
				t.Fatalf("expected v2 command, got %v", cmd)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("my-hook entry not found after force merge")
	}
}

// ---------------------------------------------------------------------------
// RemoveHook
// ---------------------------------------------------------------------------

func TestRemoveHook_RemovesCorrectEntries(t *testing.T) {
	s := &Settings{raw: map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{
				map[string]interface{}{"matcher": "Bash", "_slug": "remove-me"},
				map[string]interface{}{"matcher": "Read", "_slug": "keep-me"},
			},
			"PostToolUse": []interface{}{
				map[string]interface{}{"matcher": "Write", "_slug": "remove-me"},
			},
		},
	}}

	tracker := &Tracker{
		Hooks: []TrackedHook{
			{Slug: "remove-me", Events: []string{"PreToolUse", "PostToolUse"}},
		},
	}

	removed := s.RemoveHook(tracker, "remove-me")
	if !removed {
		t.Fatal("expected RemoveHook to return true")
	}

	hooks := s.Hooks()

	// PreToolUse should still exist with keep-me.
	matchers, ok := hooks["PreToolUse"].([]interface{})
	if !ok {
		t.Fatal("expected PreToolUse to still exist")
	}
	if len(matchers) != 1 {
		t.Fatalf("expected 1 matcher remaining, got %d", len(matchers))
	}

	// PostToolUse should be cleaned up (empty array removed).
	if _, ok := hooks["PostToolUse"]; ok {
		t.Fatal("expected PostToolUse to be removed (was empty)")
	}
}

func TestRemoveHook_UnknownSlug(t *testing.T) {
	s := &Settings{raw: map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{
				map[string]interface{}{"matcher": "Bash", "_slug": "keep-me"},
			},
		},
	}}
	tracker := &Tracker{Hooks: []TrackedHook{}}

	removed := s.RemoveHook(tracker, "nonexistent")
	if removed {
		t.Fatal("expected RemoveHook to return false for unknown slug")
	}
}

// ---------------------------------------------------------------------------
// Write
// ---------------------------------------------------------------------------

func TestWrite_CreatesDirectoriesAndWritesAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "settings.json")

	s := &Settings{raw: map[string]interface{}{
		"hooks":       map[string]interface{}{},
		"permissions": map[string]interface{}{"allow": []interface{}{"Read"}},
	}}

	if err := s.Write(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify file exists and is valid JSON.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("written file is not valid JSON: %v", err)
	}

	if parsed["permissions"] == nil {
		t.Fatal("expected permissions key in written file")
	}

	// Verify no tmp file left behind.
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatal("expected tmp file to be cleaned up")
	}
}

// ---------------------------------------------------------------------------
// Checksum
// ---------------------------------------------------------------------------

func TestChecksum_Deterministic(t *testing.T) {
	events := map[string]interface{}{
		"PreToolUse": []interface{}{
			map[string]interface{}{"matcher": "Bash"},
		},
		"PostToolUse": []interface{}{
			map[string]interface{}{"matcher": "Read"},
		},
	}

	c1 := Checksum(events)
	c2 := Checksum(events)
	if c1 != c2 {
		t.Fatalf("checksums not deterministic: %s != %s", c1, c2)
	}
	if c1 == "" {
		t.Fatal("checksum should not be empty")
	}

	// Different events should produce different checksum.
	events2 := map[string]interface{}{
		"PreToolUse": []interface{}{
			map[string]interface{}{"matcher": "Write"},
		},
	}
	c3 := Checksum(events2)
	if c1 == c3 {
		t.Fatal("different events should produce different checksums")
	}
}

// ---------------------------------------------------------------------------
// Tracker: Add / Remove / Get
// ---------------------------------------------------------------------------

func TestTracker_AddRemoveGet(t *testing.T) {
	tracker := &Tracker{
		Hooks: []TrackedHook{},
		path:  filepath.Join(t.TempDir(), "tracker.json"),
	}

	// Add.
	hook := TrackedHook{
		Slug:        "test-hook",
		Version:     1,
		InstalledAt: "2026-01-01T00:00:00Z",
		Events:      []string{"PreToolUse"},
		Checksum:    "abc123",
	}
	tracker.Add(hook)

	// Get.
	got := tracker.Get("test-hook")
	if got == nil {
		t.Fatal("expected to find test-hook")
	}
	if got.Version != 1 {
		t.Fatalf("expected version 1, got %d", got.Version)
	}

	// Get nonexistent.
	if tracker.Get("nonexistent") != nil {
		t.Fatal("expected nil for nonexistent slug")
	}

	// Update via Add.
	hook.Version = 2
	tracker.Add(hook)
	got = tracker.Get("test-hook")
	if got.Version != 2 {
		t.Fatalf("expected version 2 after update, got %d", got.Version)
	}
	if len(tracker.Hooks) != 1 {
		t.Fatalf("expected 1 hook after update, got %d", len(tracker.Hooks))
	}

	// EventsForSlug.
	events := tracker.EventsForSlug("test-hook")
	if len(events) != 1 || events[0] != "PreToolUse" {
		t.Fatalf("unexpected events: %v", events)
	}
	if tracker.EventsForSlug("nonexistent") != nil {
		t.Fatal("expected nil events for nonexistent slug")
	}

	// Remove.
	if !tracker.Remove("test-hook") {
		t.Fatal("expected Remove to return true")
	}
	if tracker.Get("test-hook") != nil {
		t.Fatal("expected hook to be removed")
	}
	if tracker.Remove("test-hook") {
		t.Fatal("expected Remove to return false for already-removed slug")
	}
}

// ---------------------------------------------------------------------------
// Tracker: Save / Load round-trip
// ---------------------------------------------------------------------------

func TestTracker_SaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude", ".promptvm-hooks.json")

	tracker := &Tracker{
		Hooks: []TrackedHook{
			{
				Slug:        "hook-a",
				Version:     3,
				SourceURL:   "https://example.com/hook-a",
				InstalledAt: "2026-06-01T12:00:00Z",
				Events:      []string{"PreToolUse", "PostToolUse"},
				Checksum:    "deadbeef",
			},
			{
				Slug:        "hook-b",
				Version:     1,
				InstalledAt: "2026-06-02T08:00:00Z",
				Events:      []string{"Notification"},
				Checksum:    "cafebabe",
			},
		},
		path: path,
	}

	if err := tracker.Save(); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := LoadTrackerFromPath(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if len(loaded.Hooks) != 2 {
		t.Fatalf("expected 2 hooks, got %d", len(loaded.Hooks))
	}

	a := loaded.Get("hook-a")
	if a == nil {
		t.Fatal("expected hook-a")
	}
	if a.Version != 3 || a.SourceURL != "https://example.com/hook-a" {
		t.Fatalf("hook-a data mismatch: %+v", a)
	}
	if len(a.Events) != 2 {
		t.Fatalf("expected 2 events for hook-a, got %d", len(a.Events))
	}

	b := loaded.Get("hook-b")
	if b == nil {
		t.Fatal("expected hook-b")
	}
	if b.Checksum != "cafebabe" {
		t.Fatalf("hook-b checksum mismatch: %s", b.Checksum)
	}
}

func TestTracker_LoadMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.json")
	tracker, err := LoadTrackerFromPath(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tracker.Hooks) != 0 {
		t.Fatalf("expected empty hooks, got %d", len(tracker.Hooks))
	}
}

// ---------------------------------------------------------------------------
// SettingsFilePath / TrackerFilePath
// ---------------------------------------------------------------------------

func TestSettingsFilePath(t *testing.T) {
	_, err := SettingsFilePath(ScopeProject)
	if err != nil {
		t.Fatalf("unexpected error for project scope: %v", err)
	}
	_, err = SettingsFilePath(ScopeUser)
	if err != nil {
		t.Fatalf("unexpected error for user scope: %v", err)
	}
	_, err = SettingsFilePath(Scope("invalid"))
	if err == nil {
		t.Fatal("expected error for invalid scope")
	}
}

func TestTrackerFilePath(t *testing.T) {
	_, err := TrackerFilePath(ScopeProject)
	if err != nil {
		t.Fatalf("unexpected error for project scope: %v", err)
	}
	_, err = TrackerFilePath(ScopeUser)
	if err != nil {
		t.Fatalf("unexpected error for user scope: %v", err)
	}
	_, err = TrackerFilePath(Scope("invalid"))
	if err == nil {
		t.Fatal("expected error for invalid scope")
	}
}
