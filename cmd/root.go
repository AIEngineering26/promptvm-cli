package cmd

import (
	"fmt"
	"os"

	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "promptvm",
	Short: "PromptVM CLI - manage prompts, workspaces, and marketplace",
	Long:  "The official CLI for the PromptVM platform.\nWraps the PromptVM API via the generated Go SDK.",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		noColor, _ := cmd.Flags().GetBool("no-color")
		output.InitColor(noColor)
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().String("api-key", "", "API key (overrides PROMPTVM_API_KEY env and config file)")
	rootCmd.PersistentFlags().String("base-url", "", "API base URL (overrides PROMPTVM_BASE_URL env and config file)")
	rootCmd.PersistentFlags().StringP("output", "o", "table", "Output format: table|json|yaml")
	rootCmd.PersistentFlags().Bool("no-color", false, "Disable colored output")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose/debug logging")
	rootCmd.PersistentFlags().Bool("no-header", false, "Hide table headers (table output only)")
	rootCmd.PersistentFlags().BoolP("wide", "w", false, "Show all columns (table output only)")
	rootCmd.PersistentFlags().Bool("compact", false, "Compact JSON output (json output only)")
}
