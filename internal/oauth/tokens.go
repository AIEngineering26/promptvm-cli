package oauth

import (
	"time"
)

// TokenUser is the nested user object returned by the backend token endpoints.
type TokenUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
	Image string `json:"image,omitempty"`
}

// TokenOrganization is the nested organization object returned by the backend.
type TokenOrganization struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// TokenResponse is the normalized shape returned by every token-granting
// endpoint in this package. Expiry is computed at parse time so callers
// never need to know about raw ExpiresIn.
type TokenResponse struct {
	AccessToken  string             `json:"access_token"`
	RefreshToken string             `json:"refresh_token,omitempty"`
	TokenType    string             `json:"token_type,omitempty"`
	ExpiresIn    int                `json:"expires_in,omitempty"`
	Scope        string             `json:"scope,omitempty"`
	User         *TokenUser         `json:"user,omitempty"`
	Organization *TokenOrganization `json:"organization,omitempty"`
	ExpiresAt    time.Time          `json:"-"`
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
