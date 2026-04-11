package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/AIEngineering26/promptvm-cli/internal/config"
	"github.com/spf13/cobra"
)

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage authentication profiles",
}

var profileListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all profiles",
	RunE:  runProfileList,
}

var profileUseCmd = &cobra.Command{
	Use:   "use <name>",
	Short: "Switch active profile",
	Args:  cobra.ExactArgs(1),
	RunE:  runProfileUse,
}

func init() {
	rootCmd.AddCommand(profileCmd)
	profileCmd.AddCommand(profileListCmd)
	profileCmd.AddCommand(profileUseCmd)
}

func runProfileList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	profiles, err := config.ListProfiles()
	if err != nil {
		return err
	}

	if len(profiles) == 0 {
		fmt.Println("No profiles found. Run `promptvm auth login` to create one.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "  NAME\tENVIRONMENT\tORG\tSTATUS")
	for _, p := range profiles {
		marker := " "
		status := ""
		if p.Name == cfg.ActiveProfile {
			marker = "*"
			status = "active"
		}
		fmt.Fprintf(w, "%s %s\t%s\t%s\t%s\n", marker, p.Name, p.Environment, p.Organization, status)
	}
	return w.Flush()
}

func runProfileUse(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if err := cfg.SetActiveProfile(name); err != nil {
		return err
	}

	fmt.Printf("Switched to profile %q.\n", name)
	return nil
}
