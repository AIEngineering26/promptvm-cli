package oauth

import "context"

// RefreshToken exchanges a refresh token for a fresh access/refresh pair.
// Callers are responsible for persisting the result back into the keychain.
func RefreshToken(ctx context.Context, baseURL, refreshToken string) (*TokenResponse, error) {
	body := map[string]string{
		"grant_type":    "refresh_token",
		"client_id":     clientID,
		"refresh_token": refreshToken,
	}
	return postTokenJSON(ctx, baseURL+cliTokenPath, body)
}
