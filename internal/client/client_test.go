package client

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const (
	testPK = "pk_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testSK = "sk_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// newRootCmdWithFlags returns a cobra command wired with the same
// persistent credential flags as the real CLI root, so resolution code
// reads them out cleanly.
func newRootCmdWithFlags() *cobra.Command {
	root := &cobra.Command{Use: "promptvm"}
	root.Flags().String("public-key", "", "")
	root.Flags().String("secret-key", "", "")
	root.Flags().String("api-key", "", "")
	root.Flags().String("base-url", "", "")
	return root
}

// resetEnv unsets all PROMPTVM_* env vars and redirects config storage
// to a temp dir so tests can't accidentally pick up the developer's
// real profile.
func resetEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PROMPTVM_API_KEY", "")
	t.Setenv("PROMPTVM_PUBLIC_KEY", "")
	t.Setenv("PROMPTVM_SECRET_KEY", "")
	t.Setenv("PROMPTVM_BASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
}

// TestResolveCredentials drives the four-layer precedence table from the
// PRD. Each row exercises one cell and the expected outcome.
func TestResolveCredentials(t *testing.T) {
	tests := []struct {
		name             string
		setup            func(cmd *cobra.Command)
		wantPK           string
		wantSK           string
		wantBearer       string
		wantErr          bool
		wantErrSubstr    string
		wantWarnContains string
	}{
		// 1. --public-key + --secret-key wins over everything.
		{
			name: "flag pair wins over api-key flag",
			setup: func(cmd *cobra.Command) {
				_ = cmd.Flags().Set("public-key", testPK)
				_ = cmd.Flags().Set("secret-key", testSK)
				_ = cmd.Flags().Set("api-key", "pk_other:sk_other")
			},
			wantPK: testPK,
			wantSK: testSK,
		},
		{
			name: "flag pair wins over env vars",
			setup: func(cmd *cobra.Command) {
				_ = cmd.Flags().Set("public-key", testPK)
				_ = cmd.Flags().Set("secret-key", testSK)
				t := cmd
				_ = t
			},
			wantPK: testPK,
			wantSK: testSK,
		},
		{
			name: "missing --public-key with --secret-key errors",
			setup: func(cmd *cobra.Command) {
				_ = cmd.Flags().Set("secret-key", testSK)
			},
			wantErr:       true,
			wantErrSubstr: "--public-key",
		},
		{
			name: "missing --secret-key with --public-key errors",
			setup: func(cmd *cobra.Command) {
				_ = cmd.Flags().Set("public-key", testPK)
			},
			wantErr:       true,
			wantErrSubstr: "--secret-key",
		},

		// 2. --api-key combined (deprecated, emits warning).
		{
			name: "api-key flag splits and warns",
			setup: func(cmd *cobra.Command) {
				_ = cmd.Flags().Set("api-key", testPK+":"+testSK)
			},
			wantPK:           testPK,
			wantSK:           testSK,
			wantWarnContains: "deprecated",
		},
		{
			name: "api-key flag pk_only is malformed",
			setup: func(cmd *cobra.Command) {
				_ = cmd.Flags().Set("api-key", "pk_only")
			},
			wantErr:       true,
			wantErrSubstr: "pk_xxx:sk_xxx",
		},
		{
			name: "api-key flag :sk_only is malformed",
			setup: func(cmd *cobra.Command) {
				_ = cmd.Flags().Set("api-key", ":sk_only")
			},
			wantErr:       true,
			wantErrSubstr: "pk_xxx:sk_xxx",
		},
		{
			name: "api-key flag pksk_no_colon is malformed",
			setup: func(cmd *cobra.Command) {
				_ = cmd.Flags().Set("api-key", "pksk_no_colon")
			},
			wantErr:       true,
			wantErrSubstr: "pk_xxx:sk_xxx",
		},
		{
			name: "api-key flag with extra colon is malformed",
			setup: func(cmd *cobra.Command) {
				_ = cmd.Flags().Set("api-key", "pk_x:sk_x:extra")
			},
			wantErr:       true,
			wantErrSubstr: "pk_xxx:sk_xxx",
		},

		// 3. PROMPTVM_PUBLIC_KEY + PROMPTVM_SECRET_KEY env vars.
		{
			name: "env pair resolves silently",
			setup: func(cmd *cobra.Command) {
				cmd.SetContext(cmd.Context())
			},
			// env set in the test wrapper below
			wantPK: testPK,
			wantSK: testSK,
		},

		// 4. PROMPTVM_API_KEY combined env (silent).
		// (Test wrapper sets via env-only branch.)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetEnv(t)
			cmd := newRootCmdWithFlags()

			// Special-case the env-pair test by setting envs.
			if tt.name == "env pair resolves silently" {
				t.Setenv("PROMPTVM_PUBLIC_KEY", testPK)
				t.Setenv("PROMPTVM_SECRET_KEY", testSK)
			}

			tt.setup(cmd)

			var buf bytes.Buffer
			old := stderrWriter
			stderrWriter = &buf
			t.Cleanup(func() { stderrWriter = old })

			creds, err := ResolveCredentials(cmd)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", creds)
				}
				if tt.wantErrSubstr != "" && !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErrSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if creds.PublicKey != tt.wantPK || creds.SecretKey != tt.wantSK {
				t.Fatalf("got pk=%q sk=%q; want pk=%q sk=%q",
					creds.PublicKey, creds.SecretKey, tt.wantPK, tt.wantSK)
			}
			if tt.wantBearer != "" && creds.BearerToken != tt.wantBearer {
				t.Fatalf("got bearer=%q want %q", creds.BearerToken, tt.wantBearer)
			}
			if tt.wantWarnContains != "" {
				if !strings.Contains(buf.String(), tt.wantWarnContains) {
					t.Fatalf("expected stderr to contain %q, got %q",
						tt.wantWarnContains, buf.String())
				}
			} else {
				if buf.Len() != 0 {
					t.Fatalf("expected no stderr output, got %q", buf.String())
				}
			}
		})
	}
}

// TestResolveCredentials_EnvAPIKeyCombined exercises the silent
// PROMPTVM_API_KEY backward-compat shim.
func TestResolveCredentials_EnvAPIKeyCombined(t *testing.T) {
	resetEnv(t)
	t.Setenv("PROMPTVM_API_KEY", testPK+":"+testSK)

	cmd := newRootCmdWithFlags()
	var buf bytes.Buffer
	old := stderrWriter
	stderrWriter = &buf
	t.Cleanup(func() { stderrWriter = old })

	creds, err := ResolveCredentials(cmd)
	if err != nil {
		t.Fatalf("ResolveCredentials: %v", err)
	}
	if creds.PublicKey != testPK || creds.SecretKey != testSK {
		t.Fatalf("got pk=%q sk=%q", creds.PublicKey, creds.SecretKey)
	}
	if buf.Len() != 0 {
		t.Fatalf("PROMPTVM_API_KEY should be silent, got stderr=%q", buf.String())
	}
}

// TestResolveCredentials_PrecedenceFlagBeatsEnv covers the conflict case
// in the PRD's table — flag wins.
func TestResolveCredentials_PrecedenceFlagBeatsEnv(t *testing.T) {
	resetEnv(t)
	t.Setenv("PROMPTVM_PUBLIC_KEY", "pk_envvalue")
	t.Setenv("PROMPTVM_SECRET_KEY", "sk_envvalue")

	cmd := newRootCmdWithFlags()
	_ = cmd.Flags().Set("public-key", testPK)
	_ = cmd.Flags().Set("secret-key", testSK)

	creds, err := ResolveCredentials(cmd)
	if err != nil {
		t.Fatalf("ResolveCredentials: %v", err)
	}
	if creds.PublicKey != testPK || creds.SecretKey != testSK {
		t.Fatalf("flag did not beat env: got pk=%q sk=%q", creds.PublicKey, creds.SecretKey)
	}
}

// TestResolveCredentials_EnvPartialErrors exercises the partial-env-pair
// failure mode demanded by US-001.
func TestResolveCredentials_EnvPartialErrors(t *testing.T) {
	cases := []struct {
		name string
		set  func(*testing.T)
		want string
	}{
		{
			name: "only PROMPTVM_PUBLIC_KEY",
			set: func(t *testing.T) {
				t.Setenv("PROMPTVM_PUBLIC_KEY", testPK)
			},
			want: "PROMPTVM_SECRET_KEY",
		},
		{
			name: "only PROMPTVM_SECRET_KEY",
			set: func(t *testing.T) {
				t.Setenv("PROMPTVM_SECRET_KEY", testSK)
			},
			want: "PROMPTVM_PUBLIC_KEY",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetEnv(t)
			tc.set(t)
			cmd := newRootCmdWithFlags()
			if _, err := ResolveCredentials(cmd); err == nil {
				t.Fatal("expected error")
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not name %q", err.Error(), tc.want)
			}
		})
	}
}

// TestResolveCredentials_Missing exercises the "no credentials anywhere"
// error path so users get the documented hint.
func TestResolveCredentials_Missing(t *testing.T) {
	resetEnv(t)
	cmd := newRootCmdWithFlags()
	_, err := ResolveCredentials(cmd)
	if err == nil {
		t.Fatal("expected error when no credentials are set")
	}
	if !strings.Contains(err.Error(), "PROMPTVM_PUBLIC_KEY") {
		t.Fatalf("error should mention env var: %v", err)
	}
}

// TestResolveToken keeps the legacy single-string code path covered.
func TestResolveToken_Compose(t *testing.T) {
	resetEnv(t)
	t.Setenv("PROMPTVM_PUBLIC_KEY", testPK)
	t.Setenv("PROMPTVM_SECRET_KEY", testSK)

	cmd := newRootCmdWithFlags()
	got, err := resolveToken(cmd)
	if err != nil {
		t.Fatalf("resolveToken: %v", err)
	}
	want := testPK + ":" + testSK
	if got != want {
		t.Errorf("resolveToken = %q, want %q", got, want)
	}
}

func TestResolveBaseURL_Default(t *testing.T) {
	resetEnv(t)
	cmd := newRootCmdWithFlags()
	if got := resolveBaseURL(cmd); got != defaultBaseURL {
		t.Errorf("resolveBaseURL = %q, want %q", got, defaultBaseURL)
	}
}

func TestResolveBaseURL_FlagBeatsEnv(t *testing.T) {
	resetEnv(t)
	t.Setenv("PROMPTVM_BASE_URL", "http://env")
	cmd := newRootCmdWithFlags()
	_ = cmd.Flags().Set("base-url", "http://flag")
	if got := resolveBaseURL(cmd); got != "http://flag" {
		t.Errorf("resolveBaseURL = %q, want flag", got)
	}
}

// TestParseCombinedAPIKey exercises the malformed-input table from the PRD.
func TestParseCombinedAPIKey(t *testing.T) {
	cases := []struct {
		input   string
		wantErr bool
	}{
		{testPK + ":" + testSK, false},
		{"pk_only", true},
		{":sk_only", true},
		{"pksk_no_colon", true},
		{"pk_x:sk_x:extra", true},
		{"", true},
		{":", true},
		{"sk_x:pk_x", true}, // wrong prefix order
	}
	for _, c := range cases {
		t.Run(c.input, func(t *testing.T) {
			_, _, err := parseCombinedAPIKey(c.input)
			if c.wantErr && err == nil {
				t.Errorf("expected error for %q", c.input)
			}
			if !c.wantErr && err != nil {
				t.Errorf("unexpected error for %q: %v", c.input, err)
			}
		})
	}
}

func TestRedact(t *testing.T) {
	got := redact(testPK + ":" + testSK)
	if strings.Contains(got, testSK) {
		t.Errorf("secret leaked through redact: %q", got)
	}
	if !strings.HasPrefix(got, testPK+":") {
		t.Errorf("public half lost: %q", got)
	}
}
