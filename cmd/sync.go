package cmd

import (
	"github.com/spf13/cobra"
)

// syncCmd is the parent for the context-sync command group. The group is named
// "sync" (DX-1) — NOT "context"/"contexts" — to avoid colliding with the
// existing `contexts` command and the one-keystroke ambiguity between them.
var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Automatic Claude Code session-context capture (Context Sync)",
	Long: `Context Sync captures what happened in a Claude Code session and uploads a
distilled, redacted context artifact into the right PromptVM workspace — with no
glue work per session.

  promptvm sync init     Set up hooks + manifest + a capture credential for this repo
  promptvm sync run      Hook-invoked uploader (reads the event from stdin, self-detaches)
  promptvm sync status   Show resolved config, target, last sync, pending spool
  promptvm sync doctor   Diagnose + repair (workspace UUID, credential, hooks, spool)
  promptvm sync push     Manually capture a session now (no hook required)
  promptvm sync export    Refresh the local context block with promoted captures

Setup writes a hierarchical manifest (.promptvm/config.json project,
.promptvm/config.local.json local, or a global default) and a command hook into
Claude Code's settings.json. Capture is opt-in, redaction is on by default, and
nothing becomes canonical without server-side governance.`,
}

func init() {
	rootCmd.AddCommand(syncCmd)
	syncCmd.AddCommand(newSyncInitCmd())
	syncCmd.AddCommand(newSyncRunCmd())
	syncCmd.AddCommand(newSyncStatusCmd())
	syncCmd.AddCommand(newSyncDoctorCmd())
	syncCmd.AddCommand(newSyncPushCmd())
	syncCmd.AddCommand(newSyncExportCmd())
}
