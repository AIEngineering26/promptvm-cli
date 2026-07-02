package mcpsetup

import (
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

// TestDeriveMCPURL is the cross-repo contract: dev-api → dev-mcp,
// staging-api → staging-mcp, api → mcp, everything else underivable. The
// hosted server serves the MCP protocol only at the /mcp path, so the derived
// endpoint always carries it (matching the landing quickstart and the
// frontend's `${base}/mcp`).
func TestDeriveMCPURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://dev-api.promptvm.ai", "https://dev-mcp.promptvm.ai/mcp"},
		{"https://api.promptvm.ai", "https://mcp.promptvm.ai/mcp"},
		{"https://staging-api.promptvm.ai", "https://staging-mcp.promptvm.ai/mcp"},
		{"https://dev-api.promptvm.com", "https://dev-mcp.promptvm.com/mcp"},
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
	if err != nil || got != "https://dev-mcp.promptvm.ai/mcp" {
		t.Errorf("derived = %q, %v", got, err)
	}

	// Underivable with no override → error.
	if _, err = ResolveMCPURL("", "http://localhost:3000"); err == nil {
		t.Error("expected error for underivable base URL")
	}
}

// TestCodexSnippetFormat pins the ~/.codex/config.toml snippet to the
// client-snippets.ts codex-cli format (URL-only OAuth form, or a manual
// Authorization bearer header).
func TestCodexSnippetFormat(t *testing.T) {
	got := CodexSnippet("https://dev-mcp.promptvm.ai/mcp", nil)
	want := "[mcp_servers.promptvm]\ntype = \"http\"\nurl = \"https://dev-mcp.promptvm.ai/mcp\""
	if got != want {
		t.Errorf("CodexSnippet = %q, want %q", got, want)
	}

	withHeaders := CodexSnippet("https://dev-mcp.promptvm.ai/mcp", map[string]string{
		"Authorization": PkSkAuthorization("pk_x", "sk_y"),
	})
	if !strings.Contains(withHeaders, "[mcp_servers.promptvm.headers]") {
		t.Errorf("headers table missing:\n%s", withHeaders)
	}
	if !strings.Contains(withHeaders, `Authorization = "Bearer pvm_mcp_pkv1_`) {
		t.Errorf("authorization header missing:\n%s", withHeaders)
	}
}

// TestPkSkAuthorization pins the MCP direct pk/sk bearer envelope:
// base64url(pk:sk), no padding, behind the pvm_mcp_pkv1_ prefix.
func TestPkSkAuthorization(t *testing.T) {
	got := PkSkAuthorization("pk_abc", "sk_def")
	// base64url("pk_abc:sk_def") = "cGtfYWJjOnNrX2RlZg"
	want := "Bearer pvm_mcp_pkv1_cGtfYWJjOnNrX2RlZg"
	if got != want {
		t.Errorf("PkSkAuthorization = %q, want %q", got, want)
	}
}

func TestClaudeSnippetFormat(t *testing.T) {
	got := ClaudeSnippet("https://dev-mcp.promptvm.ai/mcp")
	want := "claude mcp add --transport http promptvm https://dev-mcp.promptvm.ai/mcp"
	if got != want {
		t.Errorf("ClaudeSnippet = %q, want %q", got, want)
	}
}

// TestClaudeAddCommandScopes: user and project scopes are passed through
// explicitly (claude's default is its private "local" scope, which would NOT
// match the shared .mcp.json fallback for --scope project).
func TestClaudeAddCommandScopes(t *testing.T) {
	got := strings.Join(ClaudeAddCommand("https://mcp.promptvm.ai/mcp", "project"), " ")
	if got != "mcp add --scope project --transport http promptvm https://mcp.promptvm.ai/mcp" {
		t.Errorf("project scope args = %q", got)
	}
	got = strings.Join(ClaudeAddCommand("https://mcp.promptvm.ai/mcp", "user"), " ")
	if got != "mcp add --scope user --transport http promptvm https://mcp.promptvm.ai/mcp" {
		t.Errorf("user scope args = %q", got)
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
	auth := PkSkAuthorization("pk_a", "sk_b")
	out, err := MergeCodexConfig(existing, "https://mcp.promptvm.ai/mcp", map[string]string{
		"Authorization": auth,
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
	if url := dig(t, doc, "mcp_servers", "promptvm", "url"); url != "https://mcp.promptvm.ai/mcp" {
		t.Errorf("promptvm url not replaced: %v", url)
	}
	if got := dig(t, doc, "mcp_servers", "promptvm", "headers", "Authorization"); got != auth {
		t.Errorf("headers not written: %v", got)
	}
}

// TestMergeCodexConfigPreservesComments: the merge is textual, so comments,
// blank lines, and formatting of unrelated content survive byte-for-byte.
func TestMergeCodexConfigPreservesComments(t *testing.T) {
	existing := []byte(`# my model config
model = "gpt-5" # pinned

# promptvm gets replaced below
[mcp_servers.promptvm]
type = "http"
url = "https://stale-mcp.example.com"

[mcp_servers.other] # keep me
type = "http"
url = "https://other.example.com"
`)
	out, err := MergeCodexConfig(existing, "https://mcp.promptvm.ai/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"# my model config",
		`model = "gpt-5" # pinned`,
		"[mcp_servers.other] # keep me",
		`url = "https://mcp.promptvm.ai/mcp"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("merged output missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "stale-mcp.example.com") {
		t.Errorf("stale promptvm entry not removed:\n%s", s)
	}
	// Still valid TOML with the expected semantics.
	doc := decodeTOML(t, out)
	if url := dig(t, doc, "mcp_servers", "promptvm", "url"); url != "https://mcp.promptvm.ai/mcp" {
		t.Errorf("promptvm url = %v", url)
	}
	if url := dig(t, doc, "mcp_servers", "other", "url"); url != "https://other.example.com" {
		t.Errorf("other server lost: %v", url)
	}
}

// TestExistingCodexAuthorization: a stored MCP bearer is detected for reuse;
// non-bearer or legacy X-PromptVM-* headers are not.
func TestExistingCodexAuthorization(t *testing.T) {
	auth := PkSkAuthorization("pk_a", "sk_b")
	withBearer := []byte("[mcp_servers.promptvm]\ntype = \"http\"\nurl = \"https://mcp.promptvm.ai/mcp\"\n\n[mcp_servers.promptvm.headers]\nAuthorization = \"" + auth + "\"\n")
	if got := ExistingCodexAuthorization(withBearer); got != auth {
		t.Errorf("bearer not detected: %q", got)
	}
	legacy := []byte("[mcp_servers.promptvm.headers]\nX-PromptVM-Public-Key = \"pk_a\"\n")
	if got := ExistingCodexAuthorization(legacy); got != "" {
		t.Errorf("legacy headers wrongly detected as usable: %q", got)
	}
	if got := ExistingCodexAuthorization(nil); got != "" {
		t.Errorf("empty config wrongly detected: %q", got)
	}
}

func TestMergeCodexConfigRejectsInvalidTOML(t *testing.T) {
	if _, err := MergeCodexConfig([]byte("not [valid toml"), "https://mcp.promptvm.ai/mcp", nil); err == nil {
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
