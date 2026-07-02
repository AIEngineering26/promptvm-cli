package cmd

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/AIEngineering26/promptvm-cli/internal/capture"
	"github.com/AIEngineering26/promptvm-cli/internal/gitutil"
	"github.com/AIEngineering26/promptvm-cli/internal/hooks"
	"github.com/AIEngineering26/promptvm-cli/internal/manifest"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/AIEngineering26/promptvm-cli/internal/spool"
	"github.com/spf13/cobra"
)

// manifestFileStatus reports one manifest tier consulted during resolution.
type manifestFileStatus struct {
	Scope string `json:"scope"`
	Path  string `json:"path"`
	Found bool   `json:"found"`
}

// syncStatus is the JSON shape for `sync status -o json`.
type syncStatus struct {
	Workspace      string               `json:"workspace"`
	Directory      string               `json:"directory"`
	Enabled        bool                 `json:"enabled"`
	Mode           string               `json:"mode"`
	Events         []string             `json:"events"`
	Governance     string               `json:"governance"`
	InstalledHooks []string             `json:"installedHooks"`
	PendingSpool   int                  `json:"pendingSpool"`
	LastSyncAt     string               `json:"lastSyncAt,omitempty"`
	CredentialSet  bool                 `json:"credentialSet"`
	ManifestFiles  []manifestFileStatus `json:"manifestFiles"`
	CredentialPath string               `json:"credentialPath,omitempty"`
	Next           string               `json:"next,omitempty"`
}

func newSyncStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show resolved Context Sync config, target, pending spool, installed hooks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot := ""
			if repo, ok := gitutil.Detect(""); ok {
				repoRoot = repo.Root
			}
			resolved, err := manifest.Resolve(repoRoot)
			if err != nil {
				return err
			}

			pending, _ := spool.Count()
			installed := installedCaptureEvents(repoRoot)
			lastSync := lastSyncAt()
			manifestFiles := manifestFilesConsulted(repoRoot)

			credPath := ""
			if resolved.Workspace != "" {
				credPath, _ = capture.CredentialPath(resolved.Workspace)
			}

			st := syncStatus{
				Workspace:      resolved.Workspace,
				Directory:      resolved.Directory,
				Enabled:        resolved.Enabled,
				Mode:           resolved.Mode,
				Events:         resolved.Events,
				Governance:     resolved.Governance,
				InstalledHooks: installed,
				PendingSpool:   pending,
				LastSyncAt:     lastSync,
				CredentialSet:  credentialPresent(resolved.Workspace),
				ManifestFiles:  manifestFiles,
				CredentialPath: credPath,
			}
			st.Next = nextHint(st)

			return output.Print(cmd, st, func(w io.Writer) error {
				fmt.Fprintln(w, "Context Sync status")
				fmt.Fprintf(w, "  workspace:       %s\n", orDash(st.Workspace))
				fmt.Fprintf(w, "  directory:       %s\n", orDash(st.Directory))
				fmt.Fprintf(w, "  enabled:         %t\n", st.Enabled)
				fmt.Fprintf(w, "  mode:            %s\n", st.Mode)
				fmt.Fprintf(w, "  capture events:  %s\n", strings.Join(st.Events, ", "))
				fmt.Fprintf(w, "  governance:      %s\n", st.Governance)
				fmt.Fprintln(w, "  manifests:")
				for _, mf := range st.ManifestFiles {
					fmt.Fprintf(w, "    %-8s %s (%s)\n", mf.Scope+":", mf.Path, foundWord(mf.Found))
				}
				fmt.Fprintf(w, "  installed hooks: %s\n", orDash(strings.Join(st.InstalledHooks, ", ")))
				fmt.Fprintf(w, "  pending spool:   %d\n", st.PendingSpool)
				fmt.Fprintf(w, "  credential:      %s\n", boolWord(st.CredentialSet, "stored", "missing"))
				if st.CredentialPath != "" {
					fmt.Fprintf(w, "  credential file: %s (%s)\n", st.CredentialPath, foundWord(st.CredentialSet))
				}
				if st.LastSyncAt != "" {
					fmt.Fprintf(w, "  last sync:       %s\n", st.LastSyncAt)
				}
				if st.Next != "" {
					fmt.Fprintf(w, "\nNext: %s\n", st.Next)
				}
				return nil
			})
		},
	}
	return cmd
}

// manifestFilesConsulted lists the three manifest tiers (user → project →
// local) with their on-disk presence, mirroring manifest.Resolve.
func manifestFilesConsulted(repoRoot string) []manifestFileStatus {
	out := make([]manifestFileStatus, 0, 3)
	for _, sc := range []manifest.Scope{manifest.ScopeUser, manifest.ScopeProject, manifest.ScopeLocal} {
		path, err := manifest.Path(sc, repoRoot)
		if err != nil {
			continue
		}
		_, statErr := os.Stat(path)
		out = append(out, manifestFileStatus{Scope: string(sc), Path: path, Found: statErr == nil})
	}
	return out
}

// nextHint derives a single state-specific next step.
func nextHint(st syncStatus) string {
	anyManifest := false
	for _, mf := range st.ManifestFiles {
		if mf.Found {
			anyManifest = true
		}
	}
	switch {
	case !anyManifest || st.Workspace == "":
		return "Context Sync has not been set up here — run `promptvm sync init` (or `promptvm setup` for the full onboarding)."
	case !st.CredentialSet:
		return "No capture credential is stored, so captures spool locally — run `promptvm sync doctor` to mint one and flush the spool."
	case st.PendingSpool > 0:
		return fmt.Sprintf("%d capture(s) are spooled for workspace %s — run `promptvm sync doctor` to flush them.", st.PendingSpool, st.Workspace)
	case len(st.InstalledHooks) == 0:
		return "No capture hooks are installed — run `promptvm sync init` to install them."
	default:
		return ""
	}
}

func foundWord(found bool) string {
	if found {
		return "found"
	}
	return "absent"
}

// installedCaptureEvents reads the capture-hook events across all scopes,
// anchoring project/local settings at the repo root when available.
func installedCaptureEvents(repoRoot string) []string {
	set := map[string]bool{}
	for _, sc := range []hooks.Scope{hooks.ScopeLocal, hooks.ScopeProject, hooks.ScopeUser} {
		path, err := hooks.SettingsFilePathAt(sc, repoRoot)
		if err != nil {
			continue
		}
		s, err := hooks.ReadSettings(path)
		if err != nil {
			continue
		}
		for _, e := range s.CaptureEventsInstalled() {
			set[e] = true
		}
	}
	out := make([]string, 0, len(set))
	for e := range set {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}

// lastSyncAt returns the most recent captured-session timestamp from the ledger.
func lastSyncAt() string {
	led, err := spool.LoadLedger()
	if err != nil {
		return ""
	}
	var newest time.Time
	for _, t := range led.Captured {
		if t.After(newest) {
			newest = t
		}
	}
	if newest.IsZero() {
		return ""
	}
	return newest.Format(time.RFC3339)
}

func credentialPresent(workspace string) bool {
	if workspace == "" {
		return false
	}
	cred, err := loadCaptureCredential(workspace)
	return err == nil && cred
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func boolWord(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}
