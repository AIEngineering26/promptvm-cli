package installs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecordAndLoad(t *testing.T) {
	root := t.TempDir()
	if err := Record(root, Entry{Name: "pdf", Ref: "acme/pdf", Kind: "skill", Target: "/x/skills/pdf"}); err != nil {
		t.Fatalf("record: %v", err)
	}
	t2, err := Load(PathIn(root))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(t2.Installs) != 1 || t2.Installs[0].Ref != "acme/pdf" {
		t.Fatalf("unexpected entries: %+v", t2.Installs)
	}
}

func TestUpsertByRef(t *testing.T) {
	root := t.TempDir()
	_ = Record(root, Entry{Name: "pdf", Ref: "acme/pdf", Kind: "skill", Target: "old"})
	_ = Record(root, Entry{Name: "pdf", Ref: "acme/pdf", Kind: "skill", Target: "new"})
	tr, _ := Load(PathIn(root))
	if len(tr.Installs) != 1 {
		t.Fatalf("re-install should upsert, got %d entries", len(tr.Installs))
	}
	if tr.Installs[0].Target != "new" {
		t.Errorf("upsert should replace, got %q", tr.Installs[0].Target)
	}
}

func TestLoadMissingFile(t *testing.T) {
	tr, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing file should be empty tracker, got %v", err)
	}
	if len(tr.Installs) != 0 {
		t.Errorf("expected empty, got %+v", tr.Installs)
	}
}

func TestUpsertByKindNameWhenNoRef(t *testing.T) {
	root := t.TempDir()
	_ = Record(root, Entry{Name: "pdf", Kind: "prompt", Target: "old"})
	_ = Record(root, Entry{Name: "pdf", Kind: "prompt", Target: "new"})
	tr, _ := Load(PathIn(root))
	if len(tr.Installs) != 1 || tr.Installs[0].Target != "new" {
		t.Errorf("expected single upserted entry, got %+v", tr.Installs)
	}
	// Sanity: the sidecar file exists at the documented path.
	if _, err := os.Stat(PathIn(root)); err != nil {
		t.Errorf("sidecar not at PathIn: %v", err)
	}
}
