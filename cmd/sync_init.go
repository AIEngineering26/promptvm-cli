package cmd

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/AIEngineering26/promptvm-cli/internal/api"
	"github.com/AIEngineering26/promptvm-cli/internal/capture"
	"github.com/AIEngineering26/promptvm-cli/internal/gitutil"
	"github.com/AIEngineering26/promptvm-cli/internal/hooks"
	"github.com/AIEngineering26/promptvm-cli/internal/managed"
	"github.com/AIEngineering26/promptvm-cli/internal/manifest"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

// syncInitOptions carries every knob of `sync init` so `promptvm setup` can
// invoke the same flow programmatically without shelling out.
type syncInitOptions struct {
	Workspace   string
	Directory   string
	Scope       string
	Mode        string
	EventsCSV   string
	Interactive bool
	DryRun      bool
	Force       bool
}

func newSyncInitCmd() *cobra.Command {
	o := syncInitOptions{}

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up Context Sync for this repo (hooks + manifest + credential)",
		Long: `Wires the current repo for automatic session capture in one step:

  • writes a hierarchical manifest (.promptvm/config.json, config.local.json, or global)
  • installs a command hook into Claude Code's settings.json (SessionEnd, PreCompact,
    plus a SessionStart reconcile hook)
  • mints a workspace-bound, least-privilege capture credential the detached hook
    can use without the OS keychain
  • flushes any captures that spooled locally while no credential existed

Zero prompts by default: the workspace resolves from --workspace → the config
default → your account default, and names/slugs are normalized to the workspace
UUID automatically. Pass --interactive to pick a workspace from a list instead.
Use --dry-run to preview the exact file changes.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSyncInit(cmd, o)
		},
	}

	cmd.Flags().StringVar(&o.Workspace, "workspace", "", "Target workspace UUID, slug, or name (normalized to the UUID)")
	cmd.Flags().StringVar(&o.Directory, "directory", "captures", "Target capture directory")
	cmd.Flags().StringVar(&o.Scope, "scope", "project", "Scope: local|project|user")
	cmd.Flags().StringVar(&o.Mode, "mode", "summary", "Capture mode: summary|metadata|transcript")
	cmd.Flags().StringVar(&o.EventsCSV, "events", "SessionEnd,PreCompact", "Capture events (comma-separated)")
	cmd.Flags().BoolVar(&o.Interactive, "interactive", false, "Pick the target workspace from a list (opt-in; default is zero prompts)")
	cmd.Flags().BoolVar(&o.DryRun, "dry-run", false, "Preview changes without writing")
	cmd.Flags().BoolVar(&o.Force, "force", false, "Replace a previously installed capture hook (instead of leaving it untouched)")

	// Back-compat: --yes used to opt out of prompts; prompts are now opt-in,
	// so --yes is accepted-and-ignored.
	var yes bool
	cmd.Flags().BoolVar(&yes, "yes", false, "Deprecated: init is non-interactive by default")
	_ = cmd.Flags().MarkDeprecated("yes", "init is non-interactive by default; use --interactive to opt in to prompts")

	return cmd
}

// runSyncInit performs the full init flow. It is shared by `sync init` and the
// `promptvm setup` orchestrator.
func runSyncInit(cmd *cobra.Command, o syncInitOptions) error {
	// SEC-6: honor OS managed settings — never install hooks an admin disabled.
	if pol, err := managed.Detect(); err == nil && pol.Present && pol.DisableAllHooks {
		return fmt.Errorf("managed Claude Code settings (%s) set disableAllHooks; "+
			"context-sync hooks cannot be installed on this machine", pol.Path)
	}

	mScope, err := scopeToManifest(o.Scope)
	if err != nil {
		return err
	}
	hScope, err := scopeToHooks(o.Scope)
	if err != nil {
		return err
	}

	// DX-8: detect auth up front so we can offer `auth login`.
	caller, authErr := api.NewFromContext(cmd)
	if authErr != nil {
		return fmt.Errorf("not authenticated: %w\n  run `promptvm auth login` first, then re-run `promptvm sync init`", authErr)
	}

	// Detect repo + remote so the manifest and provenance are accurate.
	repo, inRepo := gitutil.Detect("")
	if !inRepo && (o.Scope == "project" || o.Scope == "local") {
		return fmt.Errorf("not inside a git repository; project/local scope needs a repo root. " +
			"Use --scope user for a global default, or run from inside a repo")
	}

	// Resolve the workspace — zero prompts. Precedence: --workspace flag →
	// config defaults.workspace → account default. ALWAYS normalized to a UUID.
	var workspaceID, workspaceName string
	if o.Interactive && isTTYFunc() {
		workspaceID, workspaceName, err = selectWorkspaceInteractive(caller)
	} else {
		workspaceID, workspaceName, err = resolveSyncWorkspace(caller, o.Workspace)
	}
	if err != nil {
		return err
	}

	events := parseEvents(o.EventsCSV)
	if len(events) == 0 {
		events = manifest.DefaultEvents
	}

	enabled := true
	redact := true
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Workspace:     workspaceID, // UUID only — never a name/slug (contract)
		Directory:     o.Directory,
		Capture: &manifest.Capture{
			Enabled:    &enabled,
			Events:     events,
			Mode:       o.Mode,
			Redact:     &redact,
			Governance: "inherit",
		},
	}

	manifestPath, err := manifest.Path(mScope, repo.Root)
	if err != nil {
		return err
	}

	// Hook events = capture events ∪ SessionStart (reconcile, HOOK-1).
	hookEvents := withSessionStart(events)
	fragment := hooks.BuildCaptureFragment(hookEvents)

	// Anchor project/local settings at the git repo root — the same root the
	// manifest uses — never at whatever subdirectory the command ran from.
	settingsPath, err := hooks.SettingsFilePathAt(hScope, repo.Root)
	if err != nil {
		return err
	}

	if o.DryRun {
		return output.Print(cmd, map[string]any{
			"manifestPath": manifestPath,
			"settingsPath": settingsPath,
			"workspace":    workspaceID,
			"events":       hookEvents,
			"mode":         o.Mode,
		}, func(w io.Writer) error {
			fmt.Fprintf(w, "[dry-run] Context Sync setup (scope=%s)\n", o.Scope)
			fmt.Fprintf(w, "  manifest → %s\n", manifestPath)
			fmt.Fprintf(w, "    workspace=%s directory=%s mode=%s\n", workspaceID, m.Directory, o.Mode)
			fmt.Fprintf(w, "    capture events=%s\n", strings.Join(events, ","))
			fmt.Fprintf(w, "  settings → %s\n", settingsPath)
			fmt.Fprintf(w, "    hook `%s` on %s\n", hooks.CaptureHookCommand, strings.Join(hookEvents, ","))
			if o.Scope == "local" && inRepo {
				fmt.Fprintf(w, "  gitignore → %s/.gitignore (+ .promptvm/config.local.json)\n", repo.Root)
			}
			fmt.Fprintln(w, "  credential → workspace-bound capture key (minted on apply)")
			return nil
		})
	}

	// Write manifest.
	if err := manifest.Write(manifestPath, m); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}

	// Merge the hook into settings idempotently (--force replaces any prior
	// capture hook from this slug; without it, identical config dedupes).
	settings, err := hooks.ReadSettings(settingsPath)
	if err != nil {
		return err
	}
	settings.MergeHook(fragment, hooks.CaptureHookSlug, o.Force)
	if err := settings.Write(settingsPath); err != nil {
		return fmt.Errorf("writing settings: %w", err)
	}

	// Track the install in the scope sidecar.
	tracker, err := hooks.LoadTracker(hScope)
	if err == nil {
		tracker.Add(hooks.TrackedHook{
			Slug:        hooks.CaptureHookSlug,
			Version:     1,
			InstalledAt: time.Now().UTC().Format(time.RFC3339),
			Events:      hookEvents,
			Checksum:    hooks.Checksum(fragment),
		})
		_ = tracker.Save()
	}

	// DX-10: gitignore the machine-local manifest at the repo root.
	if o.Scope == "local" && inRepo {
		if _, err := gitutil.EnsureGitignore(repo.Root, ".promptvm/config.local.json"); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not update .gitignore: %v\n", err)
		}
	}

	// DX-3 / SEC-1: mint a workspace-bound capture credential the detached
	// hook can use without the keychain. The manifest + hooks above are
	// already written — a mint failure degrades to spooling, never to a
	// half-configured repo.
	credStatus := mintAndStoreCredential(cmd, caller, workspaceID)

	// A freshly stored credential unblocks any captures that spooled while no
	// credential existed (e.g. OAuth-only logins before this init ran).
	flushed := 0
	if credCheckmark(credStatus) == "✓" { // any stored/reused credential unblocks the spool
		flushed, _ = flushSpoolForWorkspace(cmd, workspaceID)
	}

	wsLabel := workspaceID
	if workspaceName != "" {
		wsLabel = fmt.Sprintf("%s (%s)", workspaceName, workspaceID)
	}

	return output.Print(cmd, map[string]any{
		"status":        "ok",
		"manifestPath":  manifestPath,
		"settingsPath":  settingsPath,
		"workspace":     workspaceID,
		"workspaceName": workspaceName,
		"events":        hookEvents,
		"credential":    credStatus,
		"spoolFlushed":  flushed,
	}, func(w io.Writer) error {
		fmt.Fprintf(w, "Context Sync configured (scope=%s)\n", o.Scope)
		fmt.Fprintf(w, "  ✓ workspace:  %s\n", wsLabel)
		fmt.Fprintf(w, "  ✓ manifest:   %s\n", manifestPath)
		fmt.Fprintf(w, "  ✓ hooks:      %s (on %s)\n", settingsPath, strings.Join(hookEvents, ", "))
		fmt.Fprintf(w, "  %s credential: %s\n", credCheckmark(credStatus), credStatus)
		if flushed > 0 {
			fmt.Fprintf(w, "  ✓ spool:      flushed %d pending capture(s)\n", flushed)
		}
		fmt.Fprintln(w, "\nVerify: promptvm sync status")
		return nil
	})
}

// selectWorkspaceInteractive shows a huh Select of the caller's workspaces
// (default preselected) — a picker only, never free-text input.
func selectWorkspaceInteractive(caller *api.Caller) (id, name string, err error) {
	items, err := fetchWorkspaces(caller)
	if err != nil {
		return "", "", fmt.Errorf("listing workspaces: %w", err)
	}
	if len(items) == 0 {
		return "", "", fmt.Errorf("no workspaces available for this account")
	}
	return selectWorkspaceFunc(items)
}

// selectWorkspaceFunc is indirected so tests can bypass the interactive TUI.
var selectWorkspaceFunc = func(items []workspaceItem) (id, name string, err error) {
	byID := map[string]workspaceItem{}
	opts := make([]huh.Option[string], 0, len(items))
	var selected string
	for _, w := range items {
		byID[w.ID] = w
		label := w.Name
		if w.Slug != "" {
			label += " (" + w.Slug + ")"
		}
		if w.IsDefault {
			label += " — default"
			selected = w.ID
		}
		opts = append(opts, huh.NewOption(label, w.ID))
	}
	if selected == "" {
		selected = items[0].ID
	}
	serr := huh.NewSelect[string]().
		Title("Target workspace").
		Description("Captured Claude Code sessions will upload into this workspace.").
		Options(opts...).
		Value(&selected).
		Run()
	if serr != nil {
		return "", "", serr
	}
	return selected, byID[selected].Name, nil
}

// parseEvents splits and trims a comma-separated event list.
func parseEvents(csv string) []string {
	if strings.TrimSpace(csv) == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// withSessionStart returns events plus SessionStart (deduped) so the reconcile
// hook is always installed (HOOK-1) even when capture only fires on SessionEnd.
func withSessionStart(events []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(events)+1)
	for _, e := range events {
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	if !seen["SessionStart"] {
		out = append(out, "SessionStart")
	}
	return out
}

// Credential status values reported by mintAndStoreCredential.
const (
	credStored         = "stored"
	credStoredFallback = "stored (write-scope fallback)"
	credReused         = "stored (existing key reused)"
	credSwapped        = "stored (write-scope fallback replaced with capture key)"
	credFailed         = "failed (captures will spool)"
	credNoKey          = "pending (no key returned)"
	credNotStored      = "minted (not stored)"
)

// credCheckmark maps a credential status to a checklist glyph.
func credCheckmark(status string) string {
	switch status {
	case credStored, credStoredFallback, credReused, credSwapped:
		return "✓"
	default:
		return "✗"
	}
}

// isScopeEnumRejection reports whether an api-keys mint error is a backend 400
// scope-enum rejection — i.e. the deployed backend does not (yet) accept the
// `capture` scope. It matches both known bodies (the legacy Ajv "body/scopes/0
// must be equal to one of the allowed values" and the newer "Allowed values:"
// formatter) and deliberately requires the enum wording so that other 400s
// that merely mention "scopes" take the loud-failure path instead of silently
// downgrading to an over-broad write key.
func isScopeEnumRejection(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "api error 400") &&
		strings.Contains(msg, "scopes") &&
		strings.Contains(msg, "allowed values")
}

// mintAndStoreCredential mints a workspace-bound capture key and stores it for
// the hook. Contract: scopes:["capture"] + workspaceId (UUID). A credential
// already stored for the workspace is REUSED (capture keys never expire, so
// re-running init/setup must not accumulate live keys) — except a write-scope
// fallback credential, which is swapped for a least-privilege capture key as
// soon as the backend accepts the capture scope. When the backend rejects the
// capture scope enum (backend fix ships in parallel), it FALLS BACK to a
// write-scoped key and warns that it is broader than intended. Any other
// failure prints a loud, actionable error — the manifest + hooks are already
// written, so captures spool until the credential exists.
func mintAndStoreCredential(cmd *cobra.Command, caller *api.Caller, workspaceID string) string {
	existing, _ := capture.LoadCredential(workspaceID)
	if existing != nil && existing.Scope != capture.ScopeWrite {
		return credReused
	}

	fallback := false
	pub, sec, err := mintAPIKey(caller, "context-sync capture ("+workspaceID+")", []string{"capture"}, workspaceID)

	if existing != nil {
		// A write-scope fallback credential is stored: swap in the freshly
		// minted least-privilege key, or keep the fallback when the backend
		// still rejects the capture scope (or the mint failed).
		if err != nil || pub == "" || sec == "" {
			return credReused
		}
		if _, serr := capture.SaveCredential(workspaceID, capture.Credential{PublicKey: pub, SecretKey: sec, Scope: capture.ScopeCapture}); serr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: minted a capture-scoped key but could not store it: %v\n", serr)
			return credReused
		}
		fmt.Fprintf(cmd.ErrOrStderr(),
			"note: replaced the write-scope fallback credential (public key %s) with a least-privilege capture key.\n"+
				"      Revoke the old write key: promptvm apikeys list / promptvm apikeys revoke <id>\n", existing.PublicKey)
		return credSwapped
	}

	if isScopeEnumRejection(err) {
		fallback = true
		fmt.Fprintln(cmd.ErrOrStderr(),
			"warning: this backend does not accept the `capture` scope yet; minting a `write`-scoped key instead.\n"+
				"         This credential is BROADER than intended — re-run `promptvm sync doctor` after the backend\n"+
				"         capture scope ships to swap in a least-privilege key.")
		// No workspaceId on the fallback: write keys are not workspace-bound,
		// and newer backends reject a non-capture mint that carries one.
		pub, sec, err = mintAPIKey(caller, "context-sync capture (fallback write scope)", []string{"write"}, "")
	}
	if err != nil {
		credPath, _ := capture.CredentialPath(workspaceID)
		fmt.Fprintf(cmd.ErrOrStderr(), `
✗ Could not mint a capture credential for workspace %s.

  Error: %v

  The manifest and hooks were still written, so sessions WILL be captured —
  they spool locally and upload once a credential exists.

  To fix:
    1. Check auth:            promptvm auth status
    2. Re-mint + flush spool: promptvm sync doctor
    3. Or store a key pair manually at %s:
         PROMPTVM_PUBLIC_KEY=pk_…
         PROMPTVM_SECRET_KEY=sk_…

`, workspaceID, err, credPath)
		return credFailed
	}
	if pub == "" || sec == "" {
		return credNoKey
	}
	scope := capture.ScopeCapture
	if fallback {
		scope = capture.ScopeWrite
	}
	if _, err := capture.SaveCredential(workspaceID, capture.Credential{PublicKey: pub, SecretKey: sec, Scope: scope}); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: minted credential but could not store it: %v\n", err)
		return credNotStored
	}
	if fallback {
		return credStoredFallback
	}
	return credStored
}

// mintAPIKey POSTs /api/v1/api-keys with the contract body
// {"name","scopes","workspaceId"} and tolerates both the top-level and
// enveloped ({data:{…}}) response shapes.
func mintAPIKey(caller *api.Caller, name string, scopes []string, workspaceID string) (pub, sec string, err error) {
	body := map[string]any{
		"name":   name,
		"scopes": scopes,
	}
	if workspaceID != "" {
		body["workspaceId"] = workspaceID
	}
	var resp struct {
		PublicKey string `json:"publicKey"`
		SecretKey string `json:"secretKey"`
		Data      struct {
			PublicKey string `json:"publicKey"`
			SecretKey string `json:"secretKey"`
		} `json:"data"`
	}
	if err := caller.Post("/api/v1/api-keys", body, &resp); err != nil {
		return "", "", err
	}
	pub, sec = resp.PublicKey, resp.SecretKey
	if pub == "" {
		pub, sec = resp.Data.PublicKey, resp.Data.SecretKey
	}
	return pub, sec, nil
}
