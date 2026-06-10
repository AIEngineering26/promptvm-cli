package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/AIEngineering26/promptvm-cli/internal/config"
	"github.com/AIEngineering26/promptvm-cli/internal/oauth"
	sdkclient "github.com/AIEngineering26/promptvm-go-sdk/client"
	"github.com/AIEngineering26/promptvm-go-sdk/option"
	"github.com/spf13/cobra"
)

const (
	defaultBaseURL = "https://dev-api.promptvm.ai"

	// envAPIKey is the legacy combined env var. Kept as a backward-compat
	// shim — pre-existing scripts continue to work without churn.
	envAPIKey = "PROMPTVM_API_KEY"

	// envPublicKey + envSecretKey is the long-term supported env-var path.
	envPublicKey = "PROMPTVM_PUBLIC_KEY"
	envSecretKey = "PROMPTVM_SECRET_KEY"

	envBaseURL = "PROMPTVM_BASE_URL"
)

// apiKeyForm is the documented combined credential form, surfaced in error
// messages so users see what shape we expect.
const apiKeyForm = "<form: pk_xxx:sk_xxx>"

// stderrWriter is the destination for deprecation warnings. Tests swap this
// out so they can assert on emitted output.
var stderrWriter io.Writer = os.Stderr

// Credentials holds a resolved api-key credential pair, or — for OAuth
// profiles — a single bearer token. Exactly one of (PublicKey, SecretKey)
// or BearerToken is set.
type Credentials struct {
	PublicKey   string
	SecretKey   string
	BearerToken string

	// Organization is the org UUID recorded on the profile the bearer
	// token came from. Sent as X-Org-Id so org-scoped routes (api-keys,
	// settings, billing) can resolve the active org for CLI sessions.
	// Empty for api-key credentials — the backend derives the org from
	// the key itself.
	Organization string
}

// IsAPIKey reports whether the credential is a dual-header api-key pair.
func (c Credentials) IsAPIKey() bool {
	return c.PublicKey != "" && c.SecretKey != ""
}

// NewFromContext creates an SDK client from CLI context.
//
// Credential resolution order (first match wins):
//
//  1. --public-key + --secret-key flags (both required together)
//  2. --api-key pk_…:sk_… flag (deprecated; emits stderr warning)
//  3. PROMPTVM_PUBLIC_KEY + PROMPTVM_SECRET_KEY env vars (silent)
//  4. PROMPTVM_API_KEY=pk_…:sk_… env var (silent backward-compat)
//  5. Active profile (api-key)
//  6. Active profile (OAuth)
//
// For OAuth profiles the access token is loaded from the keychain and
// auto-refreshed if it has expired.
func NewFromContext(cmd *cobra.Command) (*sdkclient.Client, error) {
	creds, err := ResolveCredentials(cmd)
	if err != nil {
		return nil, err
	}

	baseURL := resolveBaseURL(cmd)

	var opts []option.RequestOption
	if creds.IsAPIKey() {
		opts = append(opts, option.WithCredentials(creds.PublicKey, creds.SecretKey))
	} else {
		// OAuth bearer path — rely on the SDK's standard
		// Authorization: Bearer <token> header. X-Org-Id rides in the
		// same header object because the SDK's WithHTTPHeader replaces
		// (not merges) the header set.
		h := bearerHeader(creds.BearerToken)
		if creds.Organization != "" {
			h.Set("X-Org-Id", creds.Organization)
		}
		opts = append(opts, option.WithHTTPHeader(h))
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	return sdkclient.NewClient(opts...), nil
}

// bearerHeader builds an http.Header containing a single Authorization
// entry. We use option.WithHTTPHeader because option.WithAPIKey was
// removed in go-sdk v1; OAuth tokens are just bearer JWTs and the
// backend's OAuth middleware accepts the standard header.
func bearerHeader(token string) http.Header {
	h := make(http.Header)
	h.Set("Authorization", "Bearer "+token)
	return h
}

// ResolveCredentials walks the precedence table and returns either a
// (publicKey, secretKey) pair or a bearer token. Exposed for testing.
func ResolveCredentials(cmd *cobra.Command) (Credentials, error) {
	// 1. Explicit --public-key + --secret-key flags.
	flagPublic, _ := cmd.Flags().GetString("public-key")
	flagSecret, _ := cmd.Flags().GetString("secret-key")
	if flagPublic != "" || flagSecret != "" {
		if flagPublic == "" {
			return Credentials{}, fmt.Errorf("--public-key is required when --secret-key is set")
		}
		if flagSecret == "" {
			return Credentials{}, fmt.Errorf("--secret-key is required when --public-key is set")
		}
		return Credentials{PublicKey: flagPublic, SecretKey: flagSecret}, nil
	}

	// 2. --api-key pk:sk (deprecated combined form).
	if combined, _ := cmd.Flags().GetString("api-key"); combined != "" {
		pk, sk, err := parseCombinedAPIKey(combined)
		if err != nil {
			return Credentials{}, err
		}
		fmt.Fprintln(stderrWriter, "Warning: --api-key is deprecated; use --public-key/--secret-key")
		return Credentials{PublicKey: pk, SecretKey: sk}, nil
	}

	// 3. PROMPTVM_PUBLIC_KEY + PROMPTVM_SECRET_KEY (silent — long-term).
	envPub := os.Getenv(envPublicKey)
	envSec := os.Getenv(envSecretKey)
	if envPub != "" || envSec != "" {
		if envPub == "" {
			return Credentials{}, fmt.Errorf("%s is required when %s is set", envPublicKey, envSecretKey)
		}
		if envSec == "" {
			return Credentials{}, fmt.Errorf("%s is required when %s is set", envSecretKey, envPublicKey)
		}
		return Credentials{PublicKey: envPub, SecretKey: envSec}, nil
	}

	// 4. PROMPTVM_API_KEY=pk:sk (silent backward-compat).
	if combined := os.Getenv(envAPIKey); combined != "" {
		pk, sk, err := parseCombinedAPIKey(combined)
		if err != nil {
			return Credentials{}, err
		}
		return Credentials{PublicKey: pk, SecretKey: sk}, nil
	}

	// 5–6. Active profile.
	if profile := activeProfile(); profile != nil {
		if profile.IsOAuth() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			tok, err := oauth.AccessTokenForProfile(ctx, profile)
			if err != nil {
				return Credentials{}, err
			}
			return Credentials{BearerToken: tok, Organization: profile.Organization}, nil
		}
		if profile.PublicKey != "" && profile.SecretKey != "" {
			return Credentials{PublicKey: profile.PublicKey, SecretKey: profile.SecretKey}, nil
		}
		if profile.APIKey != "" {
			pk, sk, err := parseCombinedAPIKey(profile.APIKey)
			if err == nil {
				return Credentials{PublicKey: pk, SecretKey: sk}, nil
			}
			// Profile contained a non-pk:sk token — treat as bearer
			// (e.g. legacy pvk_… or pvcli_… token shapes).
			return Credentials{BearerToken: profile.APIKey, Organization: profile.Organization}, nil
		}
	}

	return Credentials{}, fmt.Errorf(
		"API key required: pass --public-key/--secret-key, set %s + %s env vars, "+
			"or run `promptvm auth login`",
		envPublicKey, envSecretKey,
	)
}

// parseCombinedAPIKey splits a "pk_xxx:sk_xxx" credential into its two
// halves. Returns a parse error pointing at the documented form for any
// malformed input.
func parseCombinedAPIKey(token string) (string, string, error) {
	if token == "" {
		return "", "", fmt.Errorf("api key is empty: expected %s", apiKeyForm)
	}
	parts := strings.Split(token, ":")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("api key %q is malformed: expected %s", redact(token), apiKeyForm)
	}
	pk, sk := parts[0], parts[1]
	if pk == "" || sk == "" {
		return "", "", fmt.Errorf("api key %q is malformed: expected %s", redact(token), apiKeyForm)
	}
	if !strings.HasPrefix(pk, "pk_") {
		return "", "", fmt.Errorf("api key %q is malformed: public key must start with 'pk_' (expected %s)", redact(token), apiKeyForm)
	}
	if !strings.HasPrefix(sk, "sk_") {
		return "", "", fmt.Errorf("api key %q is malformed: secret key must start with 'sk_' (expected %s)", redact(token), apiKeyForm)
	}
	return pk, sk, nil
}

// redact replaces the secret half of a combined token with stars so it
// doesn't leak into terminals or log files when surfaced in error
// messages. It keeps just enough prefix so the user can tell which key
// was malformed, without leaking enough characters to be useful as
// a credential on its own.
func redact(token string) string {
	idx := strings.Index(token, ":")
	if idx < 0 {
		// No colon — the whole token is presumed-secret junk. Show at
		// most the first 4 characters as a hint so the error message
		// isn't a content-free `"***"` for short inputs.
		const hint = 4
		if len(token) <= hint {
			return "***"
		}
		return token[:hint] + "***"
	}
	return token[:idx+1] + "***"
}

// resolveToken returns the bearer-style token to send with API requests.
// Retained as a thin shim for back-compat with tests and any callers that
// only need a single string. Prefer ResolveCredentials.
func resolveToken(cmd *cobra.Command) (string, error) {
	creds, err := ResolveCredentials(cmd)
	if err != nil {
		return "", err
	}
	if creds.IsAPIKey() {
		return creds.PublicKey + ":" + creds.SecretKey, nil
	}
	return creds.BearerToken, nil
}

func resolveBaseURL(cmd *cobra.Command) string {
	// 1. Flag
	if url, _ := cmd.Flags().GetString("base-url"); url != "" {
		return url
	}

	// 2. Environment variable
	if url := os.Getenv(envBaseURL); url != "" {
		return url
	}

	// 3. Config file (active profile)
	if profile := activeProfile(); profile != nil && profile.BaseURL != "" {
		return profile.BaseURL
	}

	return defaultBaseURL
}

// activeProfile loads the active profile from config, returning nil on any error.
func activeProfile() *config.Profile {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	profile, err := cfg.ActiveProfileData()
	if err != nil {
		return nil
	}
	return profile
}
