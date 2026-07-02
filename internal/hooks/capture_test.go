package hooks

import (
	"path/filepath"
	"testing"
)

func TestScopeLocalPaths(t *testing.T) {
	sp, err := SettingsFilePath(ScopeLocal)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(sp) != "settings.local.json" {
		t.Errorf("local settings basename = %q, want settings.local.json", filepath.Base(sp))
	}
	tp, err := TrackerFilePath(ScopeLocal)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(tp) != ".promptvm-hooks.local.json" {
		t.Errorf("local tracker basename = %q", filepath.Base(tp))
	}
}

func TestBuildCaptureFragmentShape(t *testing.T) {
	frag := BuildCaptureFragment([]string{"SessionEnd", "SessionStart"})
	if len(frag) != 2 {
		t.Fatalf("fragment events = %d, want 2", len(frag))
	}
	group, ok := frag["SessionEnd"].([]interface{})
	if !ok || len(group) != 1 {
		t.Fatalf("SessionEnd group malformed: %v", frag["SessionEnd"])
	}
	g, ok := group[0].(map[string]interface{})
	if !ok {
		t.Fatalf("SessionEnd entry malformed: %v", group[0])
	}
	if g["_slug"] != CaptureHookSlug {
		t.Errorf("_slug = %v, want %s", g["_slug"], CaptureHookSlug)
	}
	handlers, ok := g["hooks"].([]interface{})
	if !ok || len(handlers) != 1 {
		t.Fatalf("handlers malformed: %v", g["hooks"])
	}
	h, ok := handlers[0].(map[string]interface{})
	if !ok {
		t.Fatalf("handler entry malformed: %v", handlers[0])
	}
	if h["type"] != "command" || h["command"] != CaptureHookCommand {
		t.Errorf("handler = %v", h)
	}
}

func TestCaptureEventsInstalledRoundTrip(t *testing.T) {
	frag := BuildCaptureFragment([]string{"SessionEnd", "PreCompact"})
	s := &Settings{raw: map[string]interface{}{}}
	s.MergeHook(frag, CaptureHookSlug, false)

	events := s.CaptureEventsInstalled()
	if len(events) != 2 {
		t.Fatalf("installed events = %v, want 2", events)
	}

	// Idempotent: merging again does not duplicate.
	s.MergeHook(frag, CaptureHookSlug, true)
	if got := s.CaptureEventsInstalled(); len(got) != 2 {
		t.Errorf("after re-merge installed events = %v, want 2", got)
	}
	hooks := s.Hooks()
	if list, ok := hooks["SessionEnd"].([]interface{}); ok && len(list) != 1 {
		t.Errorf("SessionEnd matcher groups = %d, want 1 (idempotent)", len(list))
	}
}
