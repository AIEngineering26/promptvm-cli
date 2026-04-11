package oauth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExchangeCode_SendsExpectedPayload(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != cliTokenPath {
			t.Errorf("path = %q, want %q", r.URL.Path, cliTokenPath)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}

		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "at_new",
			"refresh_token": "rt_new",
			"expires_in":    3600,
			"user": map[string]any{
				"id":    "u_1",
				"email": "alex@example.com",
			},
			"organization": map[string]any{
				"id":   "o_1",
				"name": "Acme",
				"slug": "acme",
			},
		})
	}))
	defer srv.Close()

	// Swap in a client with a short timeout for tests.
	old := httpClient
	httpClient = &http.Client{Timeout: 3 * time.Second}
	t.Cleanup(func() { httpClient = old })

	tr, err := ExchangeCode(context.Background(), srv.URL, "code-123", "verifier-abc", "http://127.0.0.1:9999/callback")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}

	if tr.AccessToken != "at_new" {
		t.Errorf("AccessToken = %q, want at_new", tr.AccessToken)
	}
	if tr.RefreshToken != "rt_new" {
		t.Errorf("RefreshToken = %q, want rt_new", tr.RefreshToken)
	}
	if tr.User == nil || tr.User.Email != "alex@example.com" {
		t.Errorf("User.Email = %+v", tr.User)
	}
	if tr.Organization == nil || tr.Organization.Slug != "acme" {
		t.Errorf("Organization = %+v", tr.Organization)
	}
	if tr.ExpiresAt.IsZero() {
		t.Errorf("ExpiresAt should be populated from expires_in")
	}

	wantFields := map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     "promptvm-cli",
		"code":          "code-123",
		"code_verifier": "verifier-abc",
		"redirect_uri":  "http://127.0.0.1:9999/callback",
	}
	for k, v := range wantFields {
		if gotBody[k] != v {
			t.Errorf("body[%q] = %q, want %q", k, gotBody[k], v)
		}
	}
}

func TestExchangeCode_OAuthErrorParsed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":             "invalid_grant",
			"error_description": "code is expired",
		})
	}))
	defer srv.Close()

	old := httpClient
	httpClient = &http.Client{Timeout: 3 * time.Second}
	t.Cleanup(func() { httpClient = old })

	_, err := ExchangeCode(context.Background(), srv.URL, "c", "v", "r")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("error = %v, want invalid_grant", err)
	}
}
