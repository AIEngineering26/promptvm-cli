package sanitize

import (
	"strings"
	"testing"
)

func TestStripsANSI(t *testing.T) {
	in := "\x1b[1mbold\x1b[22m and \x1b[0Kclear"
	got := Sanitize(in)
	if strings.Contains(got, "\x1b") {
		t.Fatalf("ESC byte survived: %q", got)
	}
	if got != "bold and clear" {
		t.Errorf("got %q, want %q", got, "bold and clear")
	}
}

func TestStripsC0ControlsKeepsTabNewline(t *testing.T) {
	in := "a\x00b\x07c\r\nd\te"
	got := Sanitize(in)
	want := "abc\nd\te"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUnwrapsClaudeCodeWrappers(t *testing.T) {
	cases := []struct{ in, want string }{
		{"<command-name>/model</command-name>", "/model"},
		{"<local-command-stdout>Kept model as Default</local-command-stdout>", "Kept model as Default"},
		{"<task-notification>done</task-notification>", "done"},
		{"<COMMAND-MESSAGE>hi</COMMAND-MESSAGE>", "hi"}, // case-insensitive
		{"<command-args></command-args>", ""},
		{"<local-command-caveat>Caveat: x</local-command-caveat>", "Caveat: x"},
	}
	for _, c := range cases {
		if got := Sanitize(c.in); got != c.want {
			t.Errorf("Sanitize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestUnescapesLiteralNewlinesTabs(t *testing.T) {
	got := Sanitize(`foo\nbar\tbaz`)
	want := "foo\nbar\tbaz"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSecretBuriedInANSIIsExposed(t *testing.T) {
	// A token split by an ANSI reset right after the prefix would be invisible to
	// a provider regex until sanitization removes the escape. We only assert here
	// that the ANSI is gone and the token is contiguous; redaction ordering is
	// covered in the cmd package where both packages run together.
	in := "ghp_\x1b[0mABCDEFGHIJKLMNOPQRSTUVWXYZ012345"
	got := Sanitize(in)
	if strings.Contains(got, "\x1b") {
		t.Fatalf("ESC survived: %q", got)
	}
	if !strings.HasPrefix(got, "ghp_ABCDEFGHIJ") {
		t.Errorf("token not contiguous after sanitize: %q", got)
	}
}

func TestIdempotent(t *testing.T) {
	in := "<command-name>/exit\x1b[0m</command-name>"
	once := Sanitize(in)
	twice := Sanitize(once)
	if once != twice {
		t.Errorf("not idempotent: %q != %q", once, twice)
	}
}

func TestEmptyAndStrings(t *testing.T) {
	if Sanitize("") != "" {
		t.Error("empty should stay empty")
	}
	if Strings(nil) != nil {
		t.Error("nil slice should stay nil")
	}
	got := Strings([]string{"\x1b[1ma", "<command-name>b</command-name>"})
	if got[0] != "a" || got[1] != "b" {
		t.Errorf("Strings = %v", got)
	}
}
