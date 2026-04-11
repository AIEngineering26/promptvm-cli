package client

import (
	"context"
	"fmt"
	"os"
	"time"

	sdkclient "github.com/AIEngineering26/promptvm-go-sdk/client"
	"github.com/AIEngineering26/promptvm-go-sdk/option"
	"github.com/AIEngineering26/promptvm-cli/internal/config"
	"github.com/AIEngineering26/promptvm-cli/internal/oauth"
	"github.com/spf13/cobra"
)

const (
	defaultBaseURL = "https://api.promptvm.com"
	envAPIKey      = "PROMPTVM_API_KEY"
	envBaseURL     = "PROMPTVM_BASE_URL"
)

// NewFromContext creates an SDK client from CLI context.
// Resolution order: flag → environment variable → config file → default.
//
// For OAuth profiles the access token is loaded from the keychain and
// auto-refreshed if it has expired.
func NewFromContext(cmd *cobra.Command) (*sdkclient.Client, error) {
	token, err := resolveToken(cmd)
	if err != nil {
		return nil, err
	}

	baseURL := resolveBaseURL(cmd)

	opts := []option.RequestOption{
		option.WithAPIKey(token),
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	return sdkclient.NewClient(opts...), nil
}

// resolveToken returns the bearer token to send with API requests. For
// OAuth profiles this dynamically loads and refreshes tokens from the
// keychain; for legacy api_key profiles it returns the stored key.
func resolveToken(cmd *cobra.Command) (string, error) {
	// 1. Flag
	if key, _ := cmd.Flags().GetString("api-key"); key != "" {
		return key, nil
	}

	// 2. Environment variable
	if key := os.Getenv(envAPIKey); key != "" {
		return key, nil
	}

	// 3. Active profile (API key or OAuth)
	if profile := activeProfile(); profile != nil {
		if profile.IsOAuth() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return oauth.AccessTokenForProfile(ctx, profile)
		}
		if profile.APIKey != "" {
			return profile.APIKey, nil
		}
	}

	return "", fmt.Errorf("API key required: set --api-key flag, %s env var, or run `promptvm auth login`", envAPIKey)
}

func resolveBaseURL(cmd *cobra.Command) string {
	// 1. Flag
	if url, _ := cmd.Flags().GetString("base-url"); url != "" {
		return url
	}

	// 2. Environment variable
	if url := os.Getenv(envBaseURL); url != "" {
		return url
	}

	// 3. Config file (active profile)
	if profile := activeProfile(); profile != nil && profile.BaseURL != "" {
		return profile.BaseURL
	}

	return defaultBaseURL
}

// activeProfile loads the active profile from config, returning nil on any error.
func activeProfile() *config.Profile {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	profile, err := cfg.ActiveProfileData()
	if err != nil {
		return nil
	}
	return profile
}
