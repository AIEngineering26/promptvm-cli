package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanReconstruct(t *testing.T) {
	cases := []struct {
		name      string
		dir       string
		bundle    Bundle
		wantErr   string
		wantFiles []string
	}{
		{
			name: "valid nested paths",
			dir:  "/tmp/skill",
			bundle: Bundle{
				RawSkillMD: "---\nname: x\n---\n",
				Files: []BundleFile{
					{Path: "scripts/run.sh"},
					{Path: "data/notes.txt"},
				},
			},
			wantFiles: []string{
				filepath.Join("/tmp/skill", "scripts", "run.sh"),
				filepath.Join("/tmp/skill", "data", "notes.txt"),
			},
		},
		{
			name:    "path escape rejected",
			dir:     "/tmp/skill",
			bundle:  Bundle{Files: []BundleFile{{Path: "../evil.sh"}}},
			wantErr: "escapes the destination directory",
		},
		{
			name:    "absolute path rejected",
			dir:     "/tmp/skill",
			bundle:  Bundle{Files: []BundleFile{{Path: "/etc/passwd"}}},
			wantErr: "escapes the destination directory",
		},
		{
			name:    "backslash rejected",
			dir:     "/tmp/skill",
			bundle:  Bundle{Files: []BundleFile{{Path: "a\\b"}}},
			wantErr: "backslashes are not allowed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mdDest, fileDests, err := PlanReconstruct(tc.dir, tc.bundle)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("want err containing %q, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if mdDest != filepath.Join(tc.dir, "SKILL.md") {
				t.Errorf("SKILL.md dest = %q", mdDest)
			}
			if len(fileDests) != len(tc.wantFiles) {
				t.Fatalf("got %d dests, want %d", len(fileDests), len(tc.wantFiles))
			}
			for i, want := range tc.wantFiles {
				if fileDests[i] != want {
					t.Errorf("dest[%d] = %q, want %q", i, fileDests[i], want)
				}
			}
		})
	}
}

func TestReconstructWritesBundle(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-skill")
	bundle := Bundle{
		RawSkillMD: "---\nname: my-skill\n---\nbody",
		Files: []BundleFile{
			{Path: "scripts/run.sh", DownloadURL: "http://x/run", SizeBytes: 3},
			{Path: "README.txt", DownloadURL: "http://x/readme", SizeBytes: 5},
		},
	}

	contents := map[string]string{
		"http://x/run":    "echo",
		"http://x/readme": "hello",
	}
	dl := func(url, dest string) error {
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dest, []byte(contents[url]), 0o644)
	}

	written, err := Reconstruct(dir, bundle, dl)
	if err != nil {
		t.Fatalf("Reconstruct: %v", err)
	}
	if len(written) != 3 { // SKILL.md + 2 files
		t.Fatalf("want 3 written, got %d", len(written))
	}

	md, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil || string(md) != bundle.RawSkillMD {
		t.Errorf("SKILL.md mismatch: %q err=%v", md, err)
	}
	run, _ := os.ReadFile(filepath.Join(dir, "scripts", "run.sh"))
	if string(run) != "echo" {
		t.Errorf("run.sh = %q", run)
	}
}

func TestReconstructRejectsUnsafePathBeforeWriting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-skill")
	bundle := Bundle{
		RawSkillMD: "x",
		Files:      []BundleFile{{Path: "../escape", DownloadURL: "http://x"}},
	}
	called := false
	dl := func(url, dest string) error { called = true; return nil }

	if _, err := Reconstruct(dir, bundle, dl); err == nil {
		t.Fatal("expected error for unsafe path")
	}
	if called {
		t.Error("downloader should not be called when a path is unsafe")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("no directory should be created when a path is unsafe")
	}
}

func TestReconstructMissingDownloadURL(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-skill")
	bundle := Bundle{RawSkillMD: "x", Files: []BundleFile{{Path: "a.txt"}}}
	dl := func(url, dest string) error { return nil }
	if _, err := Reconstruct(dir, bundle, dl); err == nil || !strings.Contains(err.Error(), "no download URL") {
		t.Fatalf("want missing-download-URL error, got %v", err)
	}
}
