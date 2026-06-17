package agentskill

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AIEngineering26/promptvm-cli/internal/skills"
)

// withTempHome redirects HOME, CODEX_HOME, and the tracker config dir to
// temporary directories for the duration of a test, and returns the home dir.
func withTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows
	t.Setenv("CODEX_HOME", "")    // exercise the ~/.agents fallback by default

	cfg := t.TempDir()
	orig := configDir
	configDir = func() (string, error) { return cfg, nil }
	t.Cleanup(func() { configDir = orig })

	return home
}

func TestBundledSkillParsesAndValidates(t *testing.T) {
	md, err := content.ReadFile(embedRoot + "/SKILL.md")
	if err != nil {
		t.Fatalf("reading embedded SKILL.md: %v", err)
	}
	fm, err := skills.ParseFrontmatter(md)
	if err != nil {
		t.Fatalf("parsing frontmatter: %v", err)
	}
	if fm.Name != Name {
		t.Errorf("frontmatter name = %q, want %q", fm.Name, Name)
	}
	if err := skills.ValidateName(fm.Name); err != nil {
		t.Errorf("ValidateName(%q): %v", fm.Name, err)
	}
	if fm.Description == "" {
		t.Error("frontmatter description is empty")
	}
}

func TestBaseDir(t *testing.T) {
	home := withTempHome(t)

	claude, _ := TargetByKey("claude")
	codex, _ := TargetByKey("codex")

	if got, _ := claude.BaseDir(ScopeUser); got != filepath.Join(home, ".claude", "skills") {
		t.Errorf("claude user BaseDir = %q", got)
	}
	if got, _ := codex.BaseDir(ScopeUser); got != filepath.Join(home, ".agents", "skills") {
		t.Errorf("codex user BaseDir = %q", got)
	}

	// CODEX_HOME override applies only to user scope.
	t.Setenv("CODEX_HOME", filepath.Join(home, "codexcfg"))
	if got, _ := codex.BaseDir(ScopeUser); got != filepath.Join(home, "codexcfg", "skills") {
		t.Errorf("codex user BaseDir with CODEX_HOME = %q", got)
	}
}

func TestInstallWritesBundledFiles(t *testing.T) {
	withTempHome(t)

	installed, err := Install(ScopeUser, AllTargets(), false)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(installed) != 2 {
		t.Fatalf("installed %d targets, want 2", len(installed))
	}

	bundled, err := content.ReadFile(embedRoot + "/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}

	for _, it := range installed {
		got, err := os.ReadFile(filepath.Join(it.Path, "SKILL.md"))
		if err != nil {
			t.Fatalf("reading installed SKILL.md for %s: %v", it.Key, err)
		}
		if string(got) != string(bundled) {
			t.Errorf("%s SKILL.md not byte-identical to bundled", it.Key)
		}
		if filepath.Base(it.Path) != Name {
			t.Errorf("install path %q does not end in %q", it.Path, Name)
		}
	}
}

func TestInstallIdempotentAndForce(t *testing.T) {
	withTempHome(t)
	targets := []Target{mustTarget(t, "claude")}

	if _, err := Install(ScopeUser, targets, false); err != nil {
		t.Fatalf("first install: %v", err)
	}
	// Re-install without force: checksum matches → no-op, no error.
	if _, err := Install(ScopeUser, targets, false); err != nil {
		t.Fatalf("idempotent re-install: %v", err)
	}

	// Mutate the installed file so checksum no longer matches.
	dest, _ := targets[0].DestDir(ScopeUser)
	if err := os.WriteFile(filepath.Join(dest, "SKILL.md"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(ScopeUser, targets, false); err == nil {
		t.Error("expected error re-installing over a modified folder without --force")
	}
	if _, err := Install(ScopeUser, targets, true); err != nil {
		t.Errorf("force re-install: %v", err)
	}
}

func TestUninstallOnlyRemovesNamedFolders(t *testing.T) {
	withTempHome(t)
	installed, err := Install(ScopeUser, AllTargets(), false)
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, len(installed))
	for _, it := range installed {
		paths = append(paths, it.Path)
	}

	// A path not ending in the skill name must be ignored.
	other := filepath.Join(t.TempDir(), "not-promptvm")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall(append(paths, other)); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	for _, p := range paths {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("expected %q removed, err=%v", p, err)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("non-skill folder %q should be untouched: %v", other, err)
	}
}

func TestTrackerRoundTrip(t *testing.T) {
	withTempHome(t)

	if exists, _ := Exists(); exists {
		t.Fatal("marker should not exist initially")
	}
	if tr, _ := LoadTracker(); tr != nil {
		t.Fatal("LoadTracker should return nil when absent")
	}

	tr := &Tracker{
		Name:     Name,
		Version:  Version,
		Checksum: Checksum(),
		Status:   StatusInstalled,
		Targets:  []TrackedTarget{{Key: "claude", Path: "/tmp/x/promptvm"}},
	}
	if err := tr.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if exists, _ := Exists(); !exists {
		t.Fatal("marker should exist after Save")
	}

	got, err := LoadTracker()
	if err != nil || got == nil {
		t.Fatalf("LoadTracker: %v", err)
	}
	if got.Status != StatusInstalled || got.Version != Version || len(got.Targets) != 1 {
		t.Errorf("round-trip mismatch: %+v", got)
	}

	if err := Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if exists, _ := Exists(); exists {
		t.Error("marker should be gone after Clear")
	}
	// Clearing an absent marker is not an error.
	if err := Clear(); err != nil {
		t.Errorf("Clear on absent marker: %v", err)
	}
}

func TestFilesIncludesSkillMD(t *testing.T) {
	files, err := Files()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range files {
		if f == "SKILL.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("Files() missing SKILL.md: %v", files)
	}
}

func TestBaseDirProjectScope(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	wd, err := os.Getwd() // resolve symlinks (macOS /var → /private/var)
	if err != nil {
		t.Fatal(err)
	}
	// CODEX_HOME must be ignored for project scope even when absolute.
	t.Setenv("CODEX_HOME", filepath.Join(dir, "ignored"))

	claude := mustTarget(t, "claude")
	codex := mustTarget(t, "codex")
	if got, _ := claude.BaseDir(ScopeProject); got != filepath.Join(wd, ".claude", "skills") {
		t.Errorf("claude project BaseDir = %q", got)
	}
	if got, _ := codex.BaseDir(ScopeProject); got != filepath.Join(wd, ".agents", "skills") {
		t.Errorf("codex project BaseDir = %q", got)
	}
}

func TestCodexHomeMustBeAbsolute(t *testing.T) {
	home := withTempHome(t)
	codex := mustTarget(t, "codex")
	// A relative CODEX_HOME is rejected; falls back to ~/.agents/skills.
	t.Setenv("CODEX_HOME", "relative/path")
	if got, _ := codex.BaseDir(ScopeUser); got != filepath.Join(home, ".agents", "skills") {
		t.Errorf("relative CODEX_HOME should be ignored, got %q", got)
	}
}

func TestInstallRepairsFolderWithoutSkillMD(t *testing.T) {
	withTempHome(t)
	tg := mustTarget(t, "claude")
	dest, _ := tg.DestDir(ScopeUser)
	if err := os.MkdirAll(dest, 0o755); err != nil { // empty folder, no SKILL.md
		t.Fatal(err)
	}
	if _, err := Install(ScopeUser, []Target{tg}, false); err != nil {
		t.Fatalf("expected repair of empty folder, got error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md not written during repair: %v", err)
	}
}

func TestInstallBestEffortSkipsConflict(t *testing.T) {
	withTempHome(t)
	// Pre-create a conflicting claude folder with different content.
	claude := mustTarget(t, "claude")
	cdest, _ := claude.DestDir(ScopeUser)
	if err := os.MkdirAll(cdest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cdest, "SKILL.md"), []byte("different content"), 0o644); err != nil {
		t.Fatal(err)
	}

	installed := InstallBestEffort(ScopeUser, AllTargets())
	if len(installed) != 1 || installed[0].Key != "codex" {
		t.Fatalf("expected only codex installed (claude conflicts), got %+v", installed)
	}
	// The conflicting claude file must be left untouched.
	got, _ := os.ReadFile(filepath.Join(cdest, "SKILL.md"))
	if string(got) != "different content" {
		t.Error("best-effort install clobbered a conflicting folder")
	}
}

func TestUninstallSkipsSymlinkedLeaf(t *testing.T) {
	withTempHome(t)
	base := t.TempDir()
	real := filepath.Join(base, "realdir")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, Name) // a symlink whose base == "promptvm"
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if err := Uninstall([]string{link}); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("symlinked leaf should not have been removed: %v", err)
	}
	if _, err := os.Stat(real); err != nil {
		t.Errorf("symlink target should be untouched: %v", err)
	}
}

func TestLoadTrackerMalformed(t *testing.T) {
	withTempHome(t)
	p, err := TrackerPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{ not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTracker(); err == nil {
		t.Error("expected error loading malformed marker")
	}
}

func mustTarget(t *testing.T, key string) Target {
	t.Helper()
	tg, ok := TargetByKey(key)
	if !ok {
		t.Fatalf("unknown target %q", key)
	}
	return tg
}
