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
	"github.com/AIEngineering26/promptvm-cli/internal/prompt"
	"github.com/spf13/cobra"
)

func newSyncInitCmd() *cobra.Command {
	var (
		workspace string
		directory string
		scope     string
		mode      string
		eventsCSV string
		yes       bool
		dryRun    bool
		force     bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Set up Context Sync for this repo (hooks + manifest + credential)",
		Long: `Wires the current repo for automatic session capture in one step:

  • writes a hierarchical manifest (.promptvm/config.json, config.local.json, or global)
  • installs a command hook into Claude Code's settings.json (SessionEnd, PreCompact,
    plus a SessionStart reconcile hook)
  • mints a workspace-bound, least-privilege capture credential the detached hook
    can use without the OS keychain

Setup is opt-in and always shows what it will write. Use --dry-run to preview the
exact file changes, and --yes for non-interactive (CI / dotfiles) installs.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// SEC-6: honor OS managed settings — never install hooks an admin disabled.
			if pol, err := managed.Detect(); err == nil && pol.Present && pol.DisableAllHooks {
				return fmt.Errorf("Claude Code managed settings (%s) set disableAllHooks; "+
					"context-sync hooks cannot be installed on this machine", pol.Path)
			}

			mScope, err := scopeToManifest(scope)
			if err != nil {
				return err
			}
			hScope, err := scopeToHooks(scope)
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
			if !inRepo && (scope == "project" || scope == "local") {
				return fmt.Errorf("not inside a git repository; project/local scope needs a repo root. " +
					"Use --scope user for a global default, or run from inside a repo")
			}

			workspaceID, err := resolveSyncWorkspace(cmd, caller)
			if err != nil {
				return err
			}

			events := parseEvents(eventsCSV)
			if len(events) == 0 {
				events = manifest.DefaultEvents
			}

			// Interactive confirmation (Huh-based prompts) unless --yes / non-TTY.
			if !yes && isTTYFunc() {
				if v, err := promptInputFunc("Target workspace", workspaceID); err == nil && v != "" {
					workspaceID = v
				}
				if v, err := promptInputFunc("Capture directory", directory); err == nil && v != "" {
					directory = v
				}
			}

			enabled := true
			redact := true
			m := &manifest.Manifest{
				SchemaVersion: manifest.SchemaVersion,
				Workspace:     workspaceID,
				Directory:     directory,
				Capture: &manifest.Capture{
					Enabled:    &enabled,
					Events:     events,
					Mode:       mode,
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

			settingsPath, err := hooks.SettingsFilePath(hScope)
			if err != nil {
				return err
			}

			if dryRun {
				return output.Print(cmd, map[string]any{
					"manifestPath": manifestPath,
					"settingsPath": settingsPath,
					"workspace":    workspaceID,
					"events":       hookEvents,
					"mode":         mode,
				}, func(w io.Writer) error {
					fmt.Fprintf(w, "[dry-run] Context Sync setup (scope=%s)\n", scope)
					fmt.Fprintf(w, "  manifest → %s\n", manifestPath)
					fmt.Fprintf(w, "    workspace=%s directory=%s mode=%s\n", workspaceID, m.Directory, mode)
					fmt.Fprintf(w, "    capture events=%s\n", strings.Join(events, ","))
					fmt.Fprintf(w, "  settings → %s\n", settingsPath)
					fmt.Fprintf(w, "    hook `%s` on %s\n", hooks.CaptureHookCommand, strings.Join(hookEvents, ","))
					if scope == "local" && inRepo {
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

			// Merge the hook into settings idempotently (force replaces prior capture hook).
			settings, err := hooks.ReadSettings(settingsPath)
			if err != nil {
				return err
			}
			settings.MergeHook(fragment, hooks.CaptureHookSlug, true)
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
			if scope == "local" && inRepo {
				if _, err := gitutil.EnsureGitignore(repo.Root, ".promptvm/config.local.json"); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not update .gitignore: %v\n", err)
				}
			}

			// DX-3 / SEC-1: mint a workspace-bound capture credential the
			// detached hook can use without the keychain.
			credStatus := mintAndStoreCredential(cmd, caller, workspaceID)

			return output.Print(cmd, map[string]any{
				"status":       "ok",
				"manifestPath": manifestPath,
				"settingsPath": settingsPath,
				"workspace":    workspaceID,
				"events":       hookEvents,
				"credential":   credStatus,
			}, func(w io.Writer) error {
				fmt.Fprintf(w, "Context Sync configured (scope=%s)\n", scope)
				fmt.Fprintf(w, "  manifest:  %s\n", manifestPath)
				fmt.Fprintf(w, "  settings:  %s (hook on %s)\n", settingsPath, strings.Join(hookEvents, ","))
				fmt.Fprintf(w, "  workspace: %s\n", workspaceID)
				fmt.Fprintf(w, "  credential: %s\n", credStatus)
				fmt.Fprintln(w, "\nVerify with: promptvm sync status")
				return nil
			})
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "Target workspace id or slug")
	cmd.Flags().StringVar(&directory, "directory", "captures", "Target capture directory")
	cmd.Flags().StringVar(&scope, "scope", "project", "Scope: local|project|user")
	cmd.Flags().StringVar(&mode, "mode", "summary", "Capture mode: summary|metadata|transcript")
	cmd.Flags().StringVar(&eventsCSV, "events", "SessionEnd,PreCompact", "Capture events (comma-separated)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Non-interactive; accept defaults")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without writing")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing capture hook")

	// `--workspace` is consumed via cmd.Flags().GetString in resolveSyncWorkspace.
	return cmd
}

// promptInputFunc is indirected so tests can bypass the interactive prompt. It
// is only ever invoked when stdin is a TTY and --yes was not passed, so the
// Huh-based prompt never fires in CI / non-interactive runs.
var promptInputFunc = prompt.Input

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

// mintAndStoreCredential mints a workspace-bound capture key and stores it for
// the hook. Best-effort: the backend `capture` scope + workspace binding is the
// sibling backend slice (SEC-1); until it ships this returns a clear status and
// does not fail setup.
func mintAndStoreCredential(cmd *cobra.Command, caller *api.Caller, workspace string) string {
	body := map[string]any{
		"name":        "context-sync capture (" + workspace + ")",
		"scopes":      []string{"capture"},
		"workspaceId": workspace,
	}
	var resp struct {
		PublicKey string `json:"publicKey"`
		SecretKey string `json:"secretKey"`
		Data      struct {
			PublicKey string `json:"publicKey"`
			SecretKey string `json:"secretKey"`
		} `json:"data"`
	}
	// TODO(context-sync): SEC-1 — depends on the backend adding the `capture`
	// scope value + nullable workspace_id binding on api_keys. Until then this
	// POST may 4xx; we degrade gracefully so init still configures the manifest.
	if err := caller.Post("/api/v1/api-keys", body, &resp); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"note: could not mint a capture credential yet (%v). Captures will spool until a credential exists.\n", err)
		return "pending (backend capture scope not available)"
	}
	pub, sec := resp.PublicKey, resp.SecretKey
	if pub == "" {
		pub, sec = resp.Data.PublicKey, resp.Data.SecretKey
	}
	if pub == "" || sec == "" {
		return "pending (no key returned)"
	}
	if _, err := capture.SaveCredential(workspace, capture.Credential{PublicKey: pub, SecretKey: sec}); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: minted credential but could not store it: %v\n", err)
		return "minted (not stored)"
	}
	return "stored"
}
