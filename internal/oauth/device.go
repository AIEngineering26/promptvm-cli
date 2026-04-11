package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DeviceCodeResponse is the RFC 8628 §3.2 device authorization response.
type DeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// RequestDeviceCode starts a device authorization grant. deviceName is
// sent to the server as a human label so the user can distinguish this
// session in the authorized-devices list later.
func RequestDeviceCode(ctx context.Context, baseURL, deviceName string) (*DeviceCodeResponse, error) {
	body := map[string]string{
		"client_id":   clientID,
		"scope":       "profile",
		"device_name": deviceName,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+deviceCodePath, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("device code request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading device code response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("device code endpoint returned %d: %s", resp.StatusCode, string(raw))
	}

	var dc DeviceCodeResponse
	if err := json.Unmarshal(raw, &dc); err != nil {
		return nil, fmt.Errorf("parsing device code response: %w", err)
	}
	if dc.DeviceCode == "" || dc.UserCode == "" {
		return nil, fmt.Errorf("device code response missing required fields")
	}
	if dc.Interval <= 0 {
		dc.Interval = 5 // sensible RFC 8628 default
	}
	return &dc, nil
}

// PollDeviceToken polls the device-token endpoint until the user
// authorizes the device, the code expires, or the context is cancelled.
//
// Follows RFC 8628 §3.5 error handling:
//   - authorization_pending: continue polling at the same interval
//   - slow_down: add 5 seconds to the interval and continue
//   - expired_token: return an error instructing the user to re-run login
//   - access_denied: return "authorization denied"
func PollDeviceToken(ctx context.Context, baseURL, deviceCode string, interval int) (*TokenResponse, error) {
	if interval <= 0 {
		interval = 5
	}
	wait := time.Duration(interval) * time.Second

	for {
		// Sleep first so we don't hammer the endpoint immediately —
		// servers specify the minimum interval they want us to honor.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}

		tr, err := postTokenJSON(ctx, baseURL+deviceTokenPath, map[string]string{
			"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
			"client_id":   clientID,
			"device_code": deviceCode,
		})
		if err == nil {
			return tr, nil
		}

		var oauthErr *OAuthError
		if !errors.As(err, &oauthErr) {
			return nil, err
		}

		switch oauthErr.Code {
		case "authorization_pending":
			// keep polling at the current interval
		case "slow_down":
			wait += 5 * time.Second
		case "expired_token":
			return nil, fmt.Errorf("device code expired — run `promptvm auth login --device` again")
		case "access_denied":
			return nil, fmt.Errorf("authorization denied")
		default:
			return nil, err
		}
	}
}
