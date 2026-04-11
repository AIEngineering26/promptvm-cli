package ioutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func newCmdWithFlags(content, file string) *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("content", content, "")
	cmd.Flags().String("file", file, "")
	return cmd
}

func TestReadContent_FromFlag(t *testing.T) {
	cmd := newCmdWithFlags("hello world", "")
	got, err := ReadContent(cmd)
	if err != nil {
		t.Fatalf("ReadContent error: %v", err)
	}
	if got != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

func TestReadContent_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.txt")
	if err := os.WriteFile(path, []byte("file body"), 0600); err != nil {
		t.Fatalf("write tmp: %v", err)
	}

	cmd := newCmdWithFlags("", path)
	got, err := ReadContent(cmd)
	if err != nil {
		t.Fatalf("ReadContent error: %v", err)
	}
	if got != "file body" {
		t.Errorf("got %q, want %q", got, "file body")
	}
}

func TestReadContent_FileNotFound(t *testing.T) {
	cmd := newCmdWithFlags("", "/nonexistent/definitely-not-here.txt")
	if _, err := ReadContent(cmd); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestReadContent_Required(t *testing.T) {
	cmd := newCmdWithFlags("", "")
	if _, err := ReadContent(cmd); err == nil {
		t.Error("expected error when neither content nor file provided")
	}
}

func TestReadContent_FlagTakesPrecedence(t *testing.T) {
	// Both flags provided — --content should win.
	cmd := newCmdWithFlags("inline", "/should/not/be/read")
	got, err := ReadContent(cmd)
	if err != nil {
		t.Fatalf("ReadContent error: %v", err)
	}
	if got != "inline" {
		t.Errorf("got %q, want %q", got, "inline")
	}
}
