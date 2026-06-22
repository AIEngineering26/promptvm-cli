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
	defaultBaseURL = "https://dev-api.promptvm.ai"
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

// AnonymousFromContext creates a Caller for public, unauthenticated endpoints
// (e.g. GET /api/v1/skills/s/:slug). Unlike NewFromContext it never errors when
// no credentials are present — the public routes accept anonymous requests.
//
// Base-URL resolution still honors --base-url, then PROMPTVM_BASE_URL, then the
// active profile's baseUrl, then the default. If credentials happen to be
// available (flags, env, or an active profile) they are attached best-effort so
// authenticated callers keep working; a credential-resolution failure is
// ignored and the request proceeds anonymously.
func AnonymousFromContext(cmd *cobra.Command) *Caller {
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

	// Best-effort: attach credentials if they resolve cleanly, but never fail.
	if creds, err := client.ResolveCredentials(cmd); err == nil {
		if creds.IsAPIKey() {
			c.PublicKey = creds.PublicKey
			c.SecretKey = creds.SecretKey
			c.APIKey = creds.PublicKey + ":" + creds.SecretKey
		} else if creds.BearerToken != "" {
			c.BearerToken = creds.BearerToken
			c.APIKey = creds.BearerToken
		}
	}
	return c
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

// StatusError is returned by GetWithContext when the server responds with a
// status >= 400. It carries the HTTP status code so callers can map specific
// statuses (e.g. 404) to tailored, user-facing messages.
type StatusError struct {
	StatusCode int
	Body       string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("API error %d: %s", e.StatusCode, e.Body)
}

// GetWithContext performs a GET using the supplied context (so callers control
// the request timeout) and decodes JSON into result. On a >= 400 response it
// returns a *StatusError; on a transport failure it returns the underlying
// error so callers can distinguish "not found" from "could not connect".
//
// It does not auto-refresh OAuth tokens — it is intended for public endpoints
// reachable anonymously; if credentials are attached they ride along but a 401
// is surfaced as a StatusError rather than triggering a refresh.
func (c *Caller) GetWithContext(ctx context.Context, path string, result interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	return c.doStatus(req, result)
}

// PostBestEffort performs a fire-and-forget POST with the supplied context. It
// is used for non-critical side effects (e.g. the install counter): any
// transport error or >= 400 response is returned for optional logging, never to
// fail the caller's primary operation.
func (c *Caller) PostBestEffort(ctx context.Context, path string, body interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bodyReader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.doStatus(req, nil)
}

// doStatus is like do but returns a *StatusError (with the status code) on a
// >= 400 response instead of a flattened error string.
func (c *Caller) doStatus(req *http.Request, result interface{}) error {
	if c.PublicKey != "" && c.SecretKey != "" {
		req.Header.Set("X-PromptVM-Public-Key", c.PublicKey)
		req.Header.Set("X-PromptVM-Secret-Key", c.SecretKey)
	} else if c.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.BearerToken)
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
		return &StatusError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	if result != nil && len(body) > 0 {
		if err := json.Unmarshal(body, result); err != nil {
			return fmt.Errorf("parsing response: %w", err)
		}
	}
	return nil
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
