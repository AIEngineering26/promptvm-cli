package cmd

import (
	"fmt"
	"os"

	clierrors "github.com/AIEngineering26/promptvm-cli/internal/errors"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/AIEngineering26/promptvm-cli/internal/prompt"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "promptvm",
	Short: "PromptVM CLI - manage prompts, workspaces, and marketplace",
	Long:  "The official CLI for the PromptVM platform.\nWraps the PromptVM API via the generated Go SDK.",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		noColor, _ := cmd.Flags().GetBool("no-color")
		output.InitColor(noColor)
		maybeAutoInstallAgentSkill(cmd)
	},
	// Cobra already prints its own error to stderr before Execute returns,
	// so silence its default error handling and let Execute print a
	// friendlier message.
	SilenceErrors: true,
	SilenceUsage:  true,
}

// Execute runs the root command and maps any error to a user-friendly
// CLIError before exiting with a non-zero status.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		// Cancelled interactive prompt should exit silently with code 1.
		if err == prompt.ErrCancelled {
			os.Exit(1)
		}

		// Translate SDK/HTTP errors into CLI-friendly output.
		if cliErr := clierrors.FromSDK(err); cliErr != nil {
			fmt.Fprintln(os.Stderr, cliErr.Error())
		} else {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().String("public-key", "", "API public key (pk_…); paired with --secret-key. Overrides PROMPTVM_PUBLIC_KEY env and config file")
	rootCmd.PersistentFlags().String("secret-key", "", "API secret key (sk_…); paired with --public-key. Overrides PROMPTVM_SECRET_KEY env and config file")
	rootCmd.PersistentFlags().String("api-key", "", "Combined API key in pk_xxx:sk_xxx form (DEPRECATED: prefer --public-key/--secret-key). Overrides PROMPTVM_API_KEY env and config file")
	rootCmd.PersistentFlags().String("base-url", "", "API base URL (overrides PROMPTVM_BASE_URL env and config file)")
	rootCmd.PersistentFlags().StringP("output", "o", "table", "Output format: table|json|yaml")
	rootCmd.PersistentFlags().Bool("no-color", false, "Disable colored output")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose/debug logging")
	rootCmd.PersistentFlags().Bool("no-header", false, "Hide table headers (table output only)")
	rootCmd.PersistentFlags().BoolP("wide", "w", false, "Show all columns (table output only)")
	rootCmd.PersistentFlags().Bool("compact", false, "Compact JSON output (json output only)")
}
