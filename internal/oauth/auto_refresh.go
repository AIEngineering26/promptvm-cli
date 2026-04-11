package oauth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AIEngineering26/promptvm-cli/internal/config"
)

// refreshSkew is how far in advance of ExpiresAt we proactively refresh.
// A small skew keeps us from sending a request with a token that's about
// to expire mid-flight on the server.
const refreshSkew = 60 * time.Second

// AccessTokenForProfile returns a usable access token for the given
// profile, transparently refreshing it if it has expired (or is within
// refreshSkew of expiry).
//
// For legacy api_key profiles it simply returns the stored API key.
// For OAuth profiles it loads tokens from the keychain, refreshes when
// needed, persists any new tokens, and returns the access token.
func AccessTokenForProfile(ctx context.Context, profile *config.Profile) (string, error) {
	if profile == nil {
		return "", errors.New("no active profile")
	}
	if !profile.IsOAuth() {
		return profile.APIKey, nil
	}

	tokens, err := LoadTokens(profile.Name)
	if err != nil {
		return "", fmt.Errorf("loading OAuth tokens for profile %q: %w", profile.Name, err)
	}

	// Prefer the expiry recorded on the profile YAML (the source of truth
	// written by login) and fall back to the keychain-side copy.
	expiry := profile.ExpiresAt
	if expiry.IsZero() {
		expiry = tokens.ExpiresAt
	}

	if !expiry.IsZero() && time.Now().Add(refreshSkew).Before(expiry) {
		return tokens.AccessToken, nil
	}
	// Token expired (or we don't know when it expires) — refresh.
	if tokens.RefreshToken == "" {
		return "", fmt.Errorf("access token expired and no refresh token available — run `promptvm auth login` again")
	}
	fresh, err := RefreshToken(ctx, profile.BaseURL, tokens.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("refreshing token: %w", err)
	}
	if err := persistRefreshed(profile, fresh); err != nil {
		return "", err
	}
	return fresh.AccessToken, nil
}

// persistRefreshed writes updated tokens back to the keychain and
// updates the profile's YAML expiry metadata.
func persistRefreshed(profile *config.Profile, tr *TokenResponse) error {
	stored := &StoredTokens{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		ExpiresAt:    tr.ExpiresAt,
	}
	if stored.RefreshToken == "" {
		// Some IdPs only return a new refresh token on the first call.
		// Keep the old one so the user isn't forced back through the
		// browser the next time this access token expires.
		old, err := LoadTokens(profile.Name)
		if err == nil {
			stored.RefreshToken = old.RefreshToken
		}
	}
	if err := SaveTokens(profile.Name, stored); err != nil {
		return fmt.Errorf("saving refreshed tokens: %w", err)
	}
	profile.ExpiresAt = tr.ExpiresAt
	if err := config.SaveProfile(profile); err != nil {
		return fmt.Errorf("updating profile expiry: %w", err)
	}
	return nil
}

// WithAutoRefresh executes fn. If fn fails with a 401-shaped error that
// mentions an expired token, WithAutoRefresh forces a refresh of the
// profile's OAuth tokens and retries fn exactly once.
//
// The "shape" of an expired-token error is detected heuristically via
// IsUnauthorizedError — the SDK does not expose a typed 401 to us, but
// it does surface the status code and the server's error body in the
// error string.
func WithAutoRefresh[T any](ctx context.Context, profile *config.Profile, fn func() (T, error)) (T, error) {
	var zero T
	result, err := fn()
	if err == nil {
		return result, nil
	}
	if profile == nil || !profile.IsOAuth() {
		return zero, err
	}
	if !IsUnauthorizedError(err) {
		return zero, err
	}

	// Force-refresh by clearing the in-memory expiry and calling the
	// resolver. AccessTokenForProfile will pick up the (now stale) expiry
	// and hit the refresh endpoint.
	profile.ExpiresAt = time.Time{}
	if _, refreshErr := AccessTokenForProfile(ctx, profile); refreshErr != nil {
		return zero, fmt.Errorf("auto-refresh failed: %w (original: %v)", refreshErr, err)
	}
	return fn()
}

// IsUnauthorizedError reports whether err looks like an OAuth 401 /
// expired-token response from the API. Checks both the literal string
// "401", "invalid_token", and "token_expired" markers.
func IsUnauthorizedError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "401") ||
		strings.Contains(msg, "invalid_token") ||
		strings.Contains(msg, "token_expired") ||
		strings.Contains(msg, "Unauthorized")
}
