package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AIEngineering26/promptvm-cli/internal/output"
)

// TestPromptsRollbackRegistered verifies the rollback subcommand is wired
// onto `prompts`.
func TestPromptsRollbackRegistered(t *testing.T) {
	got := false
	for _, c := range promptsCmd.Commands() {
		if c.Name() == "rollback" {
			got = true
			break
		}
	}
	if !got {
		t.Fatal("prompts subcommand 'rollback' missing")
	}
}

// TestPromptsRollbackFlags covers US-003: --to, --yes, --idempotency-key.
func TestPromptsRollbackFlags(t *testing.T) {
	cmd := newPromptsRollbackCmd()
	for _, name := range []string{"to", "yes", "idempotency-key"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("rollback command missing flag --%s", name)
		}
	}
	// --to must be marked required so cobra rejects calls without it.
	if cmd.Flag("to") == nil || cmd.Flag("to").Annotations == nil {
		t.Error("--to flag should be marked required")
	}
}

// canned upstream payload shaped like RollbackPromptResponse.
const fakeRollbackBody = `{
  "data": {
    "id": "ver_new",
    "promptId": "pmt_abc",
    "versionNumber": 4,
    "versionLabel": null,
    "content": "rolled-back content",
    "systemPrompt": null,
    "changeNote": "rollback to v1",
    "isCurrentVersion": true,
    "isPublished": false,
    "variablesSchema": {},
    "createdById": null,
    "createdByName": null,
    "createdAt": "2026-05-09T12:00:00Z",
    "isDeployedToDevelopment": false,
    "isDeployedToProduction": false
  }
}`

// startRollbackServer captures requests so tests can assert idempotency
// and method shape, and returns the canned body. Pass status=0 to ignore
// status overrides (defaults 200).
func startRollbackServer(t *testing.T, status int, body string) (*httptest.Server, *capturedReq) {
	t.Helper()
	captured := &capturedReq{}
	if status == 0 {
		status = 200
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/rollback") {
			http.NotFound(w, r)
			return
		}
		captured.method = r.Method
		captured.path = r.URL.Path
		captured.idempotencyKey = r.Header.Get("idempotency-key")
		buf, _ := io.ReadAll(r.Body)
		captured.body = string(buf)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, captured
}

type capturedReq struct {
	method         string
	path           string
	idempotencyKey string
	body           string
}

// runRollbackCmd is the shared scaffold for invoking the cobra command
// with consistent flag wiring.
func runRollbackCmd(t *testing.T, srvURL, format string, args []string) (string, error) {
	t.Helper()
	withTestEnv(t, srvURL)

	cmd := newPromptsRollbackCmd()
	cmd.SetContext(context.Background())
	cmd.Flags().StringP("output", "o", format, "Output format")
	cmd.Flags().Bool("compact", false, "compact json")
	cmd.Flags().Bool("no-header", false, "no header")
	cmd.Flags().Bool("wide", false, "wide")

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.ParseFlags(args); err != nil {
		return out.String(), err
	}
	if err := cmd.Args(cmd, cmd.Flags().Args()); err != nil {
		return out.String(), err
	}
	if err := cmd.RunE(cmd, cmd.Flags().Args()); err != nil {
		return out.String(), err
	}
	return out.String(), nil
}

// TestRollback_HappyPath_JSON exercises the JSON path. Covers US-003 +
// US-006 (json parses verbatim).
func TestRollback_HappyPath_JSON(t *testing.T) {
	srv, captured := startRollbackServer(t, 200, fakeRollbackBody)

	out, err := runRollbackCmd(t, srv.URL, "json",
		[]string{"--to", "1", "--yes", "pmt_abc"})
	if err != nil {
		t.Fatalf("RunE: %v\n%s", err, out)
	}

	if captured.method != http.MethodPost {
		t.Errorf("expected POST, got %s", captured.method)
	}
	if !strings.Contains(captured.path, "/prompts/pmt_abc/rollback") {
		t.Errorf("unexpected path: %s", captured.path)
	}
	if !strings.Contains(captured.body, `"targetVersion":1`) {
		t.Errorf("expected targetVersion in body, got %s", captured.body)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("JSON output did not parse: %v\n%s", err, out)
	}
	if _, ok := decoded["data"]; !ok {
		t.Errorf("expected 'data' key in response, got: %v", decoded)
	}
}

// TestRollback_TableSuccessMessage verifies the success message in
// US-003: "rolled back: prompt now points at version <new> (copy of v<target>)".
func TestRollback_TableSuccessMessage(t *testing.T) {
	srv, _ := startRollbackServer(t, 200, fakeRollbackBody)

	out, err := runRollbackCmd(t, srv.URL, "table",
		[]string{"--to", "1", "--yes", "pmt_abc"})
	if err != nil {
		t.Fatalf("RunE: %v\n%s", err, out)
	}

	for _, must := range []string{"rolled back", "version 4", "copy of v1"} {
		if !strings.Contains(out, must) {
			t.Errorf("success message missing %q:\n%s", must, out)
		}
	}
}

// TestRollback_IdempotencyKey covers US-003's --idempotency-key forwarding.
func TestRollback_IdempotencyKey(t *testing.T) {
	srv, captured := startRollbackServer(t, 200, fakeRollbackBody)

	_, err := runRollbackCmd(t, srv.URL, "json",
		[]string{"--to", "1", "--yes", "--idempotency-key", "deadbeef-1234", "pmt_abc"})
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if captured.idempotencyKey != "deadbeef-1234" {
		t.Errorf("expected idempotency-key header to be forwarded, got %q", captured.idempotencyKey)
	}
}

// TestRollback_RejectsZeroVersion covers the "malformed input" branch.
func TestRollback_RejectsZeroVersion(t *testing.T) {
	// Use a stub server so the command actually reaches its body validator.
	srv, _ := startRollbackServer(t, 200, fakeRollbackBody)
	_, err := runRollbackCmd(t, srv.URL, "json",
		[]string{"--to", "0", "--yes", "pmt_abc"})
	if err == nil {
		t.Fatal("expected error for --to 0")
	}
	if !strings.Contains(err.Error(), "positive") {
		t.Errorf("expected error mentioning 'positive', got: %v", err)
	}
}

// TestRollback_RequiresPromptID covers the "missing required argument"
// branch (cobra-level ExactArgs).
func TestRollback_RequiresPromptID(t *testing.T) {
	cmd := newPromptsRollbackCmd()
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Error("expected ExactArgs(1) to reject missing prompt id")
	}
}

// TestRollback_SDKErrorPropagates exercises FR-3.
func TestRollback_SDKErrorPropagates(t *testing.T) {
	srv, _ := startRollbackServer(t, 500, `{"error":"boom"}`)
	_, err := runRollbackCmd(t, srv.URL, "json",
		[]string{"--to", "1", "--yes", "pmt_abc"})
	if err == nil {
		t.Fatal("expected SDK error to propagate")
	}
}

// TestRollback_NonInteractiveWithoutYesErrors exercises the TTY guard:
// when stdin is not a TTY (CI, piped script) and --yes is absent, the
// command must surface a clear error rather than silently aborting on
// stdin EOF.
func TestRollback_NonInteractiveWithoutYesErrors(t *testing.T) {
	// Force the TTY check to report non-interactive.
	prev := output.IsInteractiveStdin
	output.IsInteractiveStdin = func() bool { return false }
	t.Cleanup(func() { output.IsInteractiveStdin = prev })

	// We never reach the SDK call, but the runRollbackCmd helper still
	// expects a base URL to wire withTestEnv.
	srv, captured := startRollbackServer(t, 200, fakeRollbackBody)

	_, err := runRollbackCmd(t, srv.URL, "json",
		[]string{"--to", "1", "pmt_abc"})
	if err == nil {
		t.Fatal("expected error when stdin is not a TTY and --yes is absent")
	}
	if !strings.Contains(err.Error(), "--yes is required") {
		t.Errorf("error should mention --yes, got: %v", err)
	}
	// Sanity: the SDK was NOT called when the TTY guard fired.
	if captured.method != "" {
		t.Errorf("expected no upstream call, but got %s %s", captured.method, captured.path)
	}
}

// TestRollback_NonInteractiveWithYesProceeds confirms the TTY guard
// is gated on --yes — passing --yes lets a non-TTY caller through.
func TestRollback_NonInteractiveWithYesProceeds(t *testing.T) {
	prev := output.IsInteractiveStdin
	output.IsInteractiveStdin = func() bool { return false }
	t.Cleanup(func() { output.IsInteractiveStdin = prev })

	srv, captured := startRollbackServer(t, 200, fakeRollbackBody)
	_, err := runRollbackCmd(t, srv.URL, "json",
		[]string{"--to", "1", "--yes", "pmt_abc"})
	if err != nil {
		t.Fatalf("--yes should bypass the TTY guard: %v", err)
	}
	if captured.method != http.MethodPost {
		t.Errorf("expected upstream POST when --yes is passed, got %q", captured.method)
	}
}
