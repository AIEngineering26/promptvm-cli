package mcpsetup

import (
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

// TestDeriveMCPURL is the cross-repo contract: dev-api → dev-mcp,
// staging-api → staging-mcp, api → mcp, everything else underivable.
func TestDeriveMCPURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://dev-api.promptvm.ai", "https://dev-mcp.promptvm.ai"},
		{"https://api.promptvm.ai", "https://mcp.promptvm.ai"},
		{"https://staging-api.promptvm.ai", "https://staging-mcp.promptvm.ai"},
		{"https://dev-api.promptvm.com", "https://dev-mcp.promptvm.com"},
		{"http://localhost:3000", ""},
		{"https://example.com", ""},
		{"", ""},
		{"not a url", ""},
	}
	for _, c := range cases {
		if got := DeriveMCPURL(c.in); got != c.want {
			t.Errorf("DeriveMCPURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestResolveMCPURLPrecedence(t *testing.T) {
	// Flag wins over everything.
	got, err := ResolveMCPURL("https://custom-mcp.example.com/", "https://api.promptvm.ai")
	if err != nil || got != "https://custom-mcp.example.com" {
		t.Errorf("flag override = %q, %v", got, err)
	}

	// Env wins over derivation.
	t.Setenv(EnvMCPURL, "https://env-mcp.example.com")
	got, err = ResolveMCPURL("", "https://api.promptvm.ai")
	if err != nil || got != "https://env-mcp.example.com" {
		t.Errorf("env override = %q, %v", got, err)
	}
	t.Setenv(EnvMCPURL, "")

	// Derivation.
	got, err = ResolveMCPURL("", "https://dev-api.promptvm.ai")
	if err != nil || got != "https://dev-mcp.promptvm.ai" {
		t.Errorf("derived = %q, %v", got, err)
	}

	// Underivable with no override → error.
	if _, err = ResolveMCPURL("", "http://localhost:3000"); err == nil {
		t.Error("expected error for underivable base URL")
	}
}

// TestCodexSnippetFormat pins the ~/.codex/config.toml snippet to the
// client-snippets.ts codex-cli format.
func TestCodexSnippetFormat(t *testing.T) {
	got := CodexSnippet("https://dev-mcp.promptvm.ai", nil)
	want := "[mcp_servers.promptvm]\ntype = \"http\"\nurl = \"https://dev-mcp.promptvm.ai\""
	if got != want {
		t.Errorf("CodexSnippet = %q, want %q", got, want)
	}

	withHeaders := CodexSnippet("https://dev-mcp.promptvm.ai", map[string]string{
		"X-PromptVM-Public-Key": "pk_x", "X-PromptVM-Secret-Key": "sk_y",
	})
	if !strings.Contains(withHeaders, "[mcp_servers.promptvm.headers]") {
		t.Errorf("headers table missing:\n%s", withHeaders)
	}
	if !strings.Contains(withHeaders, `X-PromptVM-Public-Key = "pk_x"`) {
		t.Errorf("public key header missing:\n%s", withHeaders)
	}
}

func TestClaudeSnippetFormat(t *testing.T) {
	got := ClaudeSnippet("https://dev-mcp.promptvm.ai")
	want := "claude mcp add --transport http promptvm https://dev-mcp.promptvm.ai"
	if got != want {
		t.Errorf("ClaudeSnippet = %q, want %q", got, want)
	}
}

// TestMergeCodexConfigFresh creates the section in an empty document.
func TestMergeCodexConfigFresh(t *testing.T) {
	out, err := MergeCodexConfig(nil, "https://dev-mcp.promptvm.ai", nil)
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeTOML(t, out)
	url := dig(t, doc, "mcp_servers", "promptvm", "url")
	if url != "https://dev-mcp.promptvm.ai" {
		t.Errorf("url = %v", url)
	}
	if typ := dig(t, doc, "mcp_servers", "promptvm", "type"); typ != "http" {
		t.Errorf("type = %v", typ)
	}
}

// TestMergeCodexConfigPreservesExisting keeps unrelated keys and other MCP
// servers while replacing an existing promptvm entry.
func TestMergeCodexConfigPreservesExisting(t *testing.T) {
	existing := []byte(`model = "gpt-5"

[mcp_servers.other]
type = "http"
url = "https://other.example.com"

[mcp_servers.promptvm]
type = "http"
url = "https://stale-mcp.example.com"
`)
	out, err := MergeCodexConfig(existing, "https://mcp.promptvm.ai", map[string]string{
		"X-PromptVM-Public-Key": "pk_a",
		"X-PromptVM-Secret-Key": "sk_b",
	})
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeTOML(t, out)

	if doc["model"] != "gpt-5" {
		t.Errorf("top-level key lost: model = %v", doc["model"])
	}
	if url := dig(t, doc, "mcp_servers", "other", "url"); url != "https://other.example.com" {
		t.Errorf("other server lost: %v", url)
	}
	if url := dig(t, doc, "mcp_servers", "promptvm", "url"); url != "https://mcp.promptvm.ai" {
		t.Errorf("promptvm url not replaced: %v", url)
	}
	if pk := dig(t, doc, "mcp_servers", "promptvm", "headers", "X-PromptVM-Public-Key"); pk != "pk_a" {
		t.Errorf("headers not written: %v", pk)
	}
}

func TestMergeCodexConfigRejectsInvalidTOML(t *testing.T) {
	if _, err := MergeCodexConfig([]byte("not [valid toml"), "https://mcp.promptvm.ai", nil); err == nil {
		t.Error("expected parse error for invalid TOML")
	}
}

// --- helpers ---

func decodeTOML(t *testing.T, data []byte) map[string]any {
	t.Helper()
	doc := map[string]any{}
	if err := toml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("output is not valid TOML: %v\n%s", err, data)
	}
	return doc
}

func dig(t *testing.T, doc map[string]any, keys ...string) any {
	t.Helper()
	var cur any = doc
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("dig %v: %T is not a map", keys, cur)
		}
		cur = m[k]
	}
	return cur
}
