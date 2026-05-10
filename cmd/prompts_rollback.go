package cmd

import (
	"fmt"
	"strconv"

	sdk "github.com/AIEngineering26/promptvm-go-sdk"
	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/spf13/cobra"
)

func init() {
	promptsCmd.AddCommand(newPromptsRollbackCmd())
}

func newPromptsRollbackCmd() *cobra.Command {
	var (
		toVersion      int
		yes            bool
		idempotencyKey string
	)

	cmd := &cobra.Command{
		Use:   "rollback <prompt-id> --to <versionNumber>",
		Short: "Roll a prompt's current pointer back to a previous version",
		Long: `Rolls a prompt back to a known-good version.

The server creates a new version whose content is a copy of v<targetVersion>
and atomically advances the prompt's "current" pointer to that new version.
The original v<targetVersion> is left intact.

The rollback is gated by an interactive y/N confirmation unless --yes is
passed. Pass --idempotency-key to make the call retry-safe; the server will
replay the original 2xx response for any subsequent retry within 24 hours.`,
		Example: `  promptvm prompts rollback pmt_abc123 --to 1
  promptvm prompts rollback pmt_abc123 --to 3 --yes
  promptvm prompts rollback pmt_abc123 --to 2 --idempotency-key $(uuidgen)`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			promptID := args[0]

			if toVersion < 1 {
				return fmt.Errorf("--to must be a positive version number (got %d)", toVersion)
			}

			if !yes {
				if !output.IsInteractiveStdin() {
					// Non-interactive (CI, piped stdin) without --yes
					// would otherwise read EOF, treat it as "no" and
					// silently abort. Be loud instead — the user
					// almost certainly forgot the flag.
					return fmt.Errorf("--yes is required when running non-interactively (stdin is not a TTY)")
				}
				prompt := fmt.Sprintf("Roll prompt %s back to v%d? A new version (copy of v%d) will become current.", promptID, toVersion, toVersion)
				if !output.Confirm(prompt) {
					fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
			}

			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.RollbackPromptRequest{
				PromptID:      promptID,
				TargetVersion: toVersion,
			}
			if idempotencyKey != "" {
				k := idempotencyKey
				req.IdempotencyKey = &k
			}

			resp, err := c.PromptVersions.RollbackPrompt(cmd.Context(), req)
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}

			d := resp.GetData()
			fmt.Fprintf(cmd.OutOrStdout(),
				"rolled back: prompt now points at version %s (copy of v%d)\n",
				strconv.Itoa(d.GetVersionNumber()), toVersion,
			)
			return nil
		},
	}

	cmd.Flags().IntVar(&toVersion, "to", 0, "Target version number to roll back to (required)")
	cmd.MarkFlagRequired("to")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip interactive confirmation")
	cmd.Flags().StringVar(&idempotencyKey, "idempotency-key", "", "Idempotency key (24h replay window)")

	return cmd
}
