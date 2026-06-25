package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/AIEngineering26/promptvm-cli/internal/api"
	"github.com/AIEngineering26/promptvm-cli/internal/ctxblock"
	"github.com/AIEngineering26/promptvm-cli/internal/gitutil"
	"github.com/AIEngineering26/promptvm-cli/internal/manifest"
	"github.com/spf13/cobra"
)

// captureListItem is the minimal shape of an inbox capture used for export.
type captureListItem struct {
	ID        string `json:"id"`
	Summary   string `json:"summary"`
	RepoURL   string `json:"repoUrl"`
	Branch    string `json:"branch"`
	CreatedAt string `json:"createdAt"`
}

func newSyncExportCmd() *cobra.Command {
	var (
		workspace string
		file      string
		limit     int
		dryRun    bool
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Refresh the local context block with the latest promoted captures",
		Long: `Writes a managed, fenced block of recently promoted captures into a project
context file (CLAUDE.md by default, or .promptvm/context.md) so the next session
benefits. The block is replaced in place — never duplicated (CEO-1). This is the
v1 payoff that closes the capture→review→reuse loop without Phase-2 retrieval.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot := ""
			if repo, ok := gitutil.Detect(""); ok {
				repoRoot = repo.Root
			}
			resolved, _ := manifest.Resolve(repoRoot)

			caller, err := api.NewFromContext(cmd)
			if err != nil {
				return fmt.Errorf("not authenticated: %w (run `promptvm auth login`)", err)
			}

			ws := workspace
			if ws == "" && resolved != nil {
				ws = resolved.Workspace
			}
			if ws == "" {
				if ws, err = resolveSyncWorkspace(cmd, caller); err != nil {
					return err
				}
			}

			// Pull promoted/published captures from the inbox endpoint.
			path := fmt.Sprintf("/api/v1/contexts/sessions?workspaceId=%s&status=promoted", ws)
			var resp struct {
				Data []captureListItem `json:"data"`
			}
			if err := caller.Get(path, &resp); err != nil {
				return fmt.Errorf("fetching promoted captures: %w", err)
			}

			lines := summaryLines(resp.Data, limit)
			block := ctxblock.Render(lines)

			target := file
			if target == "" {
				target = defaultContextFile(repoRoot)
			}

			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "[dry-run] would write %d capture(s) into %s:\n\n%s\n", len(lines), target, block)
				return nil
			}

			replaced, err := ctxblock.Upsert(target, block)
			if err != nil {
				return err
			}
			action := "added"
			if replaced {
				action = "refreshed"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Context block %s in %s (%d capture(s)).\n", action, target, len(lines))
			return nil
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "Source workspace (defaults to the manifest workspace)")
	cmd.Flags().StringVar(&file, "file", "", "Target context file (default CLAUDE.md at repo root)")
	cmd.Flags().IntVar(&limit, "limit", 10, "Max captures to include")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview the block without writing")
	return cmd
}

func summaryLines(items []captureListItem, limit int) []string {
	out := make([]string, 0, len(items))
	for i, it := range items {
		if limit > 0 && i >= limit {
			break
		}
		s := strings.TrimSpace(it.Summary)
		if s == "" {
			continue
		}
		// One-line each; collapse internal newlines.
		s = strings.Join(strings.Fields(s), " ")
		if it.Branch != "" {
			s = fmt.Sprintf("[%s] %s", it.Branch, s)
		}
		out = append(out, s)
	}
	return out
}

// defaultContextFile prefers CLAUDE.md at the repo root, else .promptvm/context.md.
func defaultContextFile(repoRoot string) string {
	if repoRoot == "" {
		return filepath.Join(".promptvm", "context.md")
	}
	return filepath.Join(repoRoot, "CLAUDE.md")
}
