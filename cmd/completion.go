package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts",
	Long: `Generate shell completion scripts for promptvm.

To load completions:

Bash:
  $ source <(promptvm completion bash)
  # Or, to load on startup:
  $ promptvm completion bash > /etc/bash_completion.d/promptvm

Zsh:
  $ promptvm completion zsh > "${fpath[1]}/_promptvm"
  # Then restart your shell or run: compinit

Fish:
  $ promptvm completion fish | source
  # Or, to load on startup:
  $ promptvm completion fish > ~/.config/fish/completions/promptvm.fish

PowerShell:
  PS> promptvm completion powershell | Out-String | Invoke-Expression
`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletion(os.Stdout)
		case "zsh":
			return rootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			return rootCmd.GenFishCompletion(os.Stdout, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(completionCmd)
}
