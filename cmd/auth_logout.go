package cmd

import (
	"fmt"
	"os"

	"github.com/AIEngineering26/promptvm-cli/internal/config"
	"github.com/AIEngineering26/promptvm-cli/internal/oauth"
	"github.com/spf13/cobra"
)

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove stored credentials",
	Long: `Remove the stored profile and any associated tokens.

For OAuth/SSO profiles, this also deletes the access and refresh
tokens from the OS keychain so no secrets are left behind.`,
	RunE: runAuthLogout,
}

func runAuthLogout(cmd *cobra.Command, _ []string) error {
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
			if err := deleteOneProfile(p); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not remove profile %q: %v\n", p.Name, err)
			} else {
				fmt.Printf("Removed profile %q.\n", p.Name)
			}
		}
		cfg.ActiveProfile = ""
		return cfg.Save()
	}

	profileName, _ := cmd.Flags().GetString("profile")
	if profileName == "" {
		profileName = cfg.ActiveProfile
	}
	if profileName == "" {
		return fmt.Errorf("no active profile; specify --profile <name> or --all")
	}

	// Load first so we know whether this is an OAuth profile; don't fail
	// the whole command if the YAML is missing — we still want to scrub
	// any stray keychain entries that might exist.
	p, loadErr := config.LoadProfile(profileName)
	if loadErr == nil {
		if err := deleteOneProfile(p); err != nil {
			return err
		}
	} else {
		if err := config.DeleteProfile(profileName); err != nil {
			return err
		}
		// Best-effort: try to scrub keychain items in case they exist.
		_ = oauth.DeleteTokens(profileName)
	}

	fmt.Printf("Removed profile %q.\n", profileName)

	if profileName == cfg.ActiveProfile {
		cfg.ActiveProfile = ""
		return cfg.Save()
	}
	return nil
}

// deleteOneProfile scrubs both the YAML file and any keychain tokens.
func deleteOneProfile(p *config.Profile) error {
	if p == nil {
		return fmt.Errorf("nil profile")
	}
	// Always attempt keychain deletion — even legacy api_key profiles
	// can have stale entries from a prior SSO login under the same name.
	_ = oauth.DeleteTokens(p.Name)
	return config.DeleteProfile(p.Name)
}
