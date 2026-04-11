package cmd

import "github.com/spf13/cobra"

var promptsCmd = &cobra.Command{
	Use:   "prompts",
	Short: "Manage prompts",
	Long:  "Create, list, get, update, delete, resolve, export, and organize prompts.",
}

func init() {
	rootCmd.AddCommand(promptsCmd)
}
