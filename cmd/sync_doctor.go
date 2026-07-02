package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/AIEngineering26/promptvm-cli/internal/api"
	"github.com/AIEngineering26/promptvm-cli/internal/capture"
	"github.com/AIEngineering26/promptvm-cli/internal/gitutil"
	"github.com/AIEngineering26/promptvm-cli/internal/hooks"
	"github.com/AIEngineering26/promptvm-cli/internal/manifest"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/AIEngineering26/promptvm-cli/internal/spool"
	"github.com/spf13/cobra"
)

// doctorCheck is one diagnose/repair step result.
type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok | fixed | failed | skipped
	Detail string `json:"detail,omitempty"`
}

func newSyncDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose and repair Context Sync (workspace UUID, credential, hooks, spool)",
		Long: `Checks the Context Sync installation for this repo and repairs what it can:

  • manifest    — a manifest exists and names a workspace
  • workspace   — the manifest workspace is a UUID; a name/slug is normalized
                  (manifest rewritten, credential file renamed to <uuid>.env)
  • credential  — a capture credential is stored; a missing one is re-minted
  • hooks       — the capture hooks are installed; missing ones are reinstalled
  • spool       — pending captures are flushed

Each check prints ok, fixed, failed, or skipped.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			checks := runSyncDoctor(cmd)
			failed := false
			for _, c := range checks {
				if c.Status == "failed" {
					failed = true
				}
			}
			err := output.Print(cmd, checks, func(w io.Writer) error {
				fmt.Fprintln(w, "Context Sync doctor")
				for _, c := range checks {
					glyph := map[string]string{"ok": "✓", "fixed": "✓", "failed": "✗", "skipped": "-"}[c.Status]
					fmt.Fprintf(w, "  %s %-10s %-7s %s\n", glyph, c.Name, c.Status, c.Detail)
				}
				fmt.Fprintln(w, "\nVerify: promptvm sync status")
				return nil
			})
			if err != nil {
				return err
			}
			if failed {
				return fmt.Errorf("one or more checks failed (see above)")
			}
			return nil
		},
	}
	return cmd
}

// runSyncDoctor executes every diagnose/repair step and returns the results.
func runSyncDoctor(cmd *cobra.Command) []doctorCheck {
	var checks []doctorCheck
	add := func(name, status, detail string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Detail: detail})
	}

	repoRoot := ""
	if repo, ok := gitutil.Detect(""); ok {
		repoRoot = repo.Root
	}

	// Auth is needed for normalization + re-minting; degrade gracefully.
	caller, authErr := api.NewFromContext(cmd)
	if authErr != nil {
		caller = nil
	}

	// 1. Manifest: find the most-specific scope file that names a workspace.
	wsScope, wsManifest, wsPath := findWorkspaceManifest(repoRoot)
	if wsManifest == nil {
		add("manifest", "failed", "no manifest found — run `promptvm sync init`")
		// Hooks + spool checks can still run.
		checkHooks(cmd, add, repoRoot, nil)
		checkSpool(cmd, add, "")
		return checks
	}
	add("manifest", "ok", fmt.Sprintf("%s (%s scope)", wsPath, wsScope))

	// 2. Workspace UUID normalization.
	workspaceID := wsManifest.Workspace
	if !isUUID(workspaceID) {
		if caller == nil {
			add("workspace", "failed", fmt.Sprintf("%q is not a UUID and the CLI is not authenticated to resolve it — run `promptvm auth login`", workspaceID))
		} else if id, name, err := normalizeWorkspace(caller, workspaceID); err != nil {
			add("workspace", "failed", err.Error())
		} else {
			old := workspaceID
			wsManifest.Workspace = id
			if werr := manifest.Write(wsPath, wsManifest); werr != nil {
				add("workspace", "failed", fmt.Sprintf("rewriting manifest: %v", werr))
			} else {
				renameCredentialFile(old, id)
				// Spool entries recorded under the old slug/name would never
				// find the renamed <uuid>.env credential — rekey them so the
				// spool check below can actually flush them.
				rekeyed := rekeySpoolWorkspace(old, id)
				workspaceID = id
				detail := fmt.Sprintf("normalized %q → %s (%s); manifest rewritten, credential renamed", old, id, name)
				if rekeyed > 0 {
					detail += fmt.Sprintf(", %d spooled capture(s) rekeyed", rekeyed)
				}
				add("workspace", "fixed", detail)
			}
		}
	} else {
		add("workspace", "ok", workspaceID)
	}

	// 3. Credential: re-mint when missing; swap a write-scope fallback for a
	// least-privilege capture key once the backend accepts the capture scope.
	if isUUID(workspaceID) {
		cred, _ := capture.LoadCredential(workspaceID)
		switch {
		case cred != nil && cred.Scope == capture.ScopeWrite && caller != nil:
			status := mintAndStoreCredential(cmd, caller, workspaceID)
			if status == credSwapped {
				add("credential", "fixed", "swapped the write-scope fallback for a capture-scoped key (revoke the old key: promptvm apikeys revoke <id>)")
			} else {
				path, _ := capture.CredentialPath(workspaceID)
				add("credential", "ok", path+" (write-scope fallback still in place — the backend does not accept the capture scope yet)")
			}
		case cred != nil:
			path, _ := capture.CredentialPath(workspaceID)
			add("credential", "ok", path)
		case caller == nil:
			add("credential", "failed", "missing and the CLI is not authenticated to mint one — run `promptvm auth login`")
		default:
			status := mintAndStoreCredential(cmd, caller, workspaceID)
			if credCheckmark(status) == "✓" {
				add("credential", "fixed", "re-minted: "+status)
			} else {
				add("credential", "failed", status)
			}
		}
	} else {
		add("credential", "skipped", "workspace is not a UUID")
	}

	// 4. Hooks.
	resolved, _ := manifest.Resolve(repoRoot)
	checkHooks(cmd, add, repoRoot, resolved)

	// 5. Spool.
	checkSpool(cmd, add, "")

	return checks
}

// findWorkspaceManifest returns the most-specific manifest scope that sets a
// workspace (local → project → user), or the most-specific existing manifest
// when none names a workspace.
func findWorkspaceManifest(repoRoot string) (manifest.Scope, *manifest.Manifest, string) {
	var firstScope manifest.Scope
	var firstM *manifest.Manifest
	var firstPath string
	for _, sc := range []manifest.Scope{manifest.ScopeLocal, manifest.ScopeProject, manifest.ScopeUser} {
		path, err := manifest.Path(sc, repoRoot)
		if err != nil {
			continue
		}
		m, err := manifest.Read(path)
		if err != nil || m == nil {
			continue
		}
		if firstM == nil {
			firstScope, firstM, firstPath = sc, m, path
		}
		if m.Workspace != "" {
			return sc, m, path
		}
	}
	return firstScope, firstM, firstPath
}

// renameCredentialFile moves <old>.env → <uuid>.env, best-effort. When both
// exist the UUID file wins and the old one is removed.
func renameCredentialFile(oldWorkspace, newWorkspace string) {
	oldPath, err1 := capture.CredentialPath(oldWorkspace)
	newPath, err2 := capture.CredentialPath(newWorkspace)
	if err1 != nil || err2 != nil || oldPath == newPath {
		return
	}
	if _, err := os.Stat(oldPath); err != nil {
		return
	}
	if _, err := os.Stat(newPath); err == nil {
		_ = os.Remove(oldPath)
		return
	}
	_ = os.Rename(oldPath, newPath)
}

// rekeySpoolWorkspace rewrites spool entries recorded under an old workspace
// identifier (a pre-normalization slug/name) so they carry the normalized UUID
// in both the entry and its payload. Entries overwrite in place (the spool
// file name keys on session + content hash, and the content hash excludes the
// workspace). Returns the number of entries rekeyed.
func rekeySpoolWorkspace(oldWorkspace, newWorkspace string) int {
	if oldWorkspace == newWorkspace {
		return 0
	}
	entries, err := spool.List()
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.WorkspaceID != oldWorkspace {
			continue
		}
		e.WorkspaceID = newWorkspace
		if e.Payload != nil {
			e.Payload.WorkspaceID = newWorkspace
		}
		if _, err := spool.Add(e); err == nil {
			n++
		}
	}
	return n
}

// checkHooks verifies (and repairs) the installed capture hooks against the
// resolved capture events.
func checkHooks(cmd *cobra.Command, add func(name, status, detail string), repoRoot string, resolved *manifest.Resolved) {
	events := manifest.DefaultEvents
	if resolved != nil && len(resolved.Events) > 0 {
		events = resolved.Events
	}
	want := withSessionStart(events)

	installed := installedCaptureEvents(repoRoot)
	missing := []string{}
	have := map[string]bool{}
	for _, e := range installed {
		have[e] = true
	}
	for _, e := range want {
		if !have[e] {
			missing = append(missing, e)
		}
	}
	if len(missing) == 0 {
		add("hooks", "ok", "installed on "+strings.Join(installed, ", "))
		return
	}

	// Reinstall at project scope when in a repo, else user scope.
	scope := hooks.ScopeUser
	if repoRoot != "" {
		scope = hooks.ScopeProject
	}
	settingsPath, err := hooks.SettingsFilePathAt(scope, repoRoot)
	if err != nil {
		add("hooks", "failed", err.Error())
		return
	}
	settings, err := hooks.ReadSettings(settingsPath)
	if err != nil {
		add("hooks", "failed", err.Error())
		return
	}
	settings.MergeHook(hooks.BuildCaptureFragment(want), hooks.CaptureHookSlug, true)
	if err := settings.Write(settingsPath); err != nil {
		add("hooks", "failed", err.Error())
		return
	}
	add("hooks", "fixed", fmt.Sprintf("reinstalled %s in %s", strings.Join(missing, ", "), settingsPath))
}

// checkSpool flushes pending captures (all workspaces when workspaceID is "").
func checkSpool(cmd *cobra.Command, add func(name, status, detail string), workspaceID string) {
	flushed, remaining := flushSpoolForWorkspace(cmd, workspaceID)
	switch {
	case flushed == 0 && remaining == 0:
		add("spool", "ok", "empty")
	case remaining == 0:
		add("spool", "fixed", fmt.Sprintf("flushed %d pending capture(s)", flushed))
	default:
		add("spool", "failed", fmt.Sprintf("flushed %d, %d still pending (missing credential or upload errors)", flushed, remaining))
	}
}
