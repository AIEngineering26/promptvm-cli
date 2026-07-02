package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/AIEngineering26/promptvm-cli/internal/api"
	"github.com/AIEngineering26/promptvm-cli/internal/gitutil"
	"github.com/AIEngineering26/promptvm-cli/internal/mcpsetup"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/spf13/cobra"
)

// mcpInstallOptions carries the install knobs so `promptvm setup` can reuse
// the flow programmatically.
type mcpInstallOptions struct {
	Target string // claude | codex | all
	Scope  string // user | project
	MCPURL string
	DryRun bool
	// SkipUndetected skips targets whose client is not present locally
	// (claude binary / ~/.codex dir) instead of installing config for them.
	SkipUndetected bool
}

// mcpInstallResult reports what happened for one target.
type mcpInstallResult struct {
	Target string `json:"target"`
	Status string `json:"status"` // installed | skipped | dry-run | failed
	Detail string `json:"detail,omitempty"`
}

func newMCPInstallCmd() *cobra.Command {
	o := mcpInstallOptions{}
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Register the PromptVM MCP server with local AI clients",
		Long: `Registers the hosted PromptVM MCP server:

  claude  → runs ` + "`claude mcp add --transport http promptvm <mcp-url>`" + ` when the
            claude binary is on PATH; otherwise writes the server into the
            project's .mcp.json
  codex   → merges [mcp_servers.promptvm] into ~/.codex/config.toml (created if
            absent, existing content preserved). Auth headers reuse the active
            api-key profile's pk/sk pair; an OAuth-only login mints a
            scopes:["read","write"] key named "codex mcp" for the headers.

Use --dry-run to preview and ` + "`promptvm mcp print`" + ` to get copy-paste snippets.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			results, err := runMCPInstall(cmd, o)
			if err != nil {
				return err
			}
			return printMCPInstallResults(cmd, results)
		},
	}
	cmd.Flags().StringVar(&o.Target, "target", "all", "Target client: claude|codex|all")
	cmd.Flags().StringVar(&o.Scope, "scope", "project", "Scope for the Claude Code registration: user|project")
	cmd.Flags().StringVar(&o.MCPURL, "mcp-url", "", "MCP endpoint override (default: derived from the API base URL; env PROMPTVM_MCP_URL)")
	cmd.Flags().BoolVar(&o.DryRun, "dry-run", false, "Preview changes without writing")
	return cmd
}

func printMCPInstallResults(cmd *cobra.Command, results []mcpInstallResult) error {
	return output.Print(cmd, results, func(w io.Writer) error {
		for _, r := range results {
			glyph := "✓"
			if r.Status == "failed" {
				glyph = "✗"
			} else if r.Status == "skipped" {
				glyph = "-"
			}
			fmt.Fprintf(w, "%s %s: %s — %s\n", glyph, r.Target, r.Status, r.Detail)
		}
		return nil
	})
}

// lookPathFunc is indirected so tests can fake client-binary detection.
var lookPathFunc = exec.LookPath

// codexHomeDir returns ~/.codex (honoring CODEX_HOME when absolute).
func codexHomeDir() string {
	if ch := strings.TrimSpace(os.Getenv("CODEX_HOME")); ch != "" && filepath.IsAbs(ch) {
		return ch
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

// runMCPInstall installs the MCP server registration for the selected targets.
func runMCPInstall(cmd *cobra.Command, o mcpInstallOptions) ([]mcpInstallResult, error) {
	if o.Target != "claude" && o.Target != "codex" && o.Target != "all" {
		return nil, fmt.Errorf("invalid --target %q: must be claude|codex|all", o.Target)
	}
	if o.Scope != "user" && o.Scope != "project" {
		return nil, fmt.Errorf("invalid --scope %q: must be user|project", o.Scope)
	}
	endpoint, err := resolveMCPEndpoint(cmd, o.MCPURL)
	if err != nil {
		return nil, err
	}

	var results []mcpInstallResult
	if o.Target == "claude" || o.Target == "all" {
		results = append(results, installMCPClaude(cmd, o, endpoint))
	}
	if o.Target == "codex" || o.Target == "all" {
		results = append(results, installMCPCodex(cmd, o, endpoint))
	}
	return results, nil
}

// installMCPClaude registers the server with Claude Code: via the `claude`
// binary when available, else by writing the project's .mcp.json.
func installMCPClaude(cmd *cobra.Command, o mcpInstallOptions, endpoint string) mcpInstallResult {
	res := mcpInstallResult{Target: "claude"}

	claudeBin, lookErr := lookPathFunc("claude")
	if lookErr != nil && o.SkipUndetected {
		res.Status = "skipped"
		res.Detail = "claude binary not found on PATH"
		return res
	}

	if lookErr == nil {
		args := mcpsetup.ClaudeAddCommand(endpoint, o.Scope == "user")
		if o.DryRun {
			res.Status = "dry-run"
			res.Detail = "would run: claude " + strings.Join(args, " ")
			return res
		}
		c := exec.Command(claudeBin, args...)
		out, err := c.CombinedOutput()
		if err != nil {
			// `claude mcp add` fails when the server is already registered —
			// treat that as success (idempotent install).
			if strings.Contains(strings.ToLower(string(out)), "already exists") {
				res.Status = "installed"
				res.Detail = "already registered (claude mcp)"
				return res
			}
			res.Status = "failed"
			res.Detail = fmt.Sprintf("claude %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
			return res
		}
		res.Status = "installed"
		res.Detail = "registered via `claude " + strings.Join(args, " ") + "`"
		return res
	}

	// No claude binary: write the project .mcp.json (Claude Code reads it at
	// the repo root). User scope has no file fallback — print the command.
	if o.Scope == "user" {
		res.Status = "failed"
		res.Detail = "claude binary not found; register manually with: " + mcpsetup.ClaudeSnippet(endpoint)
		return res
	}
	root := ""
	if repo, ok := gitutil.Detect(""); ok {
		root = repo.Root
	} else if cwd, err := os.Getwd(); err == nil {
		root = cwd
	}
	path := filepath.Join(root, ".mcp.json")
	if o.DryRun {
		res.Status = "dry-run"
		res.Detail = "would write " + path
		return res
	}
	if err := mergeMCPJSON(path, endpoint); err != nil {
		res.Status = "failed"
		res.Detail = err.Error()
		return res
	}
	res.Status = "installed"
	res.Detail = "wrote " + path + " (claude binary not found)"
	return res
}

// mergeMCPJSON merges mcpServers.promptvm into a .mcp.json file, preserving
// any other entries.
func mergeMCPJSON(path, endpoint string) error {
	doc := map[string]any{}
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
	}
	servers, _ := doc["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers["promptvm"] = map[string]any{"type": "http", "url": endpoint}
	doc["mcpServers"] = servers

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// installMCPCodex merges [mcp_servers.promptvm] into ~/.codex/config.toml.
func installMCPCodex(cmd *cobra.Command, o mcpInstallOptions, endpoint string) mcpInstallResult {
	res := mcpInstallResult{Target: "codex"}

	dir := codexHomeDir()
	if dir == "" {
		res.Status = "failed"
		res.Detail = "could not determine the Codex home directory"
		return res
	}
	if o.SkipUndetected {
		if _, err := os.Stat(dir); err != nil {
			res.Status = "skipped"
			res.Detail = dir + " not found (Codex not installed?)"
			return res
		}
	}
	path := filepath.Join(dir, "config.toml")

	headers, headerNote, ok := codexAuthHeaders(cmd, endpoint)
	if !ok {
		// Could not obtain credentials for the headers: print the manual
		// snippet instead of writing a config that cannot authenticate.
		res.Status = "failed"
		res.Detail = "could not obtain credentials for the Codex MCP headers; add this to " + path + " manually:\n\n" +
			mcpsetup.CodexSnippet(endpoint, map[string]string{
				"X-PromptVM-Public-Key": "<pk_…>", "X-PromptVM-Secret-Key": "<sk_…>",
			}) + "\n"
		return res
	}

	if o.DryRun {
		res.Status = "dry-run"
		res.Detail = "would merge [mcp_servers.promptvm] into " + path + headerNote
		return res
	}

	existing, _ := os.ReadFile(path)
	merged, err := mcpsetup.MergeCodexConfig(existing, endpoint, headers)
	if err != nil {
		res.Status = "failed"
		res.Detail = err.Error()
		return res
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		res.Status = "failed"
		res.Detail = err.Error()
		return res
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, merged, 0o600); err != nil {
		res.Status = "failed"
		res.Detail = err.Error()
		return res
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		res.Status = "failed"
		res.Detail = err.Error()
		return res
	}
	res.Status = "installed"
	res.Detail = "merged [mcp_servers.promptvm] into " + path + headerNote
	return res
}

// codexAuthHeaders builds the auth headers for the Codex MCP registration:
// the active api-key profile's pk/sk pair, or (for OAuth-only logins) a
// freshly minted scopes:["read","write"] key named "codex mcp".
func codexAuthHeaders(cmd *cobra.Command, endpoint string) (headers map[string]string, note string, ok bool) {
	if pub, sec := activeAPIKeyPair(); pub != "" && sec != "" {
		return map[string]string{
			"X-PromptVM-Public-Key": pub,
			"X-PromptVM-Secret-Key": sec,
		}, " (auth: active api-key profile)", true
	}

	caller, err := api.NewFromContext(cmd)
	if err != nil {
		return nil, "", false
	}
	pub, sec, err := mintReadWriteKey(caller, "codex mcp")
	if err != nil || pub == "" || sec == "" {
		return nil, "", false
	}
	fmt.Fprintln(cmd.ErrOrStderr(),
		"note: your login is OAuth-only, so a new API key named \"codex mcp\" (scopes: read, write) was minted for the Codex MCP headers.")
	return map[string]string{
		"X-PromptVM-Public-Key": pub,
		"X-PromptVM-Secret-Key": sec,
	}, " (auth: minted \"codex mcp\" key)", true
}
