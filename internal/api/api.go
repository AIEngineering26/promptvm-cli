package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/config"
	"github.com/AIEngineering26/promptvm-cli/internal/oauth"
	"github.com/spf13/cobra"
)

const (
	defaultBaseURL = "https://api.promptvm.com"
	envBaseURL     = "PROMPTVM_BASE_URL"
)

// Caller makes raw HTTP requests to the API for endpoints where the SDK
// does not decode response bodies. It carries the active profile so it
// can transparently refresh OAuth tokens on 401 responses.
//
// For api-key profiles the Caller stores the public/secret key pair and
// emits dual headers (X-PromptVM-Public-Key + X-PromptVM-Secret-Key) on
// each request. For OAuth profiles only BearerToken is set, sent as
// Authorization: Bearer <token>.
type Caller struct {
	// APIKey is the legacy "pk_xxx:sk_xxx" form, retained for tests and
	// callers that want to inspect a single string. New code should rely
	// on PublicKey/SecretKey/BearerToken instead.
	APIKey string

	PublicKey   string
	SecretKey   string
	BearerToken string

	BaseURL string

	// profile is set when the caller was resolved from the active config
	// profile and is used to refresh OAuth tokens on the fly.
	profile *config.Profile
}

// NewFromContext creates a Caller from CLI flags, environment, and the
// active config profile.
//
// Credential resolution mirrors internal/client.ResolveCredentials so the
// SDK and the raw HTTP path always agree on which key set to use.
func NewFromContext(cmd *cobra.Command) (*Caller, error) {
	creds, err := client.ResolveCredentials(cmd)
	if err != nil {
		return nil, err
	}

	profile := activeProfile()

	baseURL := resolveFlag(cmd, "base-url")
	if baseURL == "" {
		baseURL = os.Getenv(envBaseURL)
	}
	if baseURL == "" && profile != nil {
		baseURL = profile.BaseURL
	}
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	c := &Caller{BaseURL: baseURL, profile: profile}
	if creds.IsAPIKey() {
		c.PublicKey = creds.PublicKey
		c.SecretKey = creds.SecretKey
		c.APIKey = creds.PublicKey + ":" + creds.SecretKey
	} else {
		c.BearerToken = creds.BearerToken
		c.APIKey = creds.BearerToken
	}
	return c, nil
}

// activeProfile loads the active profile, returning nil on any error.
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

// Get performs a GET request and decodes JSON into result.
func (c *Caller) Get(path string, result interface{}) error {
	return c.withAutoRefresh(func() error {
		req, err := http.NewRequest(http.MethodGet, c.BaseURL+path, nil)
		if err != nil {
			return err
		}
		return c.do(req, result)
	})
}

// Post performs a POST request with a JSON body.
func (c *Caller) Post(path string, body interface{}, result interface{}) error {
	return c.withAutoRefresh(func() error {
		return c.mutate(http.MethodPost, path, body, result)
	})
}

// Delete performs a DELETE request.
func (c *Caller) Delete(path string, result interface{}) error {
	return c.withAutoRefresh(func() error {
		req, err := http.NewRequest(http.MethodDelete, c.BaseURL+path, nil)
		if err != nil {
			return err
		}
		return c.do(req, result)
	})
}

// Patch performs a PATCH request with a JSON body.
func (c *Caller) Patch(path string, body interface{}, result interface{}) error {
	return c.withAutoRefresh(func() error {
		return c.mutate(http.MethodPatch, path, body, result)
	})
}

// withAutoRefresh retries fn once after refreshing an expired OAuth
// access token. For legacy API-key profiles it just runs fn directly.
func (c *Caller) withAutoRefresh(fn func() error) error {
	err := fn()
	if err == nil || c.profile == nil || !c.profile.IsOAuth() {
		return err
	}
	if !oauth.IsUnauthorizedError(err) {
		return err
	}

	// Force expiry and refresh.
	c.profile.ExpiresAt = time.Time{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tok, refreshErr := oauth.AccessTokenForProfile(ctx, c.profile)
	if refreshErr != nil {
		return fmt.Errorf("auto-refresh failed: %w (original: %v)", refreshErr, err)
	}
	c.BearerToken = tok
	c.APIKey = tok
	return fn()
}

func (c *Caller) mutate(method, path string, body interface{}, result interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshaling request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, bodyReader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.do(req, result)
}

func (c *Caller) do(req *http.Request, result interface{}) error {
	// Api-key credentials use dual headers
	// (X-PromptVM-Public-Key + X-PromptVM-Secret-Key); OAuth profiles
	// use Authorization: Bearer <jwt>. The two paths are mutually
	// exclusive.
	if c.PublicKey != "" && c.SecretKey != "" {
		req.Header.Set("X-PromptVM-Public-Key", c.PublicKey)
		req.Header.Set("X-PromptVM-Secret-Key", c.SecretKey)
	} else if c.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.BearerToken)
	} else if c.APIKey != "" {
		// Legacy fallback for tests that construct &Caller{APIKey: …}
		// directly without setting the explicit fields.
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	if result != nil {
		if err := json.Unmarshal(body, result); err != nil {
			return fmt.Errorf("parsing response: %w", err)
		}
	}
	return nil
}

func resolveFlag(cmd *cobra.Command, name string) string {
	// Try the merged flag set first (includes inherited persistent flags
	// when the command is wired through cobra normally), then fall back to
	// the root's persistent flag set for cases where Flags() hasn't been
	// merged yet.
	if val, err := cmd.Flags().GetString(name); err == nil && val != "" {
		return val
	}
	val, _ := cmd.Root().PersistentFlags().GetString(name)
	return val
}
