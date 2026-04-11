package cmd

import (
	"context"
	"fmt"
	"time"

	sdkclient "github.com/AIEngineering26/promptvm-go-sdk/client"
	"github.com/AIEngineering26/promptvm-go-sdk/option"
	"github.com/AIEngineering26/promptvm-cli/internal/config"
	"github.com/AIEngineering26/promptvm-cli/internal/oauth"
	"github.com/spf13/cobra"
)

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current authentication state",
	RunE:  runAuthStatus,
}

func runAuthStatus(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if cfg.ActiveProfile == "" {
		fmt.Println("No active profile. Run `promptvm auth login` to authenticate.")
		return nil
	}

	profile, err := cfg.ActiveProfileData()
	if err != nil {
		fmt.Printf("Profile:  %s (not found)\n", cfg.ActiveProfile)
		fmt.Println("Status:   ✗ Not authenticated")
		fmt.Println("\nRun `promptvm auth login` to authenticate.")
		return nil
	}

	fmt.Printf("Profile:      %s\n", profile.Name)
	if profile.IsOAuth() {
		fmt.Printf("Auth type:    OAuth (SSO)\n")
		if profile.UserEmail != "" {
			fmt.Printf("User:         %s\n", profile.UserEmail)
		}
		if !profile.ExpiresAt.IsZero() {
			fmt.Printf("Expires at:   %s\n", profile.ExpiresAt.Format(time.RFC3339))
		}
	} else {
		fmt.Printf("Auth type:    API key\n")
		fmt.Printf("API Key:      %s\n", config.MaskAPIKey(profile.APIKey))
	}
	fmt.Printf("Base URL:     %s\n", profile.BaseURL)
	fmt.Printf("Environment:  %s\n", profile.Environment)
	if profile.Organization != "" {
		fmt.Printf("Organization: %s\n", profile.Organization)
	}

	// Validate the credentials are still active by making a lightweight call.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	token, err := oauth.AccessTokenForProfile(ctx, profile)
	if err != nil {
		fmt.Printf("Status:       ✗ %v\n", err)
		return nil
	}

	opts := []option.RequestOption{
		option.WithAPIKey(token),
		option.WithBaseURL(profile.BaseURL),
	}
	client := sdkclient.NewClient(opts...)

	if _, err := validateAPIKey(client); err != nil {
		fmt.Printf("Status:       ✗ Invalid (%v)\n", err)
	} else {
		fmt.Printf("Status:       ✓ Authenticated\n")
	}
	return nil
}
