package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadExtractsTextFilesAndCommands(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "session.jsonl")
	// Edit + Write + MultiEdit are touches; Read is NOT (CAPQ-7).
	body := `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"please refactor"}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"on it"},{"type":"tool_use","name":"Edit","input":{"file_path":"/repo/main.go"}}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Write","input":{"file_path":"/repo/new.go"}}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"MultiEdit","input":{"file_path":"/repo/multi.go"}}]}}
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
	if parsed.MessageCount != 6 {
		t.Errorf("MessageCount = %d, want 6 (bad line skipped)", parsed.MessageCount)
	}
	if len(parsed.FilesTouched) != 3 {
		t.Errorf("FilesTouched = %v, want 3 (Edit+Write+MultiEdit; Read excluded)", parsed.FilesTouched)
	}
	for _, f := range parsed.FilesTouched {
		if f == "/repo/util.go" {
			t.Errorf("Read target leaked into FilesTouched: %v", parsed.FilesTouched)
		}
	}
	if len(parsed.Commands) != 1 || parsed.Commands[0] != "go test ./..." {
		t.Errorf("Commands = %v", parsed.Commands)
	}
	if parsed.Text == "" {
		t.Errorf("Text should not be empty")
	}
	if len(parsed.UserTexts) != 1 || parsed.UserTexts[0] != "please refactor" {
		t.Errorf("UserTexts = %v, want [please refactor]", parsed.UserTexts)
	}
}

func TestReadToolUseResultFilesAndUserTurnFilters(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.jsonl")
	body := `{"type":"user","isMeta":true,"message":{"role":"user","content":[{"type":"text","text":"meta noise"}]}}
{"type":"user","isSidechain":true,"message":{"role":"user","content":[{"type":"text","text":"sidechain noise"}]}}
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"real ask"}]}}
{"type":"assistant","toolUseResult":{"filePath":"/repo/edited.go","structuredPatch":[{"oldStart":1}]},"message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}
{"type":"assistant","toolUseResult":{"filePath":"/repo/justread.go"},"message":{"role":"assistant","content":[{"type":"text","text":"read"}]}}
`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	parsed, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.UserTexts) != 1 || parsed.UserTexts[0] != "real ask" {
		t.Errorf("UserTexts = %v, want only the non-meta/non-sidechain turn", parsed.UserTexts)
	}
	if len(parsed.FilesTouched) != 1 || parsed.FilesTouched[0] != "/repo/edited.go" {
		t.Errorf("FilesTouched = %v, want only the patched toolUseResult file", parsed.FilesTouched)
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
