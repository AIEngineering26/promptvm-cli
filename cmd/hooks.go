package cmd

import "github.com/spf13/cobra"

var hooksCmd = &cobra.Command{
	Use:     "hooks",
	Short:   "Manage Claude Code hooks",
	Long:    "Install, list, and manage Claude Code lifecycle hooks from PromptVM.",
	Aliases: []string{"hook"},
}

func init() {
	rootCmd.AddCommand(hooksCmd)
}
