package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
)

// Mirrors internal/client.newRootCmdWithFlags so flag-resolution tests
// here read flags the same way ResolveCredentials does.
func newRootCmdWithFlags() *cobra.Command {
	root := &cobra.Command{Use: "promptvm"}
	root.Flags().String("public-key", "", "")
	root.Flags().String("secret-key", "", "")
	root.Flags().String("api-key", "", "")
	root.Flags().String("base-url", "", "")
	return root
}

func TestNewFromContext_RequiresAPIKey(t *testing.T) {
	t.Setenv("PROMPTVM_API_KEY", "")
	t.Setenv("PROMPTVM_BASE_URL", "")
	// Redirect config dir so we don't accidentally pick up a real profile.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	cmd := newRootCmdWithFlags()
	if _, err := NewFromContext(cmd); err == nil {
		t.Error("expected error when no API key is configured")
	}
}

func TestNewFromContext_FromFlag(t *testing.T) {
	t.Setenv("PROMPTVM_API_KEY", "")
	t.Setenv("PROMPTVM_PUBLIC_KEY", "")
	t.Setenv("PROMPTVM_SECRET_KEY", "")
	t.Setenv("PROMPTVM_BASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	cmd := newRootCmdWithFlags()
	// Combined --api-key flag is the deprecated path but still supported per
	// F2 §US-001 layer 2. Must split into pk:sk in dual-header form on the wire.
	_ = cmd.Flags().Set("api-key", "pk_test_1234:sk_test_5678")
	_ = cmd.Flags().Set("base-url", "http://example.test")

	c, err := NewFromContext(cmd)
	if err != nil {
		t.Fatalf("NewFromContext: %v", err)
	}
	if c.PublicKey != "pk_test_1234" || c.SecretKey != "sk_test_5678" {
		t.Errorf("creds = (%q, %q), want (pk_test_1234, sk_test_5678)", c.PublicKey, c.SecretKey)
	}
	if c.BaseURL != "http://example.test" {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL, "http://example.test")
	}
}

func TestNewFromContext_FromEnv(t *testing.T) {
	// Dual env-var path (F2 §US-001 layer 3 — long-term supported, silent).
	t.Setenv("PROMPTVM_API_KEY", "")
	t.Setenv("PROMPTVM_PUBLIC_KEY", "pk_live_env")
	t.Setenv("PROMPTVM_SECRET_KEY", "sk_live_env")
	t.Setenv("PROMPTVM_BASE_URL", "http://env.example")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	cmd := newRootCmdWithFlags()
	c, err := NewFromContext(cmd)
	if err != nil {
		t.Fatalf("NewFromContext: %v", err)
	}
	if c.PublicKey != "pk_live_env" || c.SecretKey != "sk_live_env" {
		t.Errorf("creds = (%q, %q), want (pk_live_env, sk_live_env)", c.PublicKey, c.SecretKey)
	}
	if c.BaseURL != "http://env.example" {
		t.Errorf("BaseURL = %q", c.BaseURL)
	}
}

func TestNewFromContext_FromCombinedEnv(t *testing.T) {
	// Combined PROMPTVM_API_KEY env var (F2 §US-001 layer 4 — silent backward-compat).
	t.Setenv("PROMPTVM_PUBLIC_KEY", "")
	t.Setenv("PROMPTVM_SECRET_KEY", "")
	t.Setenv("PROMPTVM_API_KEY", "pk_live_envkey:sk_live_envsecret")
	t.Setenv("PROMPTVM_BASE_URL", "http://env.example")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	cmd := newRootCmdWithFlags()
	c, err := NewFromContext(cmd)
	if err != nil {
		t.Fatalf("NewFromContext: %v", err)
	}
	if c.PublicKey != "pk_live_envkey" || c.SecretKey != "sk_live_envsecret" {
		t.Errorf("creds = (%q, %q), want (pk_live_envkey, sk_live_envsecret)", c.PublicKey, c.SecretKey)
	}
}

func TestCaller_GetPostDelete(t *testing.T) {
	var seenAuth, seenMethod, seenPath, seenBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		seenMethod = r.Method
		seenPath = r.URL.Path
		if r.Body != nil {
			buf := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(buf)
			seenBody = string(buf)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "yes"})
	}))
	defer srv.Close()

	c := &Caller{APIKey: "k", BaseURL: srv.URL}

	var out map[string]string
	if err := c.Get("/hello", &out); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if seenAuth != "Bearer k" {
		t.Errorf("Authorization = %q", seenAuth)
	}
	if seenMethod != http.MethodGet || seenPath != "/hello" {
		t.Errorf("method/path = %s %s", seenMethod, seenPath)
	}
	if out["ok"] != "yes" {
		t.Errorf("response decode = %v", out)
	}

	if err := c.Post("/things", map[string]int{"n": 1}, &out); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if seenMethod != http.MethodPost || seenPath != "/things" {
		t.Errorf("post method/path = %s %s", seenMethod, seenPath)
	}
	if seenBody != `{"n":1}` {
		t.Errorf("body = %q, want %q", seenBody, `{"n":1}`)
	}

	if err := c.Delete("/things/1", nil); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if seenMethod != http.MethodDelete || seenPath != "/things/1" {
		t.Errorf("delete method/path = %s %s", seenMethod, seenPath)
	}
}

func TestCaller_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"nope"}`))
	}))
	defer srv.Close()

	c := &Caller{APIKey: "k", BaseURL: srv.URL}
	err := c.Get("/denied", nil)
	if err == nil {
		t.Fatal("expected error on 403")
	}
	if !contains(err.Error(), "403") {
		t.Errorf("error should include status code: %v", err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
