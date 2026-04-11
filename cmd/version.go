package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
	sdkVer  = "v0.0.10"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print CLI, SDK, and Go version information",
	RunE: func(cmd *cobra.Command, args []string) error {
		w := cmd.OutOrStdout()
		fmt.Fprintf(w, "promptvm-cli %s\n", version)
		fmt.Fprintf(w, "  commit:  %s\n", commit)
		fmt.Fprintf(w, "  built:   %s\n", date)
		fmt.Fprintf(w, "  go-sdk:  %s\n", sdkVer)
		fmt.Fprintf(w, "  go:      %s\n", runtime.Version())
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
