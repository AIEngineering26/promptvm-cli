package cmd

import (
	"fmt"

	"github.com/AIEngineering26/promptvm-cli/internal/api"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newSkillsDeleteCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				if !output.Confirm(fmt.Sprintf("Delete skill %s?", args[0])) {
					fmt.Fprintln(cmd.OutOrStdout(), "Cancelled.")
					return nil
				}
			}

			caller, err := api.NewFromContext(cmd)
			if err != nil {
				return err
			}

			if err := caller.Delete("/api/v1/skills/"+args[0], nil); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Deleted skill %s\n", args[0])
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")

	return cmd
}

func init() {
	skillsCmd.AddCommand(newSkillsDeleteCmd())
}
