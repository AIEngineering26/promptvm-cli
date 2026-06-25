package ctxblock

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpsertCreatesThenReplacesIdempotently(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(path, []byte("# My project\n\nSome notes.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	block1 := Render([]string{"fixed the auth bug", "added retry logic"})
	replaced, err := Upsert(path, block1)
	if err != nil {
		t.Fatal(err)
	}
	if replaced {
		t.Errorf("first upsert should append, not replace")
	}

	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "# My project") {
		t.Errorf("original content lost")
	}
	if !strings.Contains(string(got), "fixed the auth bug") {
		t.Errorf("block not written")
	}

	// Second upsert with new content must REPLACE in place, not duplicate.
	block2 := Render([]string{"refactored the parser"})
	replaced, err = Upsert(path, block2)
	if err != nil {
		t.Fatal(err)
	}
	if !replaced {
		t.Errorf("second upsert should replace")
	}
	got2, _ := os.ReadFile(path)
	if strings.Count(string(got2), startMarker) != 1 {
		t.Errorf("managed block duplicated: %q", string(got2))
	}
	if strings.Contains(string(got2), "fixed the auth bug") {
		t.Errorf("old block content not replaced")
	}
	if !strings.Contains(string(got2), "refactored the parser") {
		t.Errorf("new block content missing")
	}
}

func TestUpsertCreatesFileWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".promptvm", "context.md")
	if _, err := Upsert(path, Render([]string{"hello"})); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "hello") {
		t.Errorf("file not created with block")
	}
}
