package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// httpClient is overridable in tests via postJSON wrappers.
var httpClient = &http.Client{Timeout: 30 * time.Second}

const (
	// clientID is the public identifier for the CLI. There is no secret;
	// security comes from PKCE and the loopback redirect.
	clientID = "promptvm-cli"

	cliTokenPath    = "/api/v1/auth/cli/token"
	deviceCodePath  = "/api/v1/auth/device/code"
	deviceTokenPath = "/api/v1/auth/device/token"
)

// ExchangeCode trades an authorization code + PKCE verifier for a token
// response. The redirect URI must match the one used when opening the
// browser.
func ExchangeCode(ctx context.Context, baseURL, code, verifier, redirectURI string) (*TokenResponse, error) {
	body := map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     clientID,
		"code":          code,
		"code_verifier": verifier,
		"redirect_uri":  redirectURI,
	}
	return postTokenJSON(ctx, baseURL+cliTokenPath, body)
}

// postTokenJSON sends a JSON body to a token endpoint and normalizes the
// result into *TokenResponse. OAuth-style error responses are mapped to
// Go errors that carry the underlying error code (invalid_grant, etc).
func postTokenJSON(ctx context.Context, url string, body map[string]string) (*TokenResponse, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading token response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errResp errorResponse
		if jsonErr := json.Unmarshal(raw, &errResp); jsonErr == nil && errResp.Error != "" {
			return nil, &OAuthError{
				Code:        errResp.Error,
				Description: errResp.ErrorDescription,
				Status:      resp.StatusCode,
			}
		}
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var tr TokenResponse
	if err := json.Unmarshal(raw, &tr); err != nil {
		return nil, fmt.Errorf("parsing token response: %w", err)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("token endpoint returned an empty access token")
	}
	tr.populateExpiry()
	return &tr, nil
}

// OAuthError carries a structured OAuth error code and description so
// callers can branch on things like authorization_pending or slow_down.
type OAuthError struct {
	Code        string
	Description string
	Status      int
}

func (e *OAuthError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Description)
	}
	return e.Code
}
