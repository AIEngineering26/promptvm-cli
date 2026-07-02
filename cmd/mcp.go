package cmd

import (
	"os"

	"github.com/AIEngineering26/promptvm-cli/internal/api"
	"github.com/AIEngineering26/promptvm-cli/internal/config"
	"github.com/AIEngineering26/promptvm-cli/internal/mcpsetup"
	"github.com/spf13/cobra"
)

// mcpCmd is the parent for MCP client-registration commands.
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Connect AI clients (Claude Code, Codex) to the hosted PromptVM MCP server",
	Long: `Registers the hosted PromptVM MCP server with local AI clients so agents can
drive PromptVM through promptvm_* tools:

  promptvm mcp install   Write the MCP server into client configs (claude, codex)
  promptvm mcp print     Print the per-client config snippets without writing

The MCP endpoint derives from the API base URL (dev-api.promptvm.ai →
dev-mcp.promptvm.ai, api.promptvm.ai → mcp.promptvm.ai) and can be overridden
with --mcp-url or PROMPTVM_MCP_URL.`,
}

func init() {
	rootCmd.AddCommand(mcpCmd)
	mcpCmd.AddCommand(newMCPInstallCmd())
	mcpCmd.AddCommand(newMCPPrintCmd())
}

// resolveMCPEndpoint resolves the MCP URL with the standard precedence:
// --mcp-url flag → PROMPTVM_MCP_URL → derived from the resolved API base URL.
func resolveMCPEndpoint(cmd *cobra.Command, mcpURLFlag string) (string, error) {
	base := resolveFlagOrProfileBaseURL(cmd)
	return mcpsetup.ResolveMCPURL(mcpURLFlag, base)
}

// resolveFlagOrProfileBaseURL mirrors api.NewFromContext's base-URL precedence
// without requiring credentials: --base-url flag → PROMPTVM_BASE_URL env →
// active profile baseUrl → default.
func resolveFlagOrProfileBaseURL(cmd *cobra.Command) string {
	if v, _ := cmd.Flags().GetString("base-url"); v != "" {
		return v
	}
	if v, _ := cmd.Root().PersistentFlags().GetString("base-url"); v != "" {
		return v
	}
	if v := os.Getenv("PROMPTVM_BASE_URL"); v != "" {
		return v
	}
	if cfg, err := config.Load(); err == nil {
		if p, err := cfg.ActiveProfileData(); err == nil && p.BaseURL != "" {
			return p.BaseURL
		}
	}
	return "https://dev-api.promptvm.ai"
}

// activeAPIKeyPair returns the active profile's pk/sk pair when it is an
// api-key profile, or ("", "") for OAuth-only / no profile.
func activeAPIKeyPair() (pub, sec string) {
	cfg, err := config.Load()
	if err != nil {
		return "", ""
	}
	p, err := cfg.ActiveProfileData()
	if err != nil || p == nil || p.IsOAuth() {
		return "", ""
	}
	return p.PublicKey, p.SecretKey
}

// mintReadWriteKey mints a scopes:["read","write"] API key (used for the Codex
// MCP headers when the active profile is OAuth-only and holds no pk/sk pair).
func mintReadWriteKey(caller *api.Caller, name string) (pub, sec string, err error) {
	return mintAPIKey(caller, name, []string{"read", "write"}, "")
}
