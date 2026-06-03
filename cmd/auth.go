package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	sdkclient "github.com/AIEngineering26/promptvm-go-sdk/client"
	"github.com/AIEngineering26/promptvm-go-sdk/option"
	"github.com/AIEngineering26/promptvm-cli/internal/config"
	"github.com/AIEngineering26/promptvm-cli/internal/oauth"
	"github.com/AIEngineering26/promptvm-cli/internal/prompt"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication",
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with PromptVM",
	Long: `Authenticate with PromptVM.

By default, opens a browser to sign in via the PromptVM web app and
stores a scoped CLI token in the OS keychain (OAuth/SSO flow).

Use --device on headless machines: a user code is printed and you
complete authorization in a browser on another device (RFC 8628).

For non-interactive script use, pass --public-key and --secret-key
(or the deprecated combined --api-key pk_…:sk_… form). When only
--public-key is supplied, the CLI prompts for the matching secret
with masked input so the value never leaks into shell history.`,
	RunE: runAuthLogin,
}

func init() {
	rootCmd.AddCommand(authCmd)
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authLogoutCmd)
	authCmd.AddCommand(authStatusCmd)
	authCmd.AddCommand(authSessionsCmd)

	// Login flags. The dual public-key/secret-key inputs are local to
	// this subcommand so `promptvm auth login --help` lists them
	// directly under "Flags:" rather than at the bottom under
	// "Global Flags:". They mirror the root persistent flags and are
	// resolved via the same credential precedence table in
	// internal/client.ResolveCredentials.
	authLoginCmd.Flags().String("public-key", "", "API public key (pk_…); paired with --secret-key")
	authLoginCmd.Flags().String("secret-key", "", "API secret key (sk_…); prompts interactively if --public-key is given without it")
	authLoginCmd.Flags().String("api-key", "", "Combined API key in pk_xxx:sk_xxx form (DEPRECATED: prefer --public-key/--secret-key)")
	authLoginCmd.Flags().String("profile", "", `Profile name (default: "default")`)
	authLoginCmd.Flags().String("base-url", "", "Custom API base URL")
	authLoginCmd.Flags().String("app-url", "", "Web app base URL (overrides autodetection)")
	_ = authLoginCmd.Flags().MarkHidden("app-url")
	authLoginCmd.Flags().Bool("device", false, "Use the device authorization grant (RFC 8628) — best for headless / SSH / CI")
	authLoginCmd.Flags().Bool("no-browser", false, "Alias for --device")

	// Logout flags.
	authLogoutCmd.Flags().String("profile", "", "Profile to remove (default: active profile)")
	authLogoutCmd.Flags().Bool("all", false, "Remove all profiles")
}

// runAuthLogin dispatches to one of three backend flows depending on
// the user's flags and environment:
//
//	--public-key/--secret-key  → dual-key API-key flow (preferred)
//	--api-key pk_…:sk_…        → legacy combined API-key flow (deprecated)
//	--device | --no-browser | PROMPTVM_HEADLESS=1 → device code flow
//	(default)                  → browser-based PKCE flow
//
// When only --public-key is provided (no matching --secret-key), the
// CLI prompts interactively for the secret with masked input, so the
// secret value never has to land in shell history.
func runAuthLogin(cmd *cobra.Command, _ []string) error {
	profileName, _ := cmd.Flags().GetString("profile")
	publicKey, _ := cmd.Flags().GetString("public-key")
	secretKey, _ := cmd.Flags().GetString("secret-key")
	apiKey, _ := cmd.Flags().GetString("api-key")

	// Dual-key flow: --public-key / --secret-key.
	// If only --public-key is provided, prompt for the secret with
	// hidden input via the `huh` TUI. This avoids putting the secret
	// in shell history and keeps the dual-flag form ergonomic.
	if publicKey != "" || secretKey != "" {
		if publicKey == "" {
			return fmt.Errorf("--public-key is required when --secret-key is set")
		}
		if secretKey == "" {
			val, err := prompt.MaskedInput("Enter your secret key (sk_…)")
			if err != nil {
				return err
			}
			secretKey = strings.TrimSpace(val)
			if secretKey == "" {
				return fmt.Errorf("--secret-key is required")
			}
		}
		return runDualKeyLogin(cmd, publicKey, secretKey, profileName)
	}

	if apiKey != "" {
		return runAPIKeyLogin(cmd, apiKey, profileName)
	}

	useDevice, _ := cmd.Flags().GetBool("device")
	noBrowser, _ := cmd.Flags().GetBool("no-browser")
	if os.Getenv("PROMPTVM_HEADLESS") == "1" {
		useDevice = true
	}
	if noBrowser {
		useDevice = true
	}

	if !useDevice && isLikelyHeadless() {
		fmt.Fprintln(cmd.ErrOrStderr(), "It looks like you're in a headless session.")
		fmt.Fprintln(cmd.ErrOrStderr(), "Consider `promptvm auth login --device` instead.")
		fmt.Fprintln(cmd.ErrOrStderr())
	}

	if useDevice {
		return runDeviceLogin(cmd, profileName)
	}
	return runBrowserLogin(cmd, profileName)
}

// runDualKeyLogin validates a (publicKey, secretKey) pair against the
// API and persists it to the named profile in dual-key form. Unlike
// runAPIKeyLogin, this path never stores the legacy `api_key: pk:sk`
// combined string — new profiles are written with `public_key` and
// `secret_key` directly.
func runDualKeyLogin(cmd *cobra.Command, publicKey, secretKey, profileName string) error {
	if !strings.HasPrefix(publicKey, "pk_") {
		return fmt.Errorf("--public-key must start with 'pk_'")
	}
	if !strings.HasPrefix(secretKey, "sk_") {
		return fmt.Errorf("--secret-key must start with 'sk_'")
	}

	baseURL, _ := cmd.Flags().GetString("base-url")
	if baseURL == "" {
		baseURL = resolveLoginBaseURL(cmd)
	}

	if profileName == "" {
		profileName = "default"
	}
	if err := config.ValidateProfileName(profileName); err != nil {
		return err
	}

	fmt.Fprint(cmd.OutOrStdout(), "Validating key... ")

	opts := []option.RequestOption{
		option.WithCredentials(publicKey, secretKey),
		option.WithBaseURL(baseURL),
	}
	client := sdkclient.NewClient(opts...)

	info, err := validateAPIKey(client)
	if err != nil {
		fmt.Fprintln(cmd.OutOrStdout(), "✗")
		return fmt.Errorf("API key validation failed: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "✓")

	env := "live"
	if len(publicKey) >= 8 && publicKey[3:7] == "test" {
		env = "test"
	}
	if info.environment != "" {
		env = info.environment
	}

	profile := &config.Profile{
		Name:         profileName,
		AuthType:     config.AuthTypeAPIKey,
		PublicKey:    publicKey,
		SecretKey:    secretKey,
		BaseURL:      baseURL,
		Environment:  env,
		Organization: info.orgID,
	}

	if err := config.SaveProfile(profile); err != nil {
		return fmt.Errorf("saving profile: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	cfg.ActiveProfile = profileName
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	dir, _ := config.Dir()
	fmt.Fprintf(cmd.OutOrStdout(), "\nProfile %q saved to %s/profiles/%s.yaml\n",
		profileName, dir, profileName)
	fmt.Fprintf(cmd.OutOrStdout(), "Active profile set to %q.\n", profileName)
	return nil
}

// runAPIKeyLogin keeps the old behavior: prompt for key (if missing),
// validate it against the API, then persist it under the chosen profile.
func runAPIKeyLogin(cmd *cobra.Command, apiKey, profileName string) error {
	baseURL, _ := cmd.Flags().GetString("base-url")

	if apiKey == "" {
		prompt := promptui.Prompt{
			Label: "Enter your API key",
			Mask:  rune('*'),
			Validate: func(input string) error {
				if len(input) < 10 {
					return fmt.Errorf("API key too short")
				}
				return nil
			},
		}
		var err error
		apiKey, err = prompt.Run()
		if err != nil {
			return fmt.Errorf("prompt cancelled: %w", err)
		}
	}

	if profileName == "" {
		prompt := promptui.Prompt{
			Label:   "Give this profile a name",
			Default: "default",
		}
		var err error
		profileName, err = prompt.Run()
		if err != nil {
			return fmt.Errorf("prompt cancelled: %w", err)
		}
		if profileName == "" {
			profileName = "default"
		}
	}

	if baseURL == "" {
		baseURL = "https://dev-api.promptvm.ai"
	}

	fmt.Print("Validating key... ")

	opts := []option.RequestOption{
		option.WithBaseURL(baseURL),
	}
	// If the user passed a pk:sk pair, use the dual-header SDK option
	// so the validation call exercises the same wire format as ordinary
	// CLI traffic. Fall back to a bearer-style header for legacy
	// `pvk_…` long-lived tokens — option.WithAPIKey was removed in
	// go-sdk v1 (it now panics with a migration message).
	if pk, sk, err := splitPKSK(apiKey); err == nil {
		opts = append(opts, option.WithCredentials(pk, sk))
	} else {
		h := make(http.Header)
		h.Set("Authorization", "Bearer "+apiKey)
		opts = append(opts, option.WithHTTPHeader(h))
	}
	client := sdkclient.NewClient(opts...)

	info, err := validateAPIKey(client)
	if err != nil {
		fmt.Println("✗")
		return fmt.Errorf("API key validation failed: %w", err)
	}
	fmt.Println("✓")

	if info.email != "" {
		fmt.Printf("Authenticated as %s", info.email)
		if info.org != "" {
			fmt.Printf(" (%s)", info.org)
		}
		fmt.Println()
	}
	if info.environment != "" {
		fmt.Printf("Environment: %s\n", info.environment)
	}
	if info.scopes != "" {
		fmt.Printf("Scopes: %s\n", info.scopes)
	}

	// Determine environment from key prefix (pk_test_... vs pk_live_...)
	env := "live"
	if len(apiKey) >= 8 && apiKey[3:7] == "test" {
		env = "test"
	}
	if info.environment != "" {
		env = info.environment
	}

	profile := &config.Profile{
		Name:         profileName,
		AuthType:     config.AuthTypeAPIKey,
		BaseURL:      baseURL,
		Environment:  env,
		Organization: info.orgID,
	}
	// Store credentials in dual-key form when the input is a properly
	// formatted pk_…:sk_… pair. Otherwise, fall back to the legacy
	// single-string field for backward compatibility with profiles
	// that pre-date the dual-key migration (e.g. older `pvk_…` tokens).
	if pk, sk, perr := splitPKSK(apiKey); perr == nil {
		profile.PublicKey = pk
		profile.SecretKey = sk
	} else {
		profile.APIKey = apiKey
	}

	if err := config.SaveProfile(profile); err != nil {
		return fmt.Errorf("saving profile: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	cfg.ActiveProfile = profileName
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	dir, _ := config.Dir()
	fmt.Printf("\nProfile %q saved to %s/profiles/%s.yaml\n", profileName, dir, profileName)
	fmt.Printf("Active profile set to %q.\n", profileName)
	return nil
}

type validationInfo struct {
	email       string
	org         string
	orgID       string
	environment string
	scopes      string
}

// splitPKSK parses a "pk_xxx:sk_xxx" combined credential into its two
// halves. Used by the legacy --api-key login path to migrate inputs to
// the dual-key profile shape on save. Returns an error if the input is
// not exactly a pk:sk pair with the documented prefixes.
func splitPKSK(combined string) (string, string, error) {
	parts := strings.Split(combined, ":")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("not a pk:sk pair")
	}
	pk, sk := parts[0], parts[1]
	if !strings.HasPrefix(pk, "pk_") || !strings.HasPrefix(sk, "sk_") {
		return "", "", fmt.Errorf("missing pk_/sk_ prefixes")
	}
	return pk, sk, nil
}

// validateAPIKey calls a lightweight authenticated endpoint to verify the key.
// It uses the marketplace categories list as a low-cost authenticated check.
func validateAPIKey(client *sdkclient.Client) (*validationInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := client.MarketplaceBrowse.ListMarketplaceCategories(ctx); err != nil {
		return nil, err
	}
	return &validationInfo{}, nil
}

// saveOAuthProfile persists tokens to the keychain and writes an OAuth
// profile YAML with only metadata (no secrets in the file).
func saveOAuthProfile(profileName, baseURL string, tokens *oauth.TokenResponse) error {
	if profileName == "" {
		profileName = "default"
	}
	if err := config.ValidateProfileName(profileName); err != nil {
		return err
	}

	stored := &oauth.StoredTokens{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresAt:    tokens.ExpiresAt,
	}
	if err := oauth.SaveTokens(profileName, stored); err != nil {
		return fmt.Errorf("saving tokens: %w", err)
	}

	env := "live"
	if strings.Contains(baseURL, "staging") {
		env = "staging"
	}

	var (
		orgID     string
		userID    string
		userEmail string
	)
	if tokens.Organization != nil {
		orgID = tokens.Organization.ID
	}
	if tokens.User != nil {
		userID = tokens.User.ID
		userEmail = tokens.User.Email
	}

	profile := &config.Profile{
		Name:         profileName,
		AuthType:     config.AuthTypeOAuth,
		BaseURL:      baseURL,
		Environment:  env,
		Organization: orgID,
		TokenRef:     "promptvm-cli:" + profileName,
		ExpiresAt:    tokens.ExpiresAt,
		UserID:       userID,
		UserEmail:    userEmail,
	}
	if err := config.SaveProfile(profile); err != nil {
		return fmt.Errorf("saving profile: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	cfg.ActiveProfile = profileName
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Println()
	orgSlug := ""
	if tokens.Organization != nil {
		orgSlug = tokens.Organization.Slug
	}
	if userEmail != "" {
		if orgSlug != "" {
			fmt.Printf("✓ Authenticated as %s (%s)\n", userEmail, orgSlug)
		} else {
			fmt.Printf("✓ Authenticated as %s\n", userEmail)
		}
	} else {
		fmt.Println("✓ Authenticated")
	}
	fmt.Printf("Active profile set to %q.\n", profileName)
	return nil
}

// resolveBaseURL returns the API base URL honoring the standard
// precedence: --base-url flag → PROMPTVM_BASE_URL → default.
func resolveLoginBaseURL(cmd *cobra.Command) string {
	if v, _ := cmd.Flags().GetString("base-url"); v != "" {
		return v
	}
	if v := os.Getenv("PROMPTVM_BASE_URL"); v != "" {
		return v
	}
	return "https://dev-api.promptvm.ai"
}

// resolveAppURL returns the web app base URL by checking the --app-url
// flag, then PROMPTVM_APP_URL, then deriving it from the API base URL,
// then the default.
func resolveAppURL(cmd *cobra.Command, apiBaseURL string) string {
	if v, _ := cmd.Flags().GetString("app-url"); v != "" {
		return strings.TrimRight(v, "/")
	}
	if v := os.Getenv("PROMPTVM_APP_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	if derived := deriveAppURL(apiBaseURL); derived != "" {
		return derived
	}
	return "https://dev-app.promptvm.ai"
}

// deriveAppURL swaps "api" for "app" in the host label of known
// promptvm hostnames (e.g. dev-api.promptvm.ai → dev-app.promptvm.ai,
// api.promptvm.com → app.promptvm.com). Returns "" if no match.
func deriveAppURL(apiBaseURL string) string {
	if apiBaseURL == "" {
		return ""
	}
	u, err := url.Parse(apiBaseURL)
	if err != nil || u.Host == "" {
		return ""
	}
	host := u.Host
	if strings.HasPrefix(host, "dev-api.") {
		host = "dev-app." + strings.TrimPrefix(host, "dev-api.")
		return u.Scheme + "://" + host
	}
	if strings.HasPrefix(host, "staging-api.") {
		host = "staging-app." + strings.TrimPrefix(host, "staging-api.")
		return u.Scheme + "://" + host
	}
	if strings.HasPrefix(host, "api.") {
		host = "app." + strings.TrimPrefix(host, "api.")
		return u.Scheme + "://" + host
	}
	return ""
}

// isLikelyHeadless returns true if the current process appears to be
// running without a usable display (SSH, CI, Codespaces).
func isLikelyHeadless() bool {
	if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_CLIENT") != "" {
		return true
	}
	if os.Getenv("CI") != "" {
		return true
	}
	if os.Getenv("CODESPACES") != "" {
		return true
	}
	return false
}

// deviceName returns a short, human-friendly label for this device,
// which is passed to the authorization server so the user can tell
// different CLIs apart in the authorized-devices list later.
func deviceName() string {
	if v := os.Getenv("PROMPTVM_DEVICE_NAME"); v != "" {
		return v
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "promptvm-cli"
	}
	return "promptvm-cli @ " + host
}
