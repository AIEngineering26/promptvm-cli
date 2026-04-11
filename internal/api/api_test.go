package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
)

// The Caller reads flags via cmd.Root().PersistentFlags(), so the test
// command needs to be wrapped in a fake root with those persistent flags.
func newRootCmdWithFlags() *cobra.Command {
	root := &cobra.Command{Use: "promptvm"}
	root.PersistentFlags().String("api-key", "", "")
	root.PersistentFlags().String("base-url", "", "")
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
	t.Setenv("PROMPTVM_BASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	cmd := newRootCmdWithFlags()
	_ = cmd.PersistentFlags().Set("api-key", "pvk_test_1234")
	_ = cmd.PersistentFlags().Set("base-url", "http://example.test")

	c, err := NewFromContext(cmd)
	if err != nil {
		t.Fatalf("NewFromContext: %v", err)
	}
	if c.APIKey != "pvk_test_1234" {
		t.Errorf("APIKey = %q, want %q", c.APIKey, "pvk_test_1234")
	}
	if c.BaseURL != "http://example.test" {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL, "http://example.test")
	}
}

func TestNewFromContext_FromEnv(t *testing.T) {
	t.Setenv("PROMPTVM_API_KEY", "pvk_live_envkey")
	t.Setenv("PROMPTVM_BASE_URL", "http://env.example")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	cmd := newRootCmdWithFlags()
	c, err := NewFromContext(cmd)
	if err != nil {
		t.Fatalf("NewFromContext: %v", err)
	}
	if c.APIKey != "pvk_live_envkey" {
		t.Errorf("APIKey = %q", c.APIKey)
	}
	if c.BaseURL != "http://env.example" {
		t.Errorf("BaseURL = %q", c.BaseURL)
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
