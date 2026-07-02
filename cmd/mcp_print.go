package cmd

import (
	"fmt"
	"io"

	"github.com/AIEngineering26/promptvm-cli/internal/mcpsetup"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/spf13/cobra"
)

// mcpSnippet is one per-client config snippet.
type mcpSnippet struct {
	Target  string `json:"target"`
	PasteAt string `json:"pasteAt"`
	Snippet string `json:"snippet"`
}

func newMCPPrintCmd() *cobra.Command {
	var (
		target string
		mcpURL string
	)
	cmd := &cobra.Command{
		Use:   "print",
		Short: "Print per-client MCP config snippets (nothing is written)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if target != "claude" && target != "codex" && target != "all" {
				return fmt.Errorf("invalid --target %q: must be claude|codex|all", target)
			}
			endpoint, err := resolveMCPEndpoint(cmd, mcpURL)
			if err != nil {
				return err
			}

			var snippets []mcpSnippet
			if target == "claude" || target == "all" {
				snippets = append(snippets, mcpSnippet{
					Target:  "claude",
					PasteAt: "Run this command in your terminal.",
					Snippet: mcpsetup.ClaudeSnippet(endpoint),
				})
			}
			if target == "codex" || target == "all" {
				snippet := mcpsetup.CodexSnippet(endpoint, nil)
				if pub, sec := activeAPIKeyPair(); pub != "" && sec != "" {
					snippet = mcpsetup.CodexSnippet(endpoint, map[string]string{
						"X-PromptVM-Public-Key": pub,
						"X-PromptVM-Secret-Key": sec,
					})
				}
				snippets = append(snippets, mcpSnippet{
					Target:  "codex",
					PasteAt: "~/.codex/config.toml",
					Snippet: snippet,
				})
			}

			return output.Print(cmd, snippets, func(w io.Writer) error {
				for i, s := range snippets {
					if i > 0 {
						fmt.Fprintln(w)
					}
					fmt.Fprintf(w, "# %s — %s\n%s\n", s.Target, s.PasteAt, s.Snippet)
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&target, "target", "all", "Target client: claude|codex|all")
	cmd.Flags().StringVar(&mcpURL, "mcp-url", "", "MCP endpoint override (default: derived from the API base URL; env PROMPTVM_MCP_URL)")
	return cmd
}
