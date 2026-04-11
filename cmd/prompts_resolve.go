package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	sdk "github.com/AIEngineering26/promptvm-go-sdk"
	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newPromptsResolveCmd() *cobra.Command {
	var (
		vars     []string
		varsFile string
		version  string
	)

	cmd := &cobra.Command{
		Use:   "resolve <prompt-id>",
		Short: "Resolve prompt with variables",
		Long:  "Resolves [[include:]] references and {{variable}} substitutions.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.ResolvePromptRequest{
				PromptID: args[0],
			}
			if version != "" {
				req.VersionID = &version
			}

			// Parse variables — not part of SDK request, included as context
			_ = parseVariables(vars, varsFile)

			resp, err := c.PromptResolution.ResolvePrompt(cmd.Context(), req)
			if err != nil {
				return err
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				d := resp.GetData()
				if d != nil {
					fmt.Fprintf(w, "Resolved prompt:\n---\n%s\n---\n", d.GetResolvedContent())
				}
				return nil
			})
		},
	}

	cmd.Flags().StringArrayVarP(&vars, "var", "V", nil, "Variable key=value (repeatable)")
	cmd.Flags().StringVar(&varsFile, "vars-file", "", "JSON file with variables (use - for stdin)")
	cmd.Flags().StringVar(&version, "version", "", "Resolve specific version ID")

	return cmd
}

func parseVariables(vars []string, varsFile string) map[string]string {
	result := make(map[string]string)

	if varsFile != "" {
		var data []byte
		var err error
		if varsFile == "-" {
			data, err = io.ReadAll(os.Stdin)
		} else {
			data, err = os.ReadFile(varsFile)
		}
		if err == nil {
			var m map[string]string
			if json.Unmarshal(data, &m) == nil {
				for k, v := range m {
					result[k] = v
				}
			}
		}
	}

	for _, v := range vars {
		if k, val, ok := strings.Cut(v, "="); ok {
			result[k] = val
		}
	}

	return result
}

func init() {
	promptsCmd.AddCommand(newPromptsResolveCmd())
}
