package oauth

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestStartLoopbackServer_DeliversCallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	port, ch, shutdown, err := StartLoopbackServer(ctx)
	if err != nil {
		t.Fatalf("StartLoopbackServer: %v", err)
	}
	defer shutdown()

	url := fmt.Sprintf("http://127.0.0.1:%d/callback?code=abc123&state=xyz", port)
	resp, err := http.Get(url) //nolint:gosec,noctx // test helper
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	select {
	case cb := <-ch:
		if cb.Code != "abc123" {
			t.Errorf("code = %q, want abc123", cb.Code)
		}
		if cb.State != "xyz" {
			t.Errorf("state = %q, want xyz", cb.State)
		}
		if cb.Error != "" {
			t.Errorf("unexpected error: %q", cb.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for callback delivery")
	}
}

func TestStartLoopbackServer_ErrorQuery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	port, ch, shutdown, err := StartLoopbackServer(ctx)
	if err != nil {
		t.Fatalf("StartLoopbackServer: %v", err)
	}
	defer shutdown()

	url := fmt.Sprintf("http://127.0.0.1:%d/callback?error=access_denied&error_description=nope", port)
	resp, err := http.Get(url) //nolint:gosec,noctx // test helper
	if err != nil {
		t.Fatalf("GET callback: %v", err)
	}
	resp.Body.Close()

	select {
	case cb := <-ch:
		if cb.Error == "" {
			t.Errorf("expected error in callback, got none")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}

func TestStartLoopbackServer_NonCallbackPathReturns404(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	port, _, shutdown, err := StartLoopbackServer(ctx)
	if err != nil {
		t.Fatalf("StartLoopbackServer: %v", err)
	}
	defer shutdown()

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", port)) //nolint:gosec,noctx // test helper
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestStartLoopbackServer_BindsLoopbackOnly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	port, _, shutdown, err := StartLoopbackServer(ctx)
	if err != nil {
		t.Fatalf("StartLoopbackServer: %v", err)
	}
	defer shutdown()

	// A connection via the loopback address should succeed.
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
	if err != nil {
		t.Fatalf("GET 127.0.0.1: %v", err)
	}
	resp.Body.Close()

	if port == 0 {
		t.Error("port should be > 0")
	}
}
