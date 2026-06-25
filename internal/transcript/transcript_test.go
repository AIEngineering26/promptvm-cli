package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadExtractsTextFilesAndCommands(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "session.jsonl")
	body := `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"please refactor"}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"on it"},{"type":"tool_use","name":"Edit","input":{"file_path":"/repo/main.go"}}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}
not json at all
{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{"file_path":"/repo/util.go"}}]}}
`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	parsed, err := Read(p)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if parsed.MessageCount != 4 {
		t.Errorf("MessageCount = %d, want 4 (bad line skipped)", parsed.MessageCount)
	}
	if len(parsed.FilesTouched) != 2 {
		t.Errorf("FilesTouched = %v, want 2", parsed.FilesTouched)
	}
	if len(parsed.Commands) != 1 || parsed.Commands[0] != "go test ./..." {
		t.Errorf("Commands = %v", parsed.Commands)
	}
	if parsed.Text == "" {
		t.Errorf("Text should not be empty")
	}
}

func TestReadMissingFile(t *testing.T) {
	_, err := Read(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadStringContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(p, []byte(`{"type":"user","message":{"role":"user","content":"hello world"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	parsed, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Text == "" {
		t.Errorf("expected text from string content")
	}
}
