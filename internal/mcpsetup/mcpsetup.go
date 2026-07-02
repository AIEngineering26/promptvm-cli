// Package mcpsetup derives the hosted PromptVM MCP endpoint from the API base
// URL and writes/prints per-client MCP configuration (Claude Code, Codex CLI).
//
// The snippet formats follow the frontend's src/lib/mcp/client-snippets.ts so
// every surface (web app, CLI, docs) shows equivalent configuration; header
// auth uses the MCP server's direct pk/sk bearer envelope
// (`Authorization: Bearer pvm_mcp_pkv1_<base64url(pk:sk)>`, see the frontend's
// src/lib/mcp/pksk-encoder.ts and the MCP contracts' PKSK_BEARER_PREFIX).
package mcpsetup

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// EnvMCPURL overrides the derived MCP endpoint.
const EnvMCPURL = "PROMPTVM_MCP_URL"

// PKSKBearerPrefix is the MCP server's direct pk/sk bearer envelope prefix
// (cross-repo contract: mcp packages/contracts PKSK_BEARER_PREFIX, frontend
// src/lib/mcp/pksk-encoder.ts).
const PKSKBearerPrefix = "pvm_mcp_pkv1_"

// PkSkAuthorization packs an API key pair into the only header form the hosted
// MCP server authenticates: "Bearer pvm_mcp_pkv1_<base64url(pk:sk)>"
// (base64url, no padding — the server tolerates padding but the contract
// encoder omits it).
func PkSkAuthorization(publicKey, secretKey string) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(publicKey + ":" + secretKey))
	return "Bearer " + PKSKBearerPrefix + payload
}

// DeriveMCPURL maps a PromptVM API base URL to its hosted MCP endpoint. The
// hosted server serves the streamable-HTTP protocol only at the /mcp path
// (everything else 404s), so the derived URL always carries it:
//
//	https://dev-api.promptvm.ai     → https://dev-mcp.promptvm.ai/mcp
//	https://staging-api.promptvm.ai → https://staging-mcp.promptvm.ai/mcp
//	https://api.promptvm.ai        → https://mcp.promptvm.ai/mcp
//
// Returns "" when no mapping applies (e.g. localhost) so callers can require
// an explicit --mcp-url / PROMPTVM_MCP_URL (documented as the full endpoint,
// path included).
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
	return u.Scheme + "://" + host + "/mcp"
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
// (format: client-snippets.ts `claude-code` OAuth snippet). scope is passed
// through explicitly — `claude mcp add` defaults to its private "local" scope,
// so omitting --scope for project installs would NOT register the shared,
// committed project scope the .mcp.json fallback writes.
func ClaudeAddCommand(endpoint, scope string) []string {
	args := []string{"mcp", "add"}
	if scope == "user" || scope == "project" {
		args = append(args, "--scope", scope)
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
// The merge is textual — comments, blank lines, key ordering, and formatting
// of all other content are preserved byte-for-byte. An existing
// [mcp_servers.promptvm] table (and its subtables) is removed and the fresh
// snippet is appended. headers, when non-nil, become
// [mcp_servers.promptvm.headers].
func MergeCodexConfig(existing []byte, endpoint string, headers map[string]string) ([]byte, error) {
	if len(existing) > 0 {
		doc := map[string]any{}
		if err := toml.Unmarshal(existing, &doc); err != nil {
			return nil, fmt.Errorf("parsing existing config.toml: %w", err)
		}
	}

	var b strings.Builder
	if kept := removePromptvmTables(existing); len(kept) > 0 {
		b.Write(kept)
		b.WriteString("\n") // blank line between existing content and the entry
	}
	b.WriteString(CodexSnippet(endpoint, headers))
	b.WriteString("\n")
	out := []byte(b.String())

	// Sanity: the merged document must still parse (e.g. a promptvm table we
	// failed to strip would surface here as a duplicate-table error).
	doc := map[string]any{}
	if err := toml.Unmarshal(out, &doc); err != nil {
		return nil, fmt.Errorf("merged config.toml is invalid: %w", err)
	}
	return out, nil
}

// ExistingCodexAuthorization returns the Authorization header of an existing
// [mcp_servers.promptvm.headers] table when it already carries a usable MCP
// pk/sk bearer ("Bearer pvm_mcp_pkv1_…"), or "" otherwise. Used to keep an
// already-working credential across re-runs instead of minting a new key.
func ExistingCodexAuthorization(existing []byte) string {
	if len(existing) == 0 {
		return ""
	}
	doc := map[string]any{}
	if err := toml.Unmarshal(existing, &doc); err != nil {
		return ""
	}
	servers, _ := doc["mcp_servers"].(map[string]any)
	entry, _ := servers["promptvm"].(map[string]any)
	headers, _ := entry["headers"].(map[string]any)
	auth, _ := headers["Authorization"].(string)
	if strings.HasPrefix(auth, "Bearer "+PKSKBearerPrefix) {
		return auth
	}
	return ""
}

// removePromptvmTables strips the [mcp_servers.promptvm] table and its
// subtables (e.g. [mcp_servers.promptvm.headers]) from the raw TOML text,
// preserving every other line verbatim. Lines belonging to a stripped table
// (up to the next table header) are dropped with it.
func removePromptvmTables(src []byte) []byte {
	if len(src) == 0 {
		return nil
	}
	lines := strings.Split(string(src), "\n")
	out := make([]string, 0, len(lines))
	skipping := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			skipping = isPromptvmTableHeader(t)
		}
		if skipping {
			continue
		}
		out = append(out, line)
	}
	// Trim trailing blank lines so the appended snippet sits one blank line
	// below the remaining content.
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	if len(out) == 0 {
		return nil
	}
	return []byte(strings.Join(out, "\n") + "\n")
}

// isPromptvmTableHeader reports whether a trimmed line is the table header for
// mcp_servers.promptvm or one of its subtables, tolerating whitespace, quoted
// keys, array-of-table syntax, and trailing comments.
func isPromptvmTableHeader(t string) bool {
	if !strings.HasPrefix(t, "[") {
		return false
	}
	end := strings.Index(t, "]")
	if end < 0 {
		return false
	}
	name := strings.Trim(t[1:end], "[] \t")
	name = strings.ReplaceAll(name, " ", "")
	name = strings.ReplaceAll(name, `"`, "")
	name = strings.ReplaceAll(name, "'", "")
	return name == "mcp_servers.promptvm" || strings.HasPrefix(name, "mcp_servers.promptvm.")
}
