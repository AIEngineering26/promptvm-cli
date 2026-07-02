package cmd

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/AIEngineering26/promptvm-cli/internal/oauth"
	"github.com/spf13/cobra"
)

// browserLoginTimeout bounds how long we wait for the user to complete
// authorization in the browser before giving up and shutting down the
// loopback server.
const browserLoginTimeout = 5 * time.Minute

// runBrowserLogin runs the default login flow: spin up a loopback
// redirect server, open a browser to the authorize URL, then exchange
// the returned code for a token pair using PKCE.
func runBrowserLogin(cmd *cobra.Command, profileName string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), browserLoginTimeout)
	defer cancel()

	baseURL := resolveLoginBaseURL(cmd)
	appURL := resolveAppURL(cmd, baseURL)

	if profileName == "" {
		profileName = "default"
	}

	verifier, err := oauth.GenerateVerifier()
	if err != nil {
		return err
	}
	challenge := oauth.Challenge(verifier)
	state, err := oauth.NewState()
	if err != nil {
		return err
	}

	port, callback, shutdown, err := oauth.StartLoopbackServer(ctx)
	if err != nil {
		return fmt.Errorf("starting loopback server: %w", err)
	}
	defer shutdown()

	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)
	authURL := buildAuthURL(appURL, challenge, state, redirectURI)

	errOut := cmd.ErrOrStderr()
	fmt.Fprintln(errOut, "Opening browser to authorize the CLI… (press Ctrl+C to cancel)")
	fmt.Fprintln(errOut, "If your browser does not open, visit:")
	fmt.Fprintln(errOut, "  "+authURL)
	fmt.Fprintln(errOut)

	if err := oauth.Open(authURL); err != nil {
		// Non-fatal: the user can still paste the URL.
		fmt.Fprintf(errOut, "(could not open browser automatically: %v)\n", err)
	}

	var cb oauth.Callback
	select {
	case cb = <-callback:
	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("timed out waiting for browser authorization")
		}
		return fmt.Errorf("cancelled")
	}

	if cb.Error != "" {
		return fmt.Errorf("authorization failed: %s", cb.Error)
	}
	if cb.State != state {
		return fmt.Errorf("state mismatch — possible CSRF, aborting")
	}
	if cb.Code == "" {
		return fmt.Errorf("authorization server did not return a code")
	}

	exchangeCtx, exchangeCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer exchangeCancel()
	tokens, err := oauth.ExchangeCode(exchangeCtx, baseURL, cb.Code, verifier, redirectURI)
	if err != nil {
		return fmt.Errorf("exchanging code: %w", err)
	}

	return saveOAuthProfile(cmd, profileName, baseURL, tokens)
}

// buildAuthURL constructs the query-string URL the browser opens to
// authorize the CLI. All parameters are URL-encoded.
func buildAuthURL(appURL, challenge, state, redirectURI string) string {
	q := url.Values{}
	q.Set("client_id", "promptvm-cli")
	q.Set("response_type", "code")
	q.Set("redirect_uri", redirectURI)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	q.Set("scope", "profile")
	q.Set("device_name", deviceName())
	return appURL + "/cli/authorize?" + q.Encode()
}
