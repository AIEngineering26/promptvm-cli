package gitutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestEnsureGitignoreIdempotent(t *testing.T) {
	root := t.TempDir()
	pattern := ".promptvm/config.local.json"

	added, err := EnsureGitignore(root, pattern)
	if err != nil {
		t.Fatalf("EnsureGitignore: %v", err)
	}
	if !added {
		t.Errorf("first call should add the pattern")
	}

	// Second call must be a no-op.
	added2, err := EnsureGitignore(root, pattern)
	if err != nil {
		t.Fatal(err)
	}
	if added2 {
		t.Errorf("second call should not re-add the pattern")
	}

	data, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	count := 0
	for _, line := range splitLines(string(data)) {
		if line == pattern {
			count++
		}
	}
	if count != 1 {
		t.Errorf("pattern appears %d times, want 1", count)
	}
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	out = append(out, cur)
	return out
}

func TestDetectNonRepo(t *testing.T) {
	dir := t.TempDir()
	_, ok := Detect(dir)
	if ok {
		t.Errorf("temp dir should not be a git repo")
	}
}

func TestDetectRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"remote", "add", "origin", "git@github.com:acme/widgets.git"},
	} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	repo, ok := Detect(dir)
	if !ok {
		t.Fatal("expected git repo detected")
	}
	if repo.RemoteURL != "git@github.com:acme/widgets.git" {
		t.Errorf("RemoteURL = %q", repo.RemoteURL)
	}
}
