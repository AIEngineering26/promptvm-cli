package cmd

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// TestBuildSkillsUpdateBody verifies the PATCH body build + --base-version
// wiring: base_version is included only when set (>0), and skill_md/files are
// always carried through.
func TestBuildSkillsUpdateBody(t *testing.T) {
	md := []byte("---\nname: my-skill\ndescription: d\n---\nbody\n")
	manifest := []skillFileEntry{{Path: "ref/a.txt", ResourceID: "res_1"}}

	t.Run("base-version omitted when zero", func(t *testing.T) {
		body := buildSkillsUpdateBody(md, manifest, 0)
		if body.SkillMD != string(md) {
			t.Errorf("skill_md not preserved: got %q", body.SkillMD)
		}
		if len(body.Files) != 1 || body.Files[0].ResourceID != "res_1" {
			t.Errorf("files manifest not carried through: %+v", body.Files)
		}
		if body.BaseVersion != nil {
			t.Errorf("base_version should be nil when 0, got %v", *body.BaseVersion)
		}
	})

	t.Run("base-version forwarded when set", func(t *testing.T) {
		body := buildSkillsUpdateBody(md, manifest, 3)
		if body.BaseVersion == nil {
			t.Fatal("base_version should be set")
		}
		if *body.BaseVersion != 3 {
			t.Errorf("base_version = %d, want 3", *body.BaseVersion)
		}
	})

	t.Run("negative base-version omitted", func(t *testing.T) {
		body := buildSkillsUpdateBody(md, nil, -1)
		if body.BaseVersion != nil {
			t.Errorf("negative base_version should be nil, got %v", *body.BaseVersion)
		}
	})
}

// TestSkillsUpdateMissingFolder ensures a non-existent folder fails fast with a
// non-zero exit before any network call.
func TestSkillsUpdateMissingFolder(t *testing.T) {
	cmd := newSkillsUpdateCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"sk_1", "/nonexistent/skill/folder/path"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for missing folder, got nil")
	}
}

// TestSkillsUpdateNotADirectory ensures pointing update at a regular file (not a
// folder) errors with an actionable message.
func TestSkillsUpdateNotADirectory(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(tmp, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := newSkillsUpdateCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"sk_1", tmp})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for non-directory, got nil")
	}
}
