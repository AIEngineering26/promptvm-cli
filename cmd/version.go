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
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("promptvm-cli %s\n", version)
		fmt.Printf("  commit:  %s\n", commit)
		fmt.Printf("  built:   %s\n", date)
		fmt.Printf("  go-sdk:  %s\n", sdkVer)
		fmt.Printf("  go:      %s\n", runtime.Version())
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
