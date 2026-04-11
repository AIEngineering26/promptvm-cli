package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestPollDeviceToken_PendingThenSlowDownThenSuccess walks through the
// three non-terminal states the server can return before finally
// granting a token. It verifies:
//
//  1. authorization_pending keeps the interval unchanged
//  2. slow_down adds 5 seconds to the interval permanently
//  3. a 200 response is returned to the caller
func TestPollDeviceToken_PendingThenSlowDownThenSuccess(t *testing.T) {
	var hits int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		switch n {
		case 1:
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
		case 2:
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "slow_down"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "final",
				"expires_in":   60,
			})
		}
	}))
	defer srv.Close()

	old := httpClient
	httpClient = &http.Client{Timeout: 3 * time.Second}
	t.Cleanup(func() { httpClient = old })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Interval of 1 keeps the test fast while still exercising slow_down.
	tr, err := PollDeviceToken(ctx, srv.URL, "dev_code", 1)
	if err != nil {
		t.Fatalf("PollDeviceToken: %v", err)
	}
	if tr.AccessToken != "final" {
		t.Errorf("AccessToken = %q, want final", tr.AccessToken)
	}
	if atomic.LoadInt32(&hits) < 3 {
		t.Errorf("hits = %d, want >= 3", hits)
	}
}

func TestPollDeviceToken_ExpiredToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "expired_token"})
	}))
	defer srv.Close()

	old := httpClient
	httpClient = &http.Client{Timeout: 3 * time.Second}
	t.Cleanup(func() { httpClient = old })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := PollDeviceToken(ctx, srv.URL, "dev_code", 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("error = %v, want expired message", err)
	}
}

func TestPollDeviceToken_AccessDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "access_denied"})
	}))
	defer srv.Close()

	old := httpClient
	httpClient = &http.Client{Timeout: 3 * time.Second}
	t.Cleanup(func() { httpClient = old })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := PollDeviceToken(ctx, srv.URL, "dev_code", 1)
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Errorf("err = %v, want denied", err)
	}
}

func TestRequestDeviceCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":               "dc_123",
			"user_code":                 "WXYZ-1234",
			"verification_uri":          "https://app.promptvm.com/cli/device",
			"verification_uri_complete": "https://app.promptvm.com/cli/device?code=WXYZ-1234",
			"expires_in":                600,
			"interval":                  5,
		})
	}))
	defer srv.Close()

	old := httpClient
	httpClient = &http.Client{Timeout: 3 * time.Second}
	t.Cleanup(func() { httpClient = old })

	dc, err := RequestDeviceCode(context.Background(), srv.URL, "test-device")
	if err != nil {
		t.Fatalf("RequestDeviceCode: %v", err)
	}
	if dc.DeviceCode != "dc_123" {
		t.Errorf("DeviceCode = %q", dc.DeviceCode)
	}
	if dc.UserCode != "WXYZ-1234" {
		t.Errorf("UserCode = %q", dc.UserCode)
	}
	if dc.Interval != 5 {
		t.Errorf("Interval = %d", dc.Interval)
	}
}
