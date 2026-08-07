package export

import (
	"testing"
)

func sampleSkillBundle() Bundle {
	return Bundle{
		Name:        "pdf-toolkit",
		Description: "Work with PDF files",
		Body:        "Use this skill to split and merge PDFs.\n",
		Files: []BundleFile{
			{Path: "ref/notes.md", DownloadURL: "https://example.test/dl/notes", SizeBytes: 12},
		},
	}
}

func TestLookupUnknown(t *testing.T) {
	if _, err := Lookup("emacs"); err == nil {
		t.Fatal("expected error for unknown target")
	}
	for _, name := range []string{"cursor", "codex", "windsurf"} {
		if _, err := Lookup(name); err != nil {
			t.Errorf("Lookup(%q) unexpected error: %v", name, err)
		}
	}
}

func TestNames(t *testing.T) {
	got := Names()
	want := []string{"codex", "cursor", "windsurf"}
	if len(got) != len(want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Names()[%d] = %q, want %q (sorted)", i, got[i], want[i])
		}
	}
}

func TestCursorTransformGolden(t *testing.T) {
	writes, err := (cursorTarget{}).Transform(sampleSkillBundle())
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 2 {
		t.Fatalf("expected 2 writes (rule + bundled file), got %d", len(writes))
	}
	if writes[0].RelPath != ".cursor/rules/pdf-toolkit.mdc" {
		t.Errorf("path = %q", writes[0].RelPath)
	}
	wantMDC := "---\n" +
		"description: Work with PDF files\n" +
		"globs: \n" +
		"alwaysApply: false\n" +
		"---\n" +
		"Use this skill to split and merge PDFs.\n"
	if got := string(writes[0].Content); got != wantMDC {
		t.Errorf("cursor MDC golden mismatch:\n--- got ---\n%q\n--- want ---\n%q", got, wantMDC)
	}
	// Bundled file goes under the per-rule subdirectory, fetched via download.
	if writes[1].RelPath != ".cursor/rules/pdf-toolkit/ref/notes.md" {
		t.Errorf("bundled file path = %q", writes[1].RelPath)
	}
	if writes[1].DownloadURL != "https://example.test/dl/notes" {
		t.Errorf("bundled file download url = %q", writes[1].DownloadURL)
	}
}

func TestWindsurfTransformGolden(t *testing.T) {
	writes, err := (windsurfTarget{}).Transform(sampleSkillBundle())
	if err != nil {
		t.Fatal(err)
	}
	if writes[0].RelPath != ".windsurf/rules/pdf-toolkit.md" {
		t.Errorf("path = %q", writes[0].RelPath)
	}
	wantMD := "---\n" +
		"trigger: model_decision\n" +
		"description: Work with PDF files\n" +
		"---\n" +
		"Use this skill to split and merge PDFs.\n"
	if got := string(writes[0].Content); got != wantMD {
		t.Errorf("windsurf golden mismatch:\n--- got ---\n%q\n--- want ---\n%q", got, wantMD)
	}
	if len(writes) != 2 || writes[1].RelPath != ".windsurf/rules/pdf-toolkit/ref/notes.md" {
		t.Errorf("bundled file write missing/wrong: %+v", writes)
	}
}

func TestWindsurfNoDescriptionOmitsFrontmatter(t *testing.T) {
	b := Bundle{Name: "just-text", Body: "hello\n"}
	writes, err := (windsurfTarget{}).Transform(b)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(writes[0].Content); got != "hello\n" {
		t.Errorf("no-description windsurf should be plain body, got %q", got)
	}
}

func TestCodexTransformGolden(t *testing.T) {
	writes, err := (codexTarget{}).Transform(sampleSkillBundle())
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 1 {
		t.Fatalf("codex should emit exactly 1 write (AGENTS.md, no bundled files), got %d", len(writes))
	}
	w := writes[0]
	if w.RelPath != "AGENTS.md" {
		t.Errorf("path = %q, want AGENTS.md", w.RelPath)
	}
	if !w.Append {
		t.Error("codex write should be Append (append-or-create)")
	}
	want := "\n## pdf-toolkit\n\nUse this skill to split and merge PDFs.\n"
	if got := string(w.Content); got != want {
		t.Errorf("codex golden mismatch:\n--- got ---\n%q\n--- want ---\n%q", got, want)
	}
}

func TestPromptTransformNoFrontmatterNoFiles(t *testing.T) {
	b := Bundle{Name: "my-prompt", Body: "Rewrite the following as a haiku."}
	// Cursor: prompt has no description → empty description line, body as-is.
	writes, err := (cursorTarget{}).Transform(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 1 {
		t.Fatalf("prompt export should have no bundled files, got %d writes", len(writes))
	}
	want := "---\n" +
		"description: \n" +
		"globs: \n" +
		"alwaysApply: false\n" +
		"---\n" +
		"Rewrite the following as a haiku.\n"
	if got := string(writes[0].Content); got != want {
		t.Errorf("prompt cursor golden mismatch:\n--- got ---\n%q\n--- want ---\n%q", got, want)
	}
}

func TestStripFrontmatter(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "strips block and leading blank lines",
			in:   "---\nname: x\ndescription: d\n---\n\nBody line one\nBody line two\n",
			want: "Body line one\nBody line two\n",
		},
		{
			name: "no frontmatter passes through",
			in:   "Just a prompt body.\n",
			want: "Just a prompt body.\n",
		},
		{
			name: "unclosed frontmatter passes through",
			in:   "---\nname: x\nno close here\n",
			want: "---\nname: x\nno close here\n",
		},
		{
			name: "crlf opener",
			in:   "---\r\nname: x\r\n---\r\nbody\r\n",
			want: "body\r\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StripFrontmatter(tc.in); got != tc.want {
				t.Errorf("StripFrontmatter mismatch:\n--- got ---\n%q\n--- want ---\n%q", got, tc.want)
			}
		})
	}
}

func TestPathHelpers(t *testing.T) {
	if p := (cursorTarget{}).Path("x"); p != ".cursor/rules/x.mdc" {
		t.Errorf("cursor path = %q", p)
	}
	if p := (windsurfTarget{}).Path("x"); p != ".windsurf/rules/x.md" {
		t.Errorf("windsurf path = %q", p)
	}
	if p := (codexTarget{}).Path("x"); p != "AGENTS.md" {
		t.Errorf("codex path = %q", p)
	}
}
