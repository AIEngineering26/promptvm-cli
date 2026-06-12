package cmd

import (
	"io"
	"strings"
	"testing"
)

// TestPromptsCreateContentKindHints ensures that passing a content kind
// (skill/hook) to `prompts create --kind` fails fast with a pointer to the
// right command family instead of an opaque SDK enum error.
func TestPromptsCreateContentKindHints(t *testing.T) {
	cases := []struct {
		kind     string
		wantHint string
	}{
		{"skill", "promptvm skills upload"},
		{"Skill", "promptvm skills upload"},
		{"hook", "promptvm hooks"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			cmd := newPromptsCreateCmd()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{
				"--name", "x", "--workspace", "ws_1",
				"--kind", tc.kind, "--content", "hi",
			})
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("--kind %s: expected error, got nil", tc.kind)
			}
			if !strings.Contains(err.Error(), tc.wantHint) {
				t.Errorf("--kind %s error %q does not mention %q", tc.kind, err.Error(), tc.wantHint)
			}
		})
	}
}
