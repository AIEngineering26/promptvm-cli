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
	"github.com/AIEngineering26/promptvm-cli/internal/sanitize"
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

	// FR-Q4: drop the most obvious pure-housekeeping low-signal sessions entirely
	// (/exit, /clear, /model, /upgrade, bare pleasantries). Everything else still
	// uploads — with LowSignal set so the backend can govern it server-side.
	if req.LowSignal && isHousekeepingOnly(req.Summary) {
		fmt.Fprintln(logw, "sync run: skipping low-signal housekeeping session")
		return
	}

	uploadOrSpool(cmd, req)
}

// buildRequest assembles the ingest payload for a single capture. The contractual
// content pipeline is sanitize → redact → hash/store (FR-Q3): every string is run
// through sanitize.Sanitize (strip ANSI/CC wrappers/escaped newlines) BEFORE the
// client-side layered redaction (SEC-3/FR-12), so secrets hidden inside control
// noise are still caught and the canonical hash is stable.
func buildRequest(in HookInput, repoRoot, workspace string, mode capture.Mode, resolved *manifest.Resolved) *capture.IngestRequest {
	repo, _ := gitutil.Detect(repoRoot)

	// Normalized project identity (FR-7/FR-10): owner/repo from the remote, or
	// the repo-root basename for the "Local / no remote" bucket.
	repoSlug := gitutil.Slug(repo.RemoteURL)
	projectKey := repoSlug
	if projectKey == "" {
		projectKey = filepath.Base(repoRoot)
	}

	meta := capture.Metadata{
		RepoURL:    repo.RemoteURL,
		Branch:     repo.Branch,
		HeadSha:    repo.HeadSha,
		Outcome:    in.Reason,
		ProjectKey: projectKey,
		RepoSlug:   repoSlug,
	}

	home, _ := os.UserHomeDir()

	var summary string
	var redactionApplied bool
	var lowSignal bool
	if mode != capture.ModeMetadata {
		parsed, err := transcript.Read(in.TranscriptPath)
		if err == nil {
			// Files: sanitize → repo-relative → exclude-glob filter (FR-Q5/CAPQ-7).
			meta.FilesTouched = cleanPaths(parsed.FilesTouched, repoRoot, home, resolved)

			// Commands: sanitize → redact (when enabled).
			cmds := sanitize.Strings(parsed.Commands)

			// Conversation text: sanitize always; redact when enabled.
			text := sanitize.Sanitize(parsed.Text)
			if resolved.Redact {
				r := redact.Redact(text, resolved.ExcludePaths)
				text = r.Text
				redactionApplied = r.Applied
				before := strings.Join(cmds, "\x00")
				cmds = redactStrings(cmds)
				if strings.Join(cmds, "\x00") != before {
					redactionApplied = true
				}
			}
			meta.Commands = cmds

			// Session identity (FR-1/FR-5): the first REAL user prompt drives the
			// task + a deterministic title/description. Each value is already
			// sanitized + redacted.
			firstPrompt := firstRealUserPrompt(parsed.UserTexts, resolved)
			// taskAtHand is capped to the backend's CaptureIngestMetadataSchema
			// maxLength (2000) — an over-long first prompt would otherwise 400 the
			// whole ingest and spool-retry forever. The full firstPrompt is kept
			// for title/description derivation below.
			meta.TaskAtHand = truncateChars(firstPrompt, 2000)
			meta.Title = deriveTitle(cleanField(parsed.AITitle, resolved), firstPrompt, repoSlug)
			meta.Description = deriveDescription(firstPrompt)

			summary = heuristicSummary(text, meta.FilesTouched, cmds)

			// FR-Q4: low-signal = no real user turn AND no tool work.
			hasToolWork := len(meta.FilesTouched) > 0 || len(cmds) > 0
			lowSignal = firstPrompt == "" && !hasToolWork
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
		LowSignal:        lowSignal,
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
		// Drop pure-housekeeping low-signal sessions (FR-Q4); mark them captured
		// so they are not rescanned on every reconcile.
		if req.LowSignal && isHousekeepingOnly(req.Summary) {
			markCaptured(sessionID)
			continue
		}
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

// cleanPaths sanitizes each file path, normalizes it to repo-relative, runs it
// through the same secret-redaction pass as the rest of the payload (so a secret
// embedded in a path segment is scrubbed), drops any path matched by an excluded
// glob (SEC-3 path layer), and dedupes (FR-Q5). Ordering is sanitize → redact,
// matching the contractual content pipeline.
func cleanPaths(paths []string, repoRoot, home string, resolved *manifest.Resolved) []string {
	out := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, p := range paths {
		p = normalizePath(sanitize.Sanitize(p), repoRoot, home)
		if p == "" {
			continue
		}
		if resolved.Redact {
			r := redact.Redact(p, resolved.ExcludePaths)
			p = strings.TrimSpace(r.Text)
			if p == "" {
				continue // whole path was excluded or redacted away
			}
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// normalizePath rewrites an absolute path to be repo-relative, or home-relative
// (~/...) when it falls outside the repo, so captured paths never leak machine
// layout and read cleanly in the inbox.
func normalizePath(p, repoRoot, home string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if repoRoot != "" && filepath.IsAbs(p) {
		if rel, err := filepath.Rel(repoRoot, p); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	if home != "" && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

func redactStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, redact.Redact(s, nil).Text)
	}
	return out
}

// cleanField sanitizes then (when enabled) redacts a single derived string,
// honoring the sanitize→redact ordering contract (FR-Q3). It never drops lines
// (no exclude-path layer) — it only scrubs control noise + secrets.
func cleanField(s string, resolved *manifest.Resolved) string {
	s = sanitize.Sanitize(s)
	if resolved.Redact {
		s = redact.Redact(s, nil).Text
	}
	return strings.TrimSpace(s)
}

// firstRealUserPrompt returns the first sanitized+redacted user turn that is a
// real prompt per FR-Q4: non-empty, and not a slash-command (`/…`) or a leftover
// Claude Code wrapper (`<…`). Returns "" when the session has no real user turn.
func firstRealUserPrompt(userTexts []string, resolved *manifest.Resolved) string {
	for _, raw := range userTexts {
		clean := cleanField(raw, resolved)
		if clean == "" {
			continue
		}
		if strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "<") {
			continue
		}
		return clean
	}
	return ""
}

// deriveTitle builds a human title via the FR-1 fallback chain: a transcript ai
// title → the first ~10 words of the first real user prompt → a deterministic
// "{repoSlug} session — {date}". The title is NEVER blank.
func deriveTitle(aiTitle, firstPrompt, repoSlug string) string {
	if aiTitle != "" {
		return truncateChars(aiTitle, 80)
	}
	if firstPrompt != "" {
		return truncateChars(firstWords(firstPrompt, 10), 80)
	}
	label := repoSlug
	if label == "" {
		label = "session"
	}
	return fmt.Sprintf("%s session — %s", label, time.Now().UTC().Format("2006-01-02"))
}

// deriveDescription distills a clean 1–2 sentence description from the first real
// user prompt. Empty when there is no real prompt.
func deriveDescription(firstPrompt string) string {
	if firstPrompt == "" {
		return ""
	}
	return truncateChars(firstSentences(firstPrompt, 2), 300)
}

// firstWords returns the first n whitespace-separated words of s, with an ellipsis
// when truncated.
func firstWords(s string, n int) string {
	fields := strings.Fields(s)
	if len(fields) <= n {
		return strings.Join(fields, " ")
	}
	return strings.Join(fields[:n], " ") + "…"
}

// firstSentences collapses s to a single line and returns its first n sentences.
func firstSentences(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	count := 0
	for i, r := range s {
		if r == '.' || r == '!' || r == '?' {
			count++
			if count >= n {
				return strings.TrimSpace(s[:i+1])
			}
		}
	}
	return s
}

// truncateChars rune-safely caps s to at most max characters, appending an
// ellipsis (counted within the budget) when it had to cut — so callers can rely
// on the result satisfying a hard maxLength bound (e.g. the backend's 2000-char
// taskAtHand schema cap).
func truncateChars(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return string(r[:max])
	}
	return strings.TrimSpace(string(r[:max-1])) + "…"
}

// housekeepingCommands are slash-commands with no task value (FR-Q4 drop case).
var housekeepingCommands = map[string]bool{
	"/exit": true, "/clear": true, "/model": true, "/upgrade": true,
	"/logout": true, "/login": true, "/quit": true, "/bye": true, "/help": true,
}

// pleasantries are bare farewells/acks with no task value (FR-Q4 drop case).
var pleasantries = map[string]bool{
	"see ya": true, "see ya!": true, "bye": true, "bye!": true, "goodbye": true,
	"goodbye!": true, "catch you later": true, "catch you later!": true,
	"thanks": true, "thank you": true, "ok": true, "okay": true, "cool": true,
}

// isHousekeepingOnly reports whether text contains nothing but housekeeping
// slash-commands and/or bare pleasantries. It is deliberately conservative: any
// substantive line, or any text over a short length bound, makes it false so a
// real session is never silently dropped.
func isHousekeepingOnly(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return true
	}
	if len(t) > 120 {
		return false
	}
	for _, line := range strings.Split(t, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || pleasantries[line] {
			continue
		}
		if fields := strings.Fields(line); len(fields) > 0 && housekeepingCommands[fields[0]] {
			continue
		}
		return false
	}
	return true
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
