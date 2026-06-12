package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"simple", "pdf-tools", false},
		{"single char", "a", false},
		{"digit start", "7zip-helper", false},
		{"max length 64", strings.Repeat("a", 64), false},
		{"empty", "", true},
		{"too long 65", strings.Repeat("a", 65), true},
		{"uppercase", "PDF-Tools", true},
		{"underscore", "pdf_tools", true},
		{"leading hyphen", "-pdf", true},
		{"spaces", "pdf tools", true},
		{"dots", "pdf.tools", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateName(tc.in)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateName(%q) error = %v, wantErr %v", tc.in, err, tc.wantErr)
			}
		})
	}
}

func TestParseFrontmatter(t *testing.T) {
	cases := []struct {
		name     string
		md       string
		wantName string
		wantDesc string
		wantErr  bool
	}{
		{
			name:     "valid",
			md:       "---\nname: pdf-tools\ndescription: Work with PDFs\n---\n\n# PDF Tools\n",
			wantName: "pdf-tools",
			wantDesc: "Work with PDFs",
		},
		{
			name:     "crlf line endings",
			md:       "---\r\nname: pdf-tools\r\n---\r\nbody\r\n",
			wantName: "pdf-tools",
		},
		{
			name:    "no frontmatter",
			md:      "# Just markdown\n",
			wantErr: true,
		},
		{
			name:    "unclosed frontmatter",
			md:      "---\nname: pdf-tools\n",
			wantErr: true,
		},
		{
			name:    "empty file",
			md:      "",
			wantErr: true,
		},
		{
			name:    "invalid yaml",
			md:      "---\nname: [unbalanced\n---\nbody\n",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fm, err := ParseFrontmatter([]byte(tc.md))
			if (err != nil) != tc.wantErr {
				t.Fatalf("ParseFrontmatter error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if fm.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", fm.Name, tc.wantName)
			}
			if fm.Description != tc.wantDesc {
				t.Errorf("Description = %q, want %q", fm.Description, tc.wantDesc)
			}
		})
	}
}

func TestWalk(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("SKILL.md", "---\nname: demo\n---\nbody")
	write("reference.md", "ref")
	write("scripts/run.py", "print('hi')")
	write("assets/deep/logo.png", "png")
	write(".DS_Store", "junk")
	write(".git/config", "junk")
	write("scripts/.hidden", "junk")
	// A nested SKILL.md is a regular bundled file — only the root one is skipped.
	write("nested/SKILL.md", "nested")

	files, err := Walk(root)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	want := []string{
		"assets/deep/logo.png",
		"nested/SKILL.md",
		"reference.md",
		"scripts/run.py",
	}
	if len(files) != len(want) {
		t.Fatalf("Walk returned %d files, want %d: %+v", len(files), len(want), files)
	}
	for i, w := range want {
		if files[i].Path != w {
			t.Errorf("files[%d].Path = %q, want %q", i, files[i].Path, w)
		}
		if files[i].Size <= 0 {
			t.Errorf("files[%d].Size = %d, want > 0", i, files[i].Size)
		}
		if files[i].AbsPath == "" {
			t.Errorf("files[%d].AbsPath is empty", i)
		}
	}
}

func TestWalkEmptyFolder(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("---\nname: x\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := Walk(root)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("Walk returned %d files, want 0", len(files))
	}
}

func TestSafeJoin(t *testing.T) {
	dir := filepath.FromSlash("/tmp/dest")
	cases := []struct {
		name    string
		rel     string
		want    string // forward-slash form of expected suffix
		wantErr bool
	}{
		{"simple", "reference.md", "/tmp/dest/reference.md", false},
		{"nested", "scripts/run.py", "/tmp/dest/scripts/run.py", false},
		{"dot prefix ok", "./a.txt", "/tmp/dest/a.txt", false},
		{"internal dotdot resolving inside", "a/../b.txt", "/tmp/dest/b.txt", false},
		{"empty", "", "", true},
		{"absolute", "/etc/passwd", "", true},
		{"parent escape", "../evil.txt", "", true},
		{"deep escape", "a/../../evil.txt", "", true},
		{"bare dotdot", "..", "", true},
		{"dot only", ".", "", true},
		{"backslash", "a\\b.txt", "", true},
		{"windows traversal", "..\\evil.exe", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SafeJoin(dir, tc.rel)
			if (err != nil) != tc.wantErr {
				t.Fatalf("SafeJoin(%q) error = %v, wantErr %v", tc.rel, err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if filepath.ToSlash(got) != tc.want {
				t.Errorf("SafeJoin(%q) = %q, want %q", tc.rel, filepath.ToSlash(got), tc.want)
			}
		})
	}
}

func TestReadSkillMD(t *testing.T) {
	root := t.TempDir()

	if _, err := ReadSkillMD(root); err == nil {
		t.Error("ReadSkillMD on folder without SKILL.md: want error, got nil")
	}

	content := "---\nname: demo\n---\n\n# Demo é\n"
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	md, err := ReadSkillMD(root)
	if err != nil {
		t.Fatalf("ReadSkillMD: %v", err)
	}
	// Byte-preserving read.
	if string(md) != content {
		t.Errorf("ReadSkillMD = %q, want %q", md, content)
	}
}
