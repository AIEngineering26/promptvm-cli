package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AIEngineering26/promptvm-cli/internal/config"
	"github.com/AIEngineering26/promptvm-cli/internal/oauth"
	sdkclient "github.com/AIEngineering26/promptvm-go-sdk/client"
	"github.com/AIEngineering26/promptvm-go-sdk/option"
	"github.com/spf13/cobra"
)

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current authentication state",
	Long: `Show the active profile, auth type, public-key prefix, base URL, and
organization. The secret key is never printed — not full, not truncated,
not redacted-with-asterisks.`,
	RunE: runAuthStatus,
}

// authStatus is the machine-readable shape emitted by `auth status -o json`.
//
// Notably absent: any field carrying the secret key. The secret is loaded
// into memory to drive the connectivity check, but it is never marshaled
// into the JSON output, never logged, and never echoed back to the user.
type authStatus struct {
	Profile         string     `json:"profile"`
	AuthType        string     `json:"auth_type"` // "api-key" | "oauth"
	PublicKeyPrefix string     `json:"public_key_prefix,omitempty"`
	Organization    string     `json:"organization,omitempty"`
	Scopes          []string   `json:"scopes,omitempty"`
	BaseURL         string     `json:"base_url"`
	Environment     string     `json:"environment,omitempty"`
	UserEmail       string     `json:"user_email,omitempty"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	Authenticated   bool       `json:"authenticated"`
	Error           string     `json:"error,omitempty"`
}

func runAuthStatus(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if cfg.ActiveProfile == "" {
		fmt.Fprintln(out, "No active profile. Run `promptvm auth login` to authenticate.")
		return nil
	}

	profile, err := cfg.ActiveProfileData()
	if err != nil {
		// Profile referenced but missing on disk — still emit a
		// structured response so JSON consumers don't choke.
		st := authStatus{
			Profile: cfg.ActiveProfile,
			Error:   "profile not found",
		}
		return emitAuthStatus(cmd, st)
	}

	st := authStatus{
		Profile:      profile.Name,
		BaseURL:      profile.BaseURL,
		Environment:  profile.Environment,
		Organization: profile.Organization,
	}
	if profile.IsOAuth() {
		st.AuthType = "oauth"
		st.UserEmail = profile.UserEmail
		if !profile.ExpiresAt.IsZero() {
			exp := profile.ExpiresAt
			st.ExpiresAt = &exp
		}
	} else {
		st.AuthType = "api-key"
		st.PublicKeyPrefix = publicKeyPrefix(profile)
	}

	// Validate the credentials are still active by making a lightweight call.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	token, err := oauth.AccessTokenForProfile(ctx, profile)
	if err != nil {
		st.Error = err.Error()
		return emitAuthStatus(cmd, st)
	}

	opts := []option.RequestOption{
		option.WithBaseURL(profile.BaseURL),
	}
	if profile.IsOAuth() {
		// OAuth tokens are sent as standard `Authorization: Bearer <jwt>`.
		// option.WithAPIKey was removed in go-sdk v1, so we set the
		// header explicitly via WithHTTPHeader (same pattern as
		// client.NewFromContext).
		h := make(http.Header)
		h.Set("Authorization", "Bearer "+token)
		opts = append(opts, option.WithHTTPHeader(h))
	} else {
		// Use the dual-header path for api-key profiles so the live
		// check exercises the same code path as ordinary CLI calls.
		pk, sk := credentialPair(profile)
		opts = append(opts, option.WithCredentials(pk, sk))
	}
	client := sdkclient.NewClient(opts...)

	if _, err := validateAPIKey(client); err != nil {
		st.Error = err.Error()
	} else {
		st.Authenticated = true
	}
	return emitAuthStatus(cmd, st)
}

// publicKeyPrefix returns the first 12 characters of the profile's
// public key (typically `pk_<8 hex>`) for display, or an empty string
// if the profile has no public key on file.
func publicKeyPrefix(p *config.Profile) string {
	pk, _ := credentialPair(p)
	if pk == "" {
		return ""
	}
	if len(pk) <= 12 {
		return pk
	}
	return pk[:12]
}

// credentialPair returns the (public, secret) key pair for an api-key
// profile. It prefers the modern dual-key fields and falls back to
// splitting the legacy `api_key: pk:sk` form for profiles that have not
// yet been migrated. If only the public half is present (the secret
// having been wiped or never persisted), the public key is returned
// alone — the secret slot stays empty.
func credentialPair(p *config.Profile) (string, string) {
	if p == nil {
		return "", ""
	}
	if p.PublicKey != "" {
		return p.PublicKey, p.SecretKey
	}
	if p.APIKey != "" {
		parts := strings.Split(p.APIKey, ":")
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
	}
	return "", ""
}

// emitAuthStatus dispatches to the table or JSON formatter based on
// `-o`. It is the only place that writes to the command's stdout.
func emitAuthStatus(cmd *cobra.Command, st authStatus) error {
	out := cmd.OutOrStdout()
	format, _ := cmd.Flags().GetString("output")
	if format == "json" {
		compact, _ := cmd.Flags().GetBool("compact")
		return writeStatusJSON(out, st, compact)
	}
	return writeStatusTable(out, st)
}

func writeStatusJSON(w io.Writer, st authStatus, compact bool) error {
	enc := json.NewEncoder(w)
	if !compact {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(st)
}

// writeStatusTable prints a human-readable summary. It MUST never write
// the secret key — not whole, not truncated, not asterisk-redacted.
func writeStatusTable(w io.Writer, st authStatus) error {
	fmt.Fprintf(w, "Profile:      %s\n", st.Profile)
	if st.Error != "" && st.AuthType == "" {
		fmt.Fprintf(w, "Status:       %s\n", st.Error)
		return nil
	}
	if st.AuthType == "oauth" {
		fmt.Fprintln(w, "Auth type:    oauth")
		if st.UserEmail != "" {
			fmt.Fprintf(w, "User:         %s\n", st.UserEmail)
		}
		if st.ExpiresAt != nil && !st.ExpiresAt.IsZero() {
			fmt.Fprintf(w, "Expires at:   %s\n", st.ExpiresAt.Format(time.RFC3339))
		}
	} else {
		fmt.Fprintln(w, "Auth type:    api-key")
		if st.PublicKeyPrefix != "" {
			// Show the first 12 chars of the public key (e.g. pk_554f77dcd1).
			// The public key is non-secret; the secret key is never
			// rendered into this output under any flag combination.
			fmt.Fprintf(w, "Public key:   %s…\n", st.PublicKeyPrefix)
		}
	}
	fmt.Fprintf(w, "Base URL:     %s\n", st.BaseURL)
	if st.Environment != "" {
		fmt.Fprintf(w, "Environment:  %s\n", st.Environment)
	}
	if st.Organization != "" {
		fmt.Fprintf(w, "Organization: %s\n", st.Organization)
	}
	if len(st.Scopes) > 0 {
		fmt.Fprintf(w, "Scopes:       %s\n", strings.Join(st.Scopes, ","))
	}
	if st.Authenticated {
		fmt.Fprintln(w, "Status:       authenticated")
	} else if st.Error != "" {
		fmt.Fprintf(w, "Status:       %s\n", st.Error)
	}
	return nil
}
