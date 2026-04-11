package oauth

import (
	"errors"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

// keychainService is the service name under which all CLI tokens are stored.
// The key name encodes the profile and the token type, e.g.
// "promptvm-cli:default:access".
const keychainService = "promptvm-cli"

// StoredTokens is the subset of TokenResponse we persist. It excludes
// scope and user metadata since those live in the YAML profile.
type StoredTokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
}

// tokenRef returns the keychain item name for a profile's tokens.
func tokenRef(profile string) string {
	return keychainService + ":" + profile
}

// accessKey / refreshKey produce the distinct keychain keys for each
// half of a token pair under a given profile.
func accessKey(profile string) string  { return tokenRef(profile) + ":access" }
func refreshKey(profile string) string { return tokenRef(profile) + ":refresh" }

// SaveTokens persists both access and refresh tokens for the given profile.
// On systems without a usable keychain, it falls back to the encrypted
// file store in keychain_file.go.
func SaveTokens(profile string, tokens *StoredTokens) error {
	if err := keyring.Set(keychainService, accessKey(profile), tokens.AccessToken); err != nil {
		if isUnavailable(err) {
			return saveTokensToFile(profile, tokens)
		}
		return err
	}
	if tokens.RefreshToken != "" {
		if err := keyring.Set(keychainService, refreshKey(profile), tokens.RefreshToken); err != nil {
			return err
		}
	}
	// Mirror an expiry marker into the keychain so callers that only read
	// the keychain (not YAML) can still tell when the access token is stale.
	if !tokens.ExpiresAt.IsZero() {
		_ = keyring.Set(keychainService, tokenRef(profile)+":expires", tokens.ExpiresAt.Format(time.RFC3339))
	}
	return nil
}

// LoadTokens returns the stored tokens for a profile. Falls back to the
// file store if the keychain is unavailable.
func LoadTokens(profile string) (*StoredTokens, error) {
	access, err := keyring.Get(keychainService, accessKey(profile))
	if err != nil {
		if isUnavailable(err) {
			return loadTokensFromFile(profile)
		}
		if errors.Is(err, keyring.ErrNotFound) {
			return nil, ErrNoTokens
		}
		return nil, err
	}
	refresh, err := keyring.Get(keychainService, refreshKey(profile))
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return nil, err
	}
	st := &StoredTokens{AccessToken: access, RefreshToken: refresh}
	if raw, err := keyring.Get(keychainService, tokenRef(profile)+":expires"); err == nil {
		if t, perr := time.Parse(time.RFC3339, raw); perr == nil {
			st.ExpiresAt = t
		}
	}
	return st, nil
}

// DeleteTokens removes any stored tokens for the profile. It does not
// error if tokens were never stored. File fallback items are also removed.
func DeleteTokens(profile string) error {
	var firstErr error
	for _, k := range []string{accessKey(profile), refreshKey(profile), tokenRef(profile) + ":expires"} {
		if err := keyring.Delete(keychainService, k); err != nil &&
			!errors.Is(err, keyring.ErrNotFound) && !isUnavailable(err) {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	// Always attempt to clean up the file fallback, regardless of keychain
	// availability — stale files on disk are more dangerous than extra work.
	if err := deleteTokensFile(profile); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// ErrNoTokens is returned when no tokens are stored for the given profile.
var ErrNoTokens = errors.New("no stored tokens for profile")

// isUnavailable reports whether an error indicates that the keyring
// subsystem isn't reachable at all (e.g. no Secret Service on Linux,
// running in a container without a session). When true, we fall back
// to the encrypted-file store instead of failing the command.
func isUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, keyring.ErrUnsupportedPlatform) {
		return true
	}
	// go-keyring wraps dbus errors as plain strings on Linux; match loosely.
	msg := err.Error()
	for _, needle := range []string{
		"not provided by any .service files",
		"dbus",
		"The name org.freedesktop.secrets was not provided",
		"no such interface",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}
