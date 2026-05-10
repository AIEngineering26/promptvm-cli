package cmd

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AIEngineering26/promptvm-cli/internal/config"
)

// runStatusWithProfile installs a fake profile in a temp config dir and
// invokes the auth-status writers directly (bypassing the live
// validation HTTP call). It returns the captured stdout/stderr so
// callers can assert on what the user actually sees.
func runStatusWithProfile(t *testing.T, p *config.Profile, format string) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", filepath.Dir(t.TempDir()))
	t.Setenv("HOME", t.TempDir())

	st := authStatus{
		Profile:         p.Name,
		BaseURL:         p.BaseURL,
		Environment:     p.Environment,
		Organization:    p.Organization,
		PublicKeyPrefix: publicKeyPrefix(p),
		AuthType:        "api-key",
		Authenticated:   true,
	}

	var buf bytes.Buffer
	if format == "json" {
		if err := writeStatusJSON(&buf, st, false); err != nil {
			t.Fatalf("writeStatusJSON: %v", err)
		}
	} else {
		if err := writeStatusTable(&buf, st); err != nil {
			t.Fatalf("writeStatusTable: %v", err)
		}
	}
	return buf.String()
}

// TestAuthStatus_NeverLeaksSecretKey covers PRD F3 §US-004: under any
// output format and any flag combination, the secret key value must not
// appear anywhere in the rendered output of `auth status`.
func TestAuthStatus_NeverLeaksSecretKey(t *testing.T) {
	const (
		pk = "pk_554f77dcd1f04f7cb89f1fef22b3eccc396613ee"
		sk = "sk_a2b525b78466a9c80ae743b9c66021c3317de2e8"
	)

	profile := &config.Profile{
		Name:         "default",
		AuthType:     config.AuthTypeAPIKey,
		PublicKey:    pk,
		SecretKey:    sk,
		BaseURL:      "https://api.promptvm.com",
		Environment:  "live",
		Organization: "org_test",
	}

	for _, format := range []string{"table", "json"} {
		t.Run(format, func(t *testing.T) {
			got := runStatusWithProfile(t, profile, format)
			if strings.Contains(got, sk) {
				t.Fatalf("output contains the secret key:\n%s", got)
			}
			// Even partial substrings of the secret should be absent —
			// no truncated or asterisk-redacted form is allowed.
			if strings.Contains(got, "sk_a2b525") {
				t.Fatalf("output contains a secret-key prefix:\n%s", got)
			}
			// Public key prefix (first 12 chars) should be present in both formats.
			if !strings.Contains(got, "pk_554f77dcd") {
				t.Errorf("output missing public-key prefix:\n%s", got)
			}
		})
	}
}

// TestAuthStatus_NeverLeaksLegacyAPIKey makes sure profiles still
// holding a legacy `api_key: pk:sk` value (i.e. not yet migrated)
// don't smuggle the secret half through the legacy field either.
func TestAuthStatus_NeverLeaksLegacyAPIKey(t *testing.T) {
	const (
		pk = "pk_legacycccccccccccccccccccccccccccccccccc"
		sk = "sk_legacyddddddddddddddddddddddddddddddddddd"
	)
	profile := &config.Profile{
		Name:        "legacy",
		AuthType:    config.AuthTypeAPIKey,
		APIKey:      pk + ":" + sk,
		BaseURL:     "https://api.promptvm.com",
		Environment: "live",
	}
	for _, format := range []string{"table", "json"} {
		got := runStatusWithProfile(t, profile, format)
		if strings.Contains(got, sk) {
			t.Fatalf("[%s] output leaks secret key:\n%s", format, got)
		}
	}
}

// TestAuthStatus_JSONHasNoSecretField guarantees the JSON shape never
// carries a `secret_key` field, even when the profile in memory has one.
func TestAuthStatus_JSONHasNoSecretField(t *testing.T) {
	const sk = "sk_jsonshouldnotseeme00000000000000000000000"
	st := authStatus{
		Profile:         "default",
		AuthType:        "api-key",
		PublicKeyPrefix: "pk_abcdef0123",
		BaseURL:         "https://api.promptvm.com",
	}
	var buf bytes.Buffer
	if err := writeStatusJSON(&buf, st, true); err != nil {
		t.Fatalf("writeStatusJSON: %v", err)
	}
	if strings.Contains(buf.String(), "secret_key") {
		t.Fatalf("JSON output contains a 'secret_key' field: %s", buf.String())
	}
	// Sanity-check the JSON parses and exposes the expected fields.
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := decoded["secret_key"]; ok {
		t.Errorf("decoded JSON contains 'secret_key' key")
	}
	if _, ok := decoded["public_key_prefix"]; !ok {
		t.Errorf("decoded JSON missing 'public_key_prefix'")
	}
	// And just to be doubly paranoid, the secret value never appears.
	if strings.Contains(buf.String(), sk) {
		t.Fatalf("secret leaked into JSON: %s", buf.String())
	}
}

// TestAuthStatus_VerboseDoesNotLeakSecret simulates the user passing
// --verbose / debug mode and asserts the secret is still absent from
// the rendered output. Because writeStatusTable / writeStatusJSON take
// only the redacted authStatus struct (which has no secret-key field),
// no flag combination can ever splice the secret into the output.
func TestAuthStatus_VerboseDoesNotLeakSecret(t *testing.T) {
	const (
		pk = "pk_verbose0000000000000000000000000000000000"
		sk = "sk_verbose1111111111111111111111111111111111"
	)
	profile := &config.Profile{
		Name:        "default",
		AuthType:    config.AuthTypeAPIKey,
		PublicKey:   pk,
		SecretKey:   sk,
		BaseURL:     "https://api.promptvm.com",
		Environment: "live",
	}
	t.Setenv("PROMPTVM_VERBOSE", "1")
	for _, format := range []string{"table", "json"} {
		got := runStatusWithProfile(t, profile, format)
		if strings.Contains(got, sk) {
			t.Fatalf("[%s] verbose output leaks secret: %s", format, got)
		}
	}
}

// TestPublicKeyPrefix verifies the prefix returned for display is
// always non-secret and bounded to the documented length.
func TestPublicKeyPrefix(t *testing.T) {
	cases := []struct {
		name string
		in   *config.Profile
		want string
	}{
		{
			name: "dual-key",
			in:   &config.Profile{PublicKey: "pk_aaaaaaaaaaa", SecretKey: "sk_xxx"},
			want: "pk_aaaaaaaaa",
		},
		{
			name: "legacy combined",
			in:   &config.Profile{APIKey: "pk_legacy123456789:sk_legacy987654321"},
			want: "pk_legacy123",
		},
		{
			name: "oauth → empty",
			in:   &config.Profile{AuthType: config.AuthTypeOAuth},
			want: "",
		},
		{
			name: "short pk",
			in:   &config.Profile{PublicKey: "pk_short"},
			want: "pk_short",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := publicKeyPrefix(c.in)
			if got != c.want {
				t.Errorf("publicKeyPrefix = %q, want %q", got, c.want)
			}
		})
	}
}
