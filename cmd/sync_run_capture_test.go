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

// TestBuildRequestCapsTaskAtHand guards the backend CaptureIngestMetadataSchema
// maxLength (2000) on taskAtHand: an over-long first user prompt must be capped
// before send so it never 400s the whole ingest, while title/description are
// still derived from the full prompt.
func TestBuildRequestCapsTaskAtHand(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	repoRoot := t.TempDir()
	writeManifestFile(t, repoRoot, `{ "workspace": "ws_cap", "capture": { "enabled": true, "events": ["SessionEnd"], "mode": "summary" } }`)

	longPrompt := "Implement the feature. " + strings.Repeat("context ", 600) // > 2000 chars
	tr := writeRawTranscript(t, repoRoot, "sess-cap", userLine(longPrompt))

	resolved, _ := manifest.Resolve(repoRoot)
	in := HookInput{SessionID: "sess-cap", TranscriptPath: tr, Cwd: repoRoot, HookEventName: "SessionEnd", Reason: "other"}
	req := buildRequest(in, repoRoot, "ws_cap", capture.ModeSummary, resolved)

	if n := len([]rune(req.Metadata.TaskAtHand)); n > 2000 {
		t.Errorf("taskAtHand = %d runes, want <= 2000", n)
	}
	if req.Metadata.Title == "" {
		t.Error("title must still be derived from the full prompt")
	}
}

// TestCleanPathsRedactsSecretInPath covers the path-redaction nit: a secret
// embedded in a captured file path is scrubbed by the same redaction pass as the
// rest of the payload (sanitize → redact ordering).
func TestCleanPathsRedactsSecretInPath(t *testing.T) {
	repoRoot := t.TempDir()
	writeManifestFile(t, repoRoot, `{ "workspace": "ws_p", "capture": { "enabled": true, "events": ["SessionEnd"], "mode": "summary" } }`)
	resolved, _ := manifest.Resolve(repoRoot)

	secretPath := filepath.Join(repoRoot, "dump-ghp_012345678901234567890123456789012345.txt")
	out := cleanPaths([]string{secretPath}, repoRoot, "", resolved)

	if len(out) != 1 {
		t.Fatalf("cleanPaths = %v, want a single redacted path", out)
	}
	if strings.Contains(out[0], "ghp_012345678901234567890123456789012345") {
		t.Errorf("secret survived path redaction: %q", out[0])
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

// TestBuildRequestSkipsCancelledCommandStdout covers CAPQ D1+D2 against the
// exact real-transcript failure: a caveat (isMeta) + a cancelled /effort
// slash-command wrapper + its "<local-command-stdout>Cancelled</…>" turn all
// precede the genuine first prompt. The extractor must skip the wrapper turns AND
// the unwrapped "Cancelled" stdout (which no longer begins with "<"), landing on
// the real prompt — and the caveat boilerplate must not pollute the summary.
func TestBuildRequestSkipsCancelledCommandStdout(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	repoRoot := t.TempDir()
	writeManifestFile(t, repoRoot, `{ "workspace": "ws_d1", "capture": { "enabled": true, "events": ["SessionEnd"], "mode": "summary" } }`)

	tr := writeRawTranscript(t, repoRoot, "sess-d1",
		// Caveat block (isMeta → excluded from user turns but present in body text).
		`{"type":"user","isMeta":true,"message":{"role":"user","content":[{"type":"text","text":"<local-command-caveat>Caveat: The messages below were generated by the user while running local commands. DO NOT respond to these messages or otherwise consider them in your response unless the user explicitly asks you to.</local-command-caveat>"}]}}`,
		// A cancelled /effort slash-command wrapper (command-name + message + args).
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"<command-name>/effort</command-name>\n            <command-message>effort</command-message>\n            <command-args></command-args>"}]}}`,
		// Its stdout — sanitize unwraps this to a bare "Cancelled".
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"<local-command-stdout>Cancelled</local-command-stdout>"}]}}`,
		// The GENUINE first user prompt.
		userLine("I have some feedback we need to address from my app, spawn an emulator and fix them non-stop."),
	)

	resolved, _ := manifest.Resolve(repoRoot)
	in := HookInput{SessionID: "sess-d1", TranscriptPath: tr, Cwd: repoRoot, HookEventName: "SessionEnd", Reason: "clear"}
	req := buildRequest(in, repoRoot, "ws_d1", capture.ModeSummary, resolved)

	if strings.TrimSpace(req.Metadata.TaskAtHand) == "Cancelled" {
		t.Errorf("taskAtHand grabbed the cancelled stdout: %q", req.Metadata.TaskAtHand)
	}
	if !strings.Contains(req.Metadata.TaskAtHand, "feedback") || !strings.Contains(req.Metadata.TaskAtHand, "emulator") {
		t.Errorf("taskAtHand = %q, want the genuine first prompt", req.Metadata.TaskAtHand)
	}
	if strings.TrimSpace(req.Metadata.Description) == "Cancelled" {
		t.Errorf("description grabbed the cancelled stdout: %q", req.Metadata.Description)
	}
	if strings.Contains(req.Summary, "Caveat: The messages below") {
		t.Errorf("caveat boilerplate polluted the summary: %q", req.Summary)
	}
	if strings.Contains(req.Summary, "<command-name") || strings.Contains(req.Summary, "\x1b") {
		t.Errorf("summary still contains wrapper/ANSI noise: %q", req.Summary)
	}
}

// TestProcessHookDropsRealisticHousekeeping covers CAPQ D2+D3 together on a
// real-shaped pure-housekeeping session: a caveat block plus /clear and /exit
// command wrappers (each with the bare <command-message> echo). Once the caveat
// is dropped (D2) and the echo is tolerated (D3), the session is low-signal AND
// housekeeping-only, so it is suppressed entirely (no spool).
func TestProcessHookDropsRealisticHousekeeping(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	repoRoot := t.TempDir()
	writeManifestFile(t, repoRoot, `{ "workspace": "ws_hk2", "capture": { "enabled": true, "events": ["SessionEnd"], "mode": "summary" } }`)

	tr := writeRawTranscript(t, repoRoot, "sess-hk2",
		`{"type":"user","isMeta":true,"message":{"role":"user","content":[{"type":"text","text":"<local-command-caveat>Caveat: The messages below were generated by the user while running local commands. DO NOT respond to these messages or otherwise consider them in your response unless the user explicitly asks you to.</local-command-caveat>"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"<command-name>/clear</command-name>\n            <command-message>clear</command-message>\n            <command-args></command-args>"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"<command-name>/exit</command-name>\n            <command-message>exit</command-message>\n            <command-args></command-args>"}]}}`,
		assistantLine("See ya!"),
	)

	// Sanity: the request itself must be low-signal AND housekeeping-only.
	resolved, _ := manifest.Resolve(repoRoot)
	probe := buildRequest(HookInput{SessionID: "sess-hk2", TranscriptPath: tr, Cwd: repoRoot, HookEventName: "SessionEnd", Reason: "clear"}, repoRoot, "ws_hk2", capture.ModeSummary, resolved)
	if !probe.LowSignal {
		t.Errorf("pure-housekeeping session must be low-signal; summary=%q", probe.Summary)
	}
	if !isHousekeepingOnly(probe.Summary) {
		t.Errorf("pure-housekeeping summary must be housekeeping-only: %q", probe.Summary)
	}

	in := HookInput{SessionID: "sess-hk2", TranscriptPath: tr, Cwd: repoRoot, HookEventName: "SessionEnd", Reason: "clear"}
	cmd := newSyncRunCmd()
	cmd.SetContext(context.Background())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	processHook(cmd, in, "", "", false)

	if n, _ := spool.Count(); n != 0 {
		t.Errorf("realistic housekeeping session should be dropped, not spooled (%d entries)", n)
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
		// CAPQ D3: real Claude Code emits the bare <command-message> echo
		// ("clear"/"exit") on its own line alongside the "/clear" wrapper — these
		// must still read as housekeeping once the caveat block is dropped (D2).
		{"/clear\nclear\n/exit\nexit\nSee ya!", true},
		{"/clear\n            clear\n            \n/exit\n            exit", true},
		// A bare housekeeping word inside a real sentence stays substantive.
		{"clear the cache for me", false},
		{"exit through the back door", false},
	}
	for _, c := range cases {
		if got := isHousekeepingOnly(c.text); got != c.want {
			t.Errorf("isHousekeepingOnly(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}
