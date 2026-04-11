package cmd

import "github.com/spf13/cobra"

var marketplaceCmd = &cobra.Command{
	Use:     "marketplace",
	Aliases: []string{"mp"},
	Short:   "Browse, manage, and interact with marketplace listings",
	Long:    "Search the marketplace, manage listings as a creator, subscribe, rate, comment, and follow.",
}

func init() {
	rootCmd.AddCommand(marketplaceCmd)
}
