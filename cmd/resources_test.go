package cmd

import "testing"

func TestSanitizeFilename(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"report.pdf", "report.pdf"},
		{"sub/folder/file.txt", "file.txt"},
		{"..\\..\\evil.exe", "evil.exe"},
		{"/etc/passwd", "passwd"},
		{"..", "resource"},
		{".", "resource"},
		{"", "resource"},
		{"/", "resource"},
	}
	for _, tc := range cases {
		got := sanitizeFilename(tc.in)
		if got != tc.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResHumanBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}
	for _, tc := range cases {
		got := resHumanBytes(tc.in)
		if got != tc.want {
			t.Errorf("resHumanBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate short = %q", got)
	}
	if got := truncate("this is a long string", 10); got != "this is..." {
		t.Errorf("truncate long = %q", got)
	}
}

func TestResolveContentType(t *testing.T) {
	cases := []struct {
		name     string
		file     string
		override string
		want     string
	}{
		{"markdown fallback", "doc.md", "", "text/markdown"},
		{"markdown long ext", "doc.markdown", "", "text/markdown"},
		{"case-insensitive ext", "README.MD", "", "text/markdown"},
		{"override wins over inference", "doc.md", "application/pdf", "application/pdf"},
		{"strips content-type parameters", "notes.md", "text/markdown; charset=utf-8", "text/markdown"},
		{"unknown ext falls back to octet-stream", "blob.zzz", "", "application/octet-stream"},
	}
	for _, tc := range cases {
		if got := resolveContentType(tc.file, tc.override); got != tc.want {
			t.Errorf("%s: resolveContentType(%q, %q) = %q, want %q", tc.name, tc.file, tc.override, got, tc.want)
		}
	}
}
