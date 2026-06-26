package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AIEngineering26/promptvm-cli/internal/capture"
	"github.com/AIEngineering26/promptvm-cli/internal/detach"
	"github.com/AIEngineering26/promptvm-cli/internal/gitutil"
	"github.com/AIEngineering26/promptvm-cli/internal/managed"
	"github.com/AIEngineering26/promptvm-cli/internal/manifest"
	"github.com/AIEngineering26/promptvm-cli/internal/redact"
	"github.com/AIEngineering26/promptvm-cli/internal/spool"
	"github.com/AIEngineering26/promptvm-cli/internal/transcript"
	"github.com/spf13/cobra"
)

// HookInput is the JSON Claude Code writes to the hook's stdin.
type HookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	Reason         string `json:"reason"`  // SessionEnd: clear|logout|prompt_input_exit|other
	Trigger        string `json:"trigger"` // PreCompact: manual|auto
	Source         string `json:"source"`  // SessionStart: startup|resume|clear|compact
}

func newSyncRunCmd() *cobra.Command {
	var (
		modeOverride string
		wsOverride   string
		dryRun       bool
		noDetach     bool
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Hook-invoked uploader: capture the current session (reads stdin)",
		Long: `Invoked by the Claude Code capture hook. Reads the hook event JSON from stdin,
resolves the manifest, redacts secrets client-side, and uploads a distilled
capture. It self-detaches and exits 0 immediately so it never blocks Claude Code;
on failure it spools the capture locally for the next reconcile. Not typically
run by hand.`,
		Args:          cobra.NoArgs,
		SilenceErrors: true, // a hook must never surface a non-zero/noisy failure
		RunE: func(cmd *cobra.Command, args []string) error {
			// Read the hook payload from stdin.
			raw, _ := io.ReadAll(cmd.InOrStdin())

			var in HookInput
			if len(raw) > 0 {
				_ = json.Unmarshal(raw, &in)
			}

			// SEC-6: no-op entirely under a managed disableAllHooks policy.
			if managed.HooksDisabled() {
				return nil
			}

			// Self-detach (HOOK-3 / DX-6): re-exec a detached child and return
			// immediately. The child re-enters with --no-detach and the same
			// stdin (wired from a temp file) so it can finish off the critical
			// path. Skipped when already the child, when --no-detach, or in
			// dry-run.
			if !noDetach && !dryRun && !detach.IsChild() {
				if spawnDetachedChild(raw) {
					return nil
				}
				// If detaching failed, fall through and do the work inline.
			}

			processHook(cmd, in, modeOverride, wsOverride, dryRun)
			return nil // always succeed; failures are spooled
		},
	}

	cmd.Flags().StringVar(&modeOverride, "mode", "", "Override capture mode: summary|metadata|transcript")
	cmd.Flags().StringVar(&wsOverride, "workspace", "", "Override target workspace")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Resolve + build the payload but do not upload")
	cmd.Flags().BoolVar(&noDetach, "no-detach", false, "Run inline without self-detaching (used by reconcile + tests)")
	return cmd
}

// spawnDetachedChild persists the hook stdin to a temp file and re-execs a
// detached copy of the CLI that reads it. Returns true on success.
func spawnDetachedChild(stdin []byte) bool {
	tmp, err := os.CreateTemp("", "promptvm-sync-stdin-*")
	if err != nil {
		return false
	}
	if _, err := tmp.Write(stdin); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return false
	}
	tmp.Close()
	if _, err := detach.Reexec(tmp.Name()); err != nil {
		os.Remove(tmp.Name())
		return false
	}
	// The child owns the temp file now; it is small and in the OS temp dir.
	return true
}

// processHook performs the actual capture. It never returns an error to the
// caller — any failure is logged and spooled so the hook stays non-blocking.
func processHook(cmd *cobra.Command, in HookInput, modeOverride, wsOverride string, dryRun bool) {
	logw := cmd.ErrOrStderr()

	repoRoot := in.Cwd
	if repo, ok := gitutil.Detect(in.Cwd); ok {
		repoRoot = repo.Root
	}

	resolved, err := manifest.Resolve(repoRoot)
	if err != nil {
		fmt.Fprintf(logw, "sync run: manifest resolve failed: %v\n", err)
		return
	}
	if !resolved.Enabled {
		return // capture disabled for this scope (FR-7)
	}
	workspace := resolved.Workspace
	if wsOverride != "" {
		workspace = wsOverride
	}

	// SessionStart is reconcile-only (HOOK-1/HOOK-4), never a capture trigger.
	if in.HookEventName == "SessionStart" {
		reconcile(cmd, repoRoot, resolved, workspace)
		return
	}

	// Only capture on selected events.
	if !resolved.EventSelected(in.HookEventName) {
		return
	}

	mode := capture.Mode(resolved.Mode)
	if modeOverride != "" {
		mode = capture.Mode(modeOverride)
	}

	req := buildRequest(in, repoRoot, workspace, mode, resolved)
	req.ContentHash = req.ComputeContentHash()

	if dryRun {
		data, _ := json.MarshalIndent(req, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return
	}

	uploadOrSpool(cmd, req)
}

// buildRequest assembles the ingest payload for a single capture, applying
// client-side layered redaction BEFORE any content is attached (SEC-3/FR-12).
func buildRequest(in HookInput, repoRoot, workspace string, mode capture.Mode, resolved *manifest.Resolved) *capture.IngestRequest {
	repo, _ := gitutil.Detect(repoRoot)

	meta := capture.Metadata{
		RepoURL: repo.RemoteURL,
		Branch:  repo.Branch,
		HeadSha: repo.HeadSha,
		Outcome: in.Reason,
	}

	var summary string
	var redactionApplied bool
	if mode != capture.ModeMetadata {
		parsed, err := transcript.Read(in.TranscriptPath)
		if err == nil {
			meta.FilesTouched = filterPaths(parsed.FilesTouched, resolved)
			meta.Commands = parsed.Commands
			text := parsed.Text
			if resolved.Redact {
				r := redact.Redact(text, resolved.ExcludePaths)
				text = r.Text
				redactionApplied = r.Applied
				// Redact secrets in command lines too, tracking whether it fired.
				before := strings.Join(meta.Commands, "\x00")
				meta.Commands = redactStrings(meta.Commands)
				if strings.Join(meta.Commands, "\x00") != before {
					redactionApplied = true
				}
			}
			// TODO(context-sync): richer local distillation. v1 ships a cheap
			// heuristic summary; server-side distillation (AI-2) enriches it.
			summary = heuristicSummary(text, parsed.FilesTouched, parsed.Commands)
		}
	}

	return &capture.IngestRequest{
		WorkspaceID:      workspace,
		ClaudeSessionID:  in.SessionID,
		Source:           "claude-code",
		CaptureMode:      mode,
		Summary:          summary,
		Metadata:         meta,
		OccurredAt:       time.Now().UTC().Format(time.RFC3339),
		RedactionApplied: redactionApplied,
	}
}

// uploadOrSpool attempts an upload via the workspace capture credential and
// spools on any failure (offline, no credential, server error). On success it
// records the session in the ledger and clears any matching spool entry.
func uploadOrSpool(cmd *cobra.Command, req *capture.IngestRequest) {
	logw := cmd.ErrOrStderr()

	caller, err := captureCaller(cmd, req.WorkspaceID)
	if err == nil && caller != nil {
		if _, ierr := capture.Ingest(caller, req); ierr == nil {
			markCaptured(req.ClaudeSessionID)
			clearSpool(req)
			return
		} else {
			fmt.Fprintf(logw, "sync run: upload failed, spooling: %v\n", ierr)
		}
	} else if caller == nil {
		fmt.Fprintln(logw, "sync run: no capture credential yet, spooling")
	}

	if _, serr := spool.Add(spoolEntry(req)); serr != nil {
		fmt.Fprintf(logw, "sync run: spool failed: %v\n", serr)
	}
}

// reconcile flushes ALL pending spool entries regardless of cwd (DX-7) and,
// transcript-driven (HOOK-1), captures any recent session absent from the
// ledger. It is stdout-silent (HOOK-4) so it never pollutes a new session.
func reconcile(cmd *cobra.Command, repoRoot string, resolved *manifest.Resolved, workspace string) {
	logw := cmd.ErrOrStderr()

	// 1. Flush the spool (all entries, repo-agnostic).
	entries, err := spool.List()
	if err == nil {
		for _, e := range entries {
			caller, cerr := captureCaller(cmd, e.WorkspaceID)
			if cerr != nil || caller == nil {
				continue
			}
			if _, ierr := capture.Ingest(caller, e.Payload); ierr == nil {
				markCaptured(e.ClaudeSessionID)
				_ = e.Remove()
			} else {
				_ = e.MarkAttempt()
			}
		}
	}

	// 2. Transcript-driven catch-up: capture sessions never uploaded.
	// TODO(context-sync): HOOK-1 full enumeration of ~/.claude/projects/*/*.jsonl.
	// v1 reconciles the spool + ledger; broad transcript scanning is wired via
	// reconcileTranscripts below and bounded to avoid heavy startup cost.
	reconcileTranscripts(cmd, repoRoot, resolved, workspace)

	fmt.Fprintln(logw, "sync run: reconcile complete")
}

// reconcileTranscripts enumerates recent transcripts and captures any whose
// session id is not in the ledger (HOOK-1).
func reconcileTranscripts(cmd *cobra.Command, repoRoot string, resolved *manifest.Resolved, workspace string) {
	led, err := spool.LoadLedger()
	if err != nil {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	pattern := filepath.Join(home, ".claude", "projects", "*", "*.jsonl")
	files, _ := filepath.Glob(pattern)
	for _, f := range files {
		sessionID := strings.TrimSuffix(filepath.Base(f), ".jsonl")
		if sessionID == "" || led.Has(sessionID) {
			continue
		}
		in := HookInput{
			SessionID:      sessionID,
			TranscriptPath: f,
			Cwd:            repoRoot,
			HookEventName:  "SessionEnd",
			Reason:         "reconcile",
		}
		req := buildRequest(in, repoRoot, workspace, capture.Mode(resolved.Mode), resolved)
		req.ContentHash = req.ComputeContentHash()
		uploadOrSpool(cmd, req)
	}
}

// --- small helpers ---

func spoolEntry(req *capture.IngestRequest) *spool.Entry {
	return &spool.Entry{
		ClaudeSessionID: req.ClaudeSessionID,
		WorkspaceID:     req.WorkspaceID,
		DirectoryID:     req.DirectoryID,
		CaptureMode:     req.CaptureMode,
		RepoURL:         req.Metadata.RepoURL,
		ContentHash:     req.ContentHash,
		Payload:         req,
	}
}

func clearSpool(req *capture.IngestRequest) {
	entries, err := spool.List()
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.ClaudeSessionID == req.ClaudeSessionID && e.ContentHash == req.ContentHash {
			_ = e.Remove()
		}
	}
}

func markCaptured(sessionID string) {
	led, err := spool.LoadLedger()
	if err != nil {
		return
	}
	led.Mark(sessionID)
	_ = led.Save()
}

// filterPaths drops file paths that match an excluded glob (SEC-3 path layer).
func filterPaths(paths []string, resolved *manifest.Resolved) []string {
	if !resolved.Redact || len(resolved.ExcludePaths) == 0 {
		return paths
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		r := redact.Redact(p, resolved.ExcludePaths)
		if r.Applied && strings.TrimSpace(r.Text) == "" {
			continue // whole path was excluded
		}
		out = append(out, p)
	}
	return out
}

func redactStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, redact.Redact(s, nil).Text)
	}
	return out
}

// heuristicSummary builds a cheap local summary for `summary` mode. Server-side
// distillation (AI-2) replaces/enriches this; here we keep egress minimal.
func heuristicSummary(text string, files, cmds []string) string {
	var b strings.Builder
	if n := len(files); n > 0 {
		fmt.Fprintf(&b, "Touched %d file(s)", n)
	}
	if n := len(cmds); n > 0 {
		if b.Len() > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "ran %d command(s)", n)
	}
	// Append a bounded slice of the conversation text for context (AI-7: bound input).
	trimmed := strings.TrimSpace(text)
	if trimmed != "" {
		const max = 2000
		if len(trimmed) > max {
			trimmed = trimmed[:max] + "…"
		}
		if b.Len() > 0 {
			b.WriteString(". ")
		}
		b.WriteString(trimmed)
	}
	return b.String()
}
