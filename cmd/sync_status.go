package cmd

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/AIEngineering26/promptvm-cli/internal/gitutil"
	"github.com/AIEngineering26/promptvm-cli/internal/hooks"
	"github.com/AIEngineering26/promptvm-cli/internal/manifest"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/AIEngineering26/promptvm-cli/internal/spool"
	"github.com/spf13/cobra"
)

// syncStatus is the JSON shape for `sync status -o json`.
type syncStatus struct {
	Workspace      string   `json:"workspace"`
	Directory      string   `json:"directory"`
	Enabled        bool     `json:"enabled"`
	Mode           string   `json:"mode"`
	Events         []string `json:"events"`
	Governance     string   `json:"governance"`
	InstalledHooks []string `json:"installedHooks"`
	PendingSpool   int      `json:"pendingSpool"`
	LastSyncAt     string   `json:"lastSyncAt,omitempty"`
	CredentialSet  bool     `json:"credentialSet"`
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
			installed := installedCaptureEvents()
			lastSync := lastSyncAt()

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
			}

			return output.Print(cmd, st, func(w io.Writer) error {
				fmt.Fprintln(w, "Context Sync status")
				fmt.Fprintf(w, "  workspace:       %s\n", orDash(st.Workspace))
				fmt.Fprintf(w, "  directory:       %s\n", orDash(st.Directory))
				fmt.Fprintf(w, "  enabled:         %t\n", st.Enabled)
				fmt.Fprintf(w, "  mode:            %s\n", st.Mode)
				fmt.Fprintf(w, "  capture events:  %s\n", strings.Join(st.Events, ", "))
				fmt.Fprintf(w, "  governance:      %s\n", st.Governance)
				fmt.Fprintf(w, "  installed hooks: %s\n", orDash(strings.Join(st.InstalledHooks, ", ")))
				fmt.Fprintf(w, "  pending spool:   %d\n", st.PendingSpool)
				fmt.Fprintf(w, "  credential:      %s\n", boolWord(st.CredentialSet, "stored", "missing"))
				if st.LastSyncAt != "" {
					fmt.Fprintf(w, "  last sync:       %s\n", st.LastSyncAt)
				}
				return nil
			})
		},
	}
	return cmd
}

// installedCaptureEvents reads the capture-hook events across all scopes.
func installedCaptureEvents() []string {
	set := map[string]bool{}
	for _, sc := range []hooks.Scope{hooks.ScopeLocal, hooks.ScopeProject, hooks.ScopeUser} {
		path, err := hooks.SettingsFilePath(sc)
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
