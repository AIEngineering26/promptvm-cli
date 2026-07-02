// Package mcpsetup derives the hosted PromptVM MCP endpoint from the API base
// URL and writes/prints per-client MCP configuration (Claude Code, Codex CLI).
//
// The snippet formats are copied verbatim from the frontend's
// src/lib/mcp/client-snippets.ts so every surface (web app, CLI, docs) shows
// byte-identical configuration.
package mcpsetup

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// EnvMCPURL overrides the derived MCP endpoint.
const EnvMCPURL = "PROMPTVM_MCP_URL"

// DeriveMCPURL maps a PromptVM API base URL to its hosted MCP endpoint:
//
//	https://dev-api.promptvm.ai     → https://dev-mcp.promptvm.ai
//	https://staging-api.promptvm.ai → https://staging-mcp.promptvm.ai
//	https://api.promptvm.ai        → https://mcp.promptvm.ai
//
// Returns "" when no mapping applies (e.g. localhost) so callers can require
// an explicit --mcp-url / PROMPTVM_MCP_URL.
func DeriveMCPURL(apiBaseURL string) string {
	s := strings.TrimSpace(apiBaseURL)
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return ""
	}
	host := u.Host
	switch {
	case strings.HasPrefix(host, "dev-api."):
		host = "dev-mcp." + strings.TrimPrefix(host, "dev-api.")
	case strings.HasPrefix(host, "staging-api."):
		host = "staging-mcp." + strings.TrimPrefix(host, "staging-api.")
	case strings.HasPrefix(host, "api."):
		host = "mcp." + strings.TrimPrefix(host, "api.")
	default:
		return ""
	}
	return u.Scheme + "://" + host
}

// ResolveMCPURL resolves the MCP endpoint with the standard precedence:
// explicit flag value → PROMPTVM_MCP_URL env → derivation from the API base
// URL. Returns an error when nothing resolves.
func ResolveMCPURL(flagValue, apiBaseURL string) (string, error) {
	if v := strings.TrimSpace(flagValue); v != "" {
		return strings.TrimRight(v, "/"), nil
	}
	if v := strings.TrimSpace(os.Getenv(EnvMCPURL)); v != "" {
		return strings.TrimRight(v, "/"), nil
	}
	if derived := DeriveMCPURL(apiBaseURL); derived != "" {
		return derived, nil
	}
	return "", fmt.Errorf("could not derive an MCP endpoint from API base URL %q; pass --mcp-url or set %s", apiBaseURL, EnvMCPURL)
}

// ClaudeAddCommand renders the Claude Code CLI registration command
// (format: client-snippets.ts `claude-code` OAuth snippet).
func ClaudeAddCommand(endpoint string, userScope bool) []string {
	args := []string{"mcp", "add"}
	if userScope {
		args = append(args, "--scope", "user")
	}
	args = append(args, "--transport", "http", "promptvm", endpoint)
	return args
}

// ClaudeSnippet is the copy-paste form of the Claude Code registration command.
func ClaudeSnippet(endpoint string) string {
	return "claude mcp add --transport http promptvm " + endpoint
}

// ClaudeMCPJSON renders the project-level .mcp.json server entry used when the
// `claude` binary is not available (format: client-snippets.ts `generic-json`).
func ClaudeMCPJSON(endpoint string) string {
	return `{
  "mcpServers": {
    "promptvm": {
      "type": "http",
      "url": "` + endpoint + `"
    }
  }
}`
}

// CodexSnippet renders the ~/.codex/config.toml block (format:
// client-snippets.ts `codex-cli`). headers, when non-nil, are emitted as a
// `[mcp_servers.promptvm.headers]` table, matching the manual-token snippet
// shape.
func CodexSnippet(endpoint string, headers map[string]string) string {
	var b strings.Builder
	b.WriteString("[mcp_servers.promptvm]\n")
	b.WriteString("type = \"http\"\n")
	b.WriteString("url = \"" + endpoint + "\"")
	if len(headers) > 0 {
		b.WriteString("\n\n[mcp_servers.promptvm.headers]\n")
		keys := make([]string, 0, len(headers))
		for k := range headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(k + " = \"" + headers[k] + "\"")
		}
	}
	return b.String()
}

// MergeCodexConfig merges the promptvm MCP server entry into an existing
// ~/.codex/config.toml document (existing may be nil/empty for a fresh file).
// All other keys are preserved; an existing [mcp_servers.promptvm] table is
// replaced. headers, when non-nil, become [mcp_servers.promptvm.headers].
func MergeCodexConfig(existing []byte, endpoint string, headers map[string]string) ([]byte, error) {
	doc := map[string]any{}
	if len(existing) > 0 {
		if err := toml.Unmarshal(existing, &doc); err != nil {
			return nil, fmt.Errorf("parsing existing config.toml: %w", err)
		}
	}

	servers, _ := doc["mcp_servers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	entry := map[string]any{
		"type": "http",
		"url":  endpoint,
	}
	if len(headers) > 0 {
		h := map[string]any{}
		for k, v := range headers {
			h[k] = v
		}
		entry["headers"] = h
	}
	servers["promptvm"] = entry
	doc["mcp_servers"] = servers

	out, err := toml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("marshaling config.toml: %w", err)
	}
	return out, nil
}
