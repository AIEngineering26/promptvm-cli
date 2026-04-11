// Package oauth implements the OAuth 2.0 flows used by the PromptVM CLI:
// the Authorization Code flow with PKCE over a loopback redirect (RFC 8252)
// and the Device Authorization Grant (RFC 8628). It also provides secure
// token storage backed by the OS keychain with an encrypted-file fallback.
package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// GenerateVerifier returns a cryptographically random PKCE code verifier
// encoded as an unpadded base64url string. RFC 7636 requires the verifier
// to be 43-128 characters; 32 random bytes → 43 characters.
func GenerateVerifier() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating PKCE verifier: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Challenge returns the S256 code challenge for the given verifier:
// base64url(sha256(verifier)) with no padding.
func Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
