package oauth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// NewState returns a cryptographically random state value, encoded as an
// unpadded base64url string. The state is compared against the value
// returned from the authorization server to protect against CSRF.
func NewState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
