package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// newDualKeyTestCmd returns a cobra.Command with the flags
// runDualKeyLogin reads from. It does NOT register the function as a
// RunE — tests call runDualKeyLogin directly so they can target each
// validation branch.
func newDualKeyTestCmd(t *testing.T) *cobra.Command {
	t.Helper()
	c := &cobra.Command{Use: "promptvm"}
	c.Flags().String("base-url", "", "")
	c.Flags().String("profile", "", "")
	c.Flags().String("public-key", "", "")
	c.Flags().String("secret-key", "", "")
	var stdout, stderr bytes.Buffer
	c.SetOut(&stdout)
	c.SetErr(&stderr)
	return c
}

// isolateProfileDir redirects config storage and OS auth env to a
// fresh temp directory so the test can't read or write the
// developer's real profile.
func isolateProfileDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PROMPTVM_API_KEY", "")
	t.Setenv("PROMPTVM_PUBLIC_KEY", "")
	t.Setenv("PROMPTVM_SECRET_KEY", "")
	t.Setenv("PROMPTVM_BASE_URL", "")
}

func TestRunDualKeyLogin_PublicKeyMustHavePkPrefix(t *testing.T) {
	isolateProfileDir(t)
	cmd := newDualKeyTestCmd(t)
	err := runDualKeyLogin(cmd, "wrong_prefix", "sk_anything", "default")
	if err == nil {
		t.Fatalf("expected error for non-pk_ public key")
	}
	if !strings.Contains(err.Error(), "pk_") {
		t.Fatalf("error should mention pk_ prefix, got: %v", err)
	}
}

func TestRunDualKeyLogin_SecretKeyMustHaveSkPrefix(t *testing.T) {
	isolateProfileDir(t)
	cmd := newDualKeyTestCmd(t)
	err := runDualKeyLogin(cmd, "pk_anything", "wrong_prefix", "default")
	if err == nil {
		t.Fatalf("expected error for non-sk_ secret key")
	}
	if !strings.Contains(err.Error(), "sk_") {
		t.Fatalf("error should mention sk_ prefix, got: %v", err)
	}
}

func TestRunDualKeyLogin_RejectsInvalidProfileName(t *testing.T) {
	isolateProfileDir(t)
	cmd := newDualKeyTestCmd(t)
	// Profile names with `/` are rejected by config.ValidateProfileName
	// to prevent path traversal in the YAML write step.
	err := runDualKeyLogin(cmd, "pk_test_123", "sk_test_123", "../escape")
	if err == nil {
		t.Fatalf("expected error for path-traversal profile name")
	}
	if !strings.Contains(err.Error(), "profile name") {
		t.Fatalf("error should mention profile name, got: %v", err)
	}
}

// TestRunDualKeyLogin_SurfacesBackendValidationFailure stands up a
// httptest server that returns 401 for every request, points the SDK
// at it via --base-url, and asserts runDualKeyLogin surfaces the
// backend's rejection rather than silently writing a bad profile.
func TestRunDualKeyLogin_SurfacesBackendValidationFailure(t *testing.T) {
	isolateProfileDir(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized","message":"invalid api key"}`))
	}))
	t.Cleanup(server.Close)

	cmd := newDualKeyTestCmd(t)
	if err := cmd.Flags().Set("base-url", server.URL); err != nil {
		t.Fatalf("set base-url: %v", err)
	}

	err := runDualKeyLogin(cmd, "pk_test_validation", "sk_test_validation", "default")
	if err == nil {
		t.Fatalf("expected an error when the backend rejects the key")
	}
	if !strings.Contains(err.Error(), "API key validation failed") {
		t.Fatalf("error should wrap the validation failure, got: %v", err)
	}
}
