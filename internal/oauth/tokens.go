package oauth

import (
	"time"
)

// TokenResponse is the normalized shape returned by every token-granting
// endpoint in this package. Expiry is computed at parse time so callers
// never need to know about raw ExpiresIn.
type TokenResponse struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	ExpiresIn    int       `json:"expires_in,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	UserID       string    `json:"user_id,omitempty"`
	UserEmail    string    `json:"user_email,omitempty"`
	Organization string    `json:"organization,omitempty"`
	ExpiresAt    time.Time `json:"-"`
}

// populateExpiry fills ExpiresAt from ExpiresIn if it is still unset.
func (t *TokenResponse) populateExpiry() {
	if t.ExpiresAt.IsZero() && t.ExpiresIn > 0 {
		t.ExpiresAt = time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)
	}
}

// errorResponse is the RFC 6749 §5.2 error object. Also used by the device
// flow (RFC 8628 §3.5) where errors like authorization_pending / slow_down
// are returned with a 400 status.
type errorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}
