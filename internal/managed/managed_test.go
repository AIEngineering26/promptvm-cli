package managed

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectAbsent(t *testing.T) {
	pathOverride = filepath.Join(t.TempDir(), "nope.json")
	t.Cleanup(func() { pathOverride = "" })

	p, err := Detect()
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if p.Present {
		t.Errorf("expected absent policy")
	}
}

func TestDetectDisableAllHooks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "managed-settings.json")
	if err := os.WriteFile(path, []byte(`{"disableAllHooks": true, "other": 1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	pathOverride = path
	t.Cleanup(func() { pathOverride = "" })

	p, err := Detect()
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !p.Present || !p.DisableAllHooks {
		t.Errorf("expected present + disableAllHooks, got %+v", p)
	}
	if !HooksDisabled() {
		t.Errorf("HooksDisabled() = false, want true")
	}
}

func TestDetectHooksEnabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "managed-settings.json")
	if err := os.WriteFile(path, []byte(`{"disableAllHooks": false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	pathOverride = path
	t.Cleanup(func() { pathOverride = "" })

	if HooksDisabled() {
		t.Errorf("HooksDisabled() = true, want false")
	}
}
