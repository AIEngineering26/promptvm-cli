package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AIEngineering26/promptvm-cli/internal/capture"
	"github.com/AIEngineering26/promptvm-cli/internal/manifest"
	"github.com/AIEngineering26/promptvm-cli/internal/redact"
	"github.com/AIEngineering26/promptvm-cli/internal/sanitize"
	"github.com/AIEngineering26/promptvm-cli/internal/spool"
)

// TestSanitizeRunsStrictlyBeforeRedaction is the FR-Q3 ordering contract: a
// secret hidden inside an ANSI escape is invisible to the provider redactor until
// sanitization removes the escape. So redact-alone must LEAK it while
// sanitize→redact must CATCH it — proving sanitize runs first.
func TestSanitizeRunsStrictlyBeforeRedaction(t *testing.T) {
	// A GitHub token with an ANSI reset spliced in right after the `ghp_` prefix.
	// The provider pattern needs `ghp_` immediately followed by [A-Za-z0-9]{20,};
	// the ESC byte breaks that, and the digit run is too low-entropy for layer 2.
	secret := "012345678901234567890123456789012345"
	raw := "my token is ghp_\x1b[0m" + secret + " ok"

	// redact alone (no sanitize) cannot see the token → the secret digits leak.
	leak := redact.Redact(raw, nil)
	if !strings.Contains(leak.Text, secret) {
		t.Fatalf("precondition failed: redact-alone unexpectedly redacted the secret: %q", leak.Text)
	}

	// sanitize → redact removes the ANSI, exposing the `ghp_<secret>` shape to the
	// provider redactor — proving sanitize must run first.
	clean := redact.Redact(sanitize.Sanitize(raw), nil)
	if strings.Contains(clean.Text, secret) {
		t.Errorf("secret survived sanitize→redact: %q", clean.Text)
	}
	if strings.Contains(clean.Text, "\x1b") {
		t.Errorf("ANSI survived: %q", clean.Text)
	}
}

// writeRawTranscript writes a JSONL transcript with the given lines.
func writeRawTranscript(t *testing.T, root, session string, lines ...string) string {
	t.Helper()
	p := filepath.Join(root, session+".jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func userLine(text string) string {
	return fmt.Sprintf(`{"type":"user","message":{"role":"user","content":[{"type":"text","text":%q}]}}`, text)
}

func assistantLine(text string) string {
	return fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":%q}]}}`, text)
}

// TestBuildRequestDerivesSessionIdentity covers FR-1/FR-5/FR-7/FR-Q5: a real
// title + task + description, a normalized project key, and a populated
// files_touched extracted from tool_use entries (CAPQ-7 regression).
func TestBuildRequestDerivesSessionIdentity(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	repoRoot := t.TempDir()
	writeManifestFile(t, repoRoot, `{ "workspace": "ws_id", "capture": { "enabled": true, "events": ["SessionEnd"], "mode": "summary" } }`)

	editPath := filepath.Join(repoRoot, "auth.go")
	tr := writeRawTranscript(t, repoRoot, "sess-id",
		// A leading housekeeping turn must be skipped in favor of the real one.
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"<command-name>/model</command-name>"}]}}`,
		userLine("Refactor the auth module to use JWT tokens. It should validate signatures."),
		fmt.Sprintf(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Edit","input":{"file_path":%q}},{"type":"tool_use","name":"Bash","input":{"command":"go test ./..."}}]}}`, editPath),
	)

	resolved, _ := manifest.Resolve(repoRoot)
	in := HookInput{SessionID: "sess-id", TranscriptPath: tr, Cwd: repoRoot, HookEventName: "SessionEnd", Reason: "other"}
	req := buildRequest(in, repoRoot, "ws_id", capture.ModeSummary, resolved)

	if req.Metadata.Title == "" {
		t.Error("title must never be blank")
	}
	if strings.HasPrefix(req.Metadata.Title, "/") || strings.HasPrefix(req.Metadata.Title, "<") {
		t.Errorf("title is a slash-command/wrapper: %q", req.Metadata.Title)
	}
	if !strings.HasPrefix(req.Metadata.TaskAtHand, "Refactor the auth module") {
		t.Errorf("taskAtHand = %q, want the first real user prompt", req.Metadata.TaskAtHand)
	}
	if req.Metadata.Description == "" {
		t.Error("description should be derived from the first real prompt")
	}
	// No git remote in a temp dir → projectKey falls back to the repo-root basename.
	if req.Metadata.ProjectKey != filepath.Base(repoRoot) {
		t.Errorf("projectKey = %q, want %q", req.Metadata.ProjectKey, filepath.Base(repoRoot))
	}
	if len(req.Metadata.FilesTouched) != 1 || req.Metadata.FilesTouched[0] != "auth.go" {
		t.Errorf("filesTouched = %v, want [auth.go] (repo-relative, CAPQ-7 regression)", req.Metadata.FilesTouched)
	}
	if len(req.Metadata.Commands) != 1 {
		t.Errorf("commands = %v, want 1", req.Metadata.Commands)
	}
	if req.LowSignal {
		t.Error("a session with a real prompt + tool work must not be low-signal")
	}
}

// TestBuildRequestLowSignalNoUserTurn covers FR-Q4: a session with no real user
// turn and no tool work is flagged low-signal (but not necessarily dropped).
func TestBuildRequestLowSignalNoUserTurn(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	repoRoot := t.TempDir()
	writeManifestFile(t, repoRoot, `{ "workspace": "ws_ls", "capture": { "enabled": true, "events": ["SessionEnd"], "mode": "summary" } }`)

	tr := writeRawTranscript(t, repoRoot, "sess-ls", assistantLine("Here is a long standalone analysis with no user turn and no tools."))
	resolved, _ := manifest.Resolve(repoRoot)
	in := HookInput{SessionID: "sess-ls", TranscriptPath: tr, Cwd: repoRoot, HookEventName: "SessionEnd"}
	req := buildRequest(in, repoRoot, "ws_ls", capture.ModeSummary, resolved)

	if !req.LowSignal {
		t.Error("no real user turn + no tool work must be low-signal")
	}
	if req.Metadata.Title == "" {
		t.Error("title must never be blank even for low-signal")
	}
}

// TestProcessHookDropsHousekeeping covers FR-Q4's drop path: a pure /exit
// housekeeping session is suppressed entirely (no spool, no upload).
func TestProcessHookDropsHousekeeping(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	repoRoot := t.TempDir()
	writeManifestFile(t, repoRoot, `{ "workspace": "ws_hk", "capture": { "enabled": true, "events": ["SessionEnd"], "mode": "summary" } }`)

	tr := writeRawTranscript(t, repoRoot, "sess-hk",
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"<command-name>/exit</command-name>"}]}}`,
		assistantLine("See ya!"),
	)
	in := HookInput{SessionID: "sess-hk", TranscriptPath: tr, Cwd: repoRoot, HookEventName: "SessionEnd"}
	cmd := newSyncRunCmd()
	cmd.SetContext(context.Background())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	processHook(cmd, in, "", "", false)

	if n, _ := spool.Count(); n != 0 {
		t.Errorf("housekeeping session should be dropped, not spooled (%d entries)", n)
	}
}

func TestIsHousekeepingOnly(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"", true},
		{"/exit", true},
		{"/exit\nSee ya!", true},
		{"/model\n/clear", true},
		{"Bye!", true},
		{"Refactor the auth module", false},
		{"/exit\nactually first fix the bug", false},
		{strings.Repeat("word ", 40), false}, // too long
	}
	for _, c := range cases {
		if got := isHousekeepingOnly(c.text); got != c.want {
			t.Errorf("isHousekeepingOnly(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}
