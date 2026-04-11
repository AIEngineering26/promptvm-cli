package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/AIEngineering26/promptvm-cli/internal/oauth"
	"github.com/spf13/cobra"
)

// deviceLoginTimeout is the hard upper bound on the entire device flow,
// including the user opening their browser on another device.
const deviceLoginTimeout = 20 * time.Minute

// runDeviceLogin runs the RFC 8628 device authorization grant. It prints
// a short user code and a verification URL, attempts to open the URL in
// a browser (non-fatal if that fails), and polls the token endpoint
// until the user completes authorization elsewhere.
func runDeviceLogin(cmd *cobra.Command, profileName string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), deviceLoginTimeout)
	defer cancel()

	baseURL := resolveLoginBaseURL(cmd)
	if profileName == "" {
		profileName = "default"
	}

	dc, err := oauth.RequestDeviceCode(ctx, baseURL, deviceName())
	if err != nil {
		return fmt.Errorf("requesting device code: %w", err)
	}

	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	fmt.Fprintln(out)
	fmt.Fprintf(out, "  Enter this code: %s\n", dc.UserCode)
	fmt.Fprintf(out, "  At this URL:     %s\n", dc.VerificationURI)
	fmt.Fprintln(out)
	if dc.VerificationURIComplete != "" {
		fmt.Fprintln(out, "  (or use: "+dc.VerificationURIComplete+")")
		fmt.Fprintln(out)
	}
	fmt.Fprintln(errOut, "Waiting for authorization…")

	// Try to open a browser at the complete URL — totally optional.
	if dc.VerificationURIComplete != "" {
		_ = oauth.Open(dc.VerificationURIComplete)
	}

	tokens, err := oauth.PollDeviceToken(ctx, baseURL, dc.DeviceCode, dc.Interval)
	if err != nil {
		return err
	}

	return saveOAuthProfile(profileName, baseURL, tokens)
}
