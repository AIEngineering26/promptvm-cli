package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseVariables_Flags(t *testing.T) {
	got, err := parseVariables([]string{"name=Ada", "lang=Go"}, "")
	if err != nil {
		t.Fatalf("parseVariables: %v", err)
	}
	if got["name"] != "Ada" || got["lang"] != "Go" {
		t.Errorf("parseVariables = %v", got)
	}
}

func TestParseVariables_InvalidFlag(t *testing.T) {
	if _, err := parseVariables([]string{"no-equals"}, ""); err == nil {
		t.Error("expected error for malformed --var")
	}
}

func TestParseVariables_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vars.json")
	if err := os.WriteFile(path, []byte(`{"greeting":"hi","who":"world"}`), 0600); err != nil {
		t.Fatal(err)
	}

	got, err := parseVariables(nil, path)
	if err != nil {
		t.Fatalf("parseVariables: %v", err)
	}
	if got["greeting"] != "hi" || got["who"] != "world" {
		t.Errorf("parseVariables(file) = %v", got)
	}
}

func TestParseVariables_FlagOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vars.json")
	if err := os.WriteFile(path, []byte(`{"name":"fromfile"}`), 0600); err != nil {
		t.Fatal(err)
	}

	got, err := parseVariables([]string{"name=fromflag"}, path)
	if err != nil {
		t.Fatalf("parseVariables: %v", err)
	}
	if got["name"] != "fromflag" {
		t.Errorf("flag should override file: %v", got)
	}
}

func TestParseVariables_FileBadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vars.json")
	if err := os.WriteFile(path, []byte(`not json`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := parseVariables(nil, path); err == nil {
		t.Error("expected parse error for invalid JSON")
	}
}

func TestApplyVariables(t *testing.T) {
	vars := map[string]string{"name": "Ada", "lang": "Go"}
	cases := []struct {
		in, want string
	}{
		{"Hello {{name}}!", "Hello Ada!"},
		{"Hello {{ name }}, write {{lang}}", "Hello Ada, write Go"},
		{"{{unknown}} stays", "{{unknown}} stays"},
		{"no tokens", "no tokens"},
	}
	for _, tc := range cases {
		got := applyVariables(tc.in, vars)
		if got != tc.want {
			t.Errorf("applyVariables(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestApplyVariables_Empty(t *testing.T) {
	got := applyVariables("hello {{name}}", nil)
	if got != "hello {{name}}" {
		t.Errorf("applyVariables with nil vars modified content: %q", got)
	}
}
