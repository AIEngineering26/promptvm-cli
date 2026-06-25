package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"
)

func newSyncPushCmd() *cobra.Command {
	var (
		last      bool
		sessionID string
		mode      string
		workspace string
		dryRun    bool
	)

	cmd := &cobra.Command{
		Use:   "push",
		Short: "Manually capture a session now (no hook required)",
		Long: `Captures a session transcript on demand — for users who don't run hooks or to
backfill. Use --last for the most recent session or --session <id> for a named
one. Runs inline (no self-detach) and reports the result.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !last && sessionID == "" {
				return fmt.Errorf("specify --last or --session <id>")
			}

			path, sid, err := resolveTranscript(sessionID, last)
			if err != nil {
				return err
			}

			cwd, _ := os.Getwd()
			in := HookInput{
				SessionID:      sid,
				TranscriptPath: path,
				Cwd:            cwd,
				HookEventName:  "SessionEnd",
				Reason:         "manual-push",
			}
			// Inline (no detach); processHook spools on failure and never errors.
			processHook(cmd, in, mode, workspace, dryRun)
			if !dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "Captured session %s (or spooled if offline). Check `promptvm sync status`.\n", sid)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&last, "last", false, "Capture the most recent session transcript")
	cmd.Flags().StringVar(&sessionID, "session", "", "Capture a specific session id")
	cmd.Flags().StringVar(&mode, "mode", "", "Override capture mode: summary|metadata|transcript")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Override target workspace")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Build the payload but do not upload")
	return cmd
}

// resolveTranscript finds a transcript file by session id or the most recent
// one, scanning ~/.claude/projects/*/*.jsonl.
func resolveTranscript(sessionID string, last bool) (path, sid string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	files, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", "*.jsonl"))
	if len(files) == 0 {
		return "", "", fmt.Errorf("no Claude Code transcripts found under ~/.claude/projects")
	}

	if sessionID != "" {
		for _, f := range files {
			if id := transcriptSessionID(f); id == sessionID {
				return f, sessionID, nil
			}
		}
		return "", "", fmt.Errorf("no transcript found for session %q", sessionID)
	}

	// --last: newest by mtime.
	sort.Slice(files, func(i, j int) bool {
		return fileModTime(files[i]).After(fileModTime(files[j]))
	})
	newest := files[0]
	return newest, transcriptSessionID(newest), nil
}

func transcriptSessionID(path string) string {
	base := filepath.Base(path)
	return base[:len(base)-len(filepath.Ext(base))]
}

func fileModTime(path string) time.Time {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}
