package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	sdkclient "github.com/AIEngineering26/promptvm-go-sdk/client"
	"github.com/AIEngineering26/promptvm-go-sdk/option"
	"github.com/AIEngineering26/promptvm-cli/internal/config"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage API key authentication",
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with an API key",
	Long:  "Interactively enter and validate an API key, then save it as a named profile.",
	RunE:  runAuthLogin,
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove stored credentials",
	RunE:  runAuthLogout,
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current authentication state",
	RunE:  runAuthStatus,
}

func init() {
	rootCmd.AddCommand(authCmd)
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authLogoutCmd)
	authCmd.AddCommand(authStatusCmd)

	authLoginCmd.Flags().String("api-key", "", "API key (non-interactive mode)")
	authLoginCmd.Flags().String("profile", "", "Profile name (default: \"default\")")
	authLoginCmd.Flags().String("base-url", "", "Custom API base URL")

	authLogoutCmd.Flags().String("profile", "", "Profile to remove (default: active profile)")
	authLogoutCmd.Flags().Bool("all", false, "Remove all profiles")
}

func runAuthLogin(cmd *cobra.Command, args []string) error {
	apiKey, _ := cmd.Flags().GetString("api-key")
	profileName, _ := cmd.Flags().GetString("profile")
	baseURL, _ := cmd.Flags().GetString("base-url")

	// Interactive API key input
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

	// Interactive profile name input
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
		baseURL = "https://api.promptvm.com"
	}

	// Validate the key against the API
	fmt.Print("Validating key... ")

	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithBaseURL(baseURL),
	}
	client := sdkclient.NewClient(opts...)

	info, err := validateAPIKey(client)
	if err != nil {
		fmt.Println("✗")
		return fmt.Errorf("API key validation failed: %w", err)
	}
	fmt.Println("✓")

	// Display validation results
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

	// Save profile
	profile := &config.Profile{
		Name:         profileName,
		APIKey:       apiKey,
		BaseURL:      baseURL,
		Environment:  env,
		Organization: info.orgID,
	}

	if err := config.SaveProfile(profile); err != nil {
		return fmt.Errorf("saving profile: %w", err)
	}

	// Set as active profile
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

func runAuthLogout(cmd *cobra.Command, args []string) error {
	removeAll, _ := cmd.Flags().GetBool("all")

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if removeAll {
		profiles, err := config.ListProfiles()
		if err != nil {
			return err
		}
		for _, p := range profiles {
			if err := config.DeleteProfile(p.Name); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not remove profile %q: %v\n", p.Name, err)
			} else {
				fmt.Printf("Removed profile %q.\n", p.Name)
			}
		}
		return nil
	}

	profileName, _ := cmd.Flags().GetString("profile")
	if profileName == "" {
		profileName = cfg.ActiveProfile
	}

	if err := config.DeleteProfile(profileName); err != nil {
		return err
	}

	fmt.Printf("Removed profile %q.\n", profileName)

	// If we removed the active profile, clear it
	if profileName == cfg.ActiveProfile {
		cfg.ActiveProfile = ""
		return cfg.Save()
	}
	return nil
}

func runAuthStatus(cmd *cobra.Command, args []string) error {
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
	fmt.Printf("API Key:      %s\n", config.MaskAPIKey(profile.APIKey))
	fmt.Printf("Base URL:     %s\n", profile.BaseURL)
	fmt.Printf("Environment:  %s\n", profile.Environment)
	if profile.Organization != "" {
		fmt.Printf("Organization: %s\n", profile.Organization)
	}

	// Validate the key is still active
	opts := []option.RequestOption{
		option.WithAPIKey(profile.APIKey),
		option.WithBaseURL(profile.BaseURL),
	}
	client := sdkclient.NewClient(opts...)

	_, err = validateAPIKey(client)
	if err != nil {
		fmt.Printf("Status:       ✗ Invalid (%v)\n", err)
	} else {
		fmt.Printf("Status:       ✓ Authenticated\n")
	}

	return nil
}
