package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	sdk "github.com/AIEngineering26/promptvm-go-sdk"
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

			// Parse CLI-supplied variables for client-side substitution.
			// The SDK's ResolvePromptRequest handles server-side [[include:]]
			// expansion; {{variable}} substitution happens below on the
			// resolved content.
			variables, err := parseVariables(vars, varsFile)
			if err != nil {
				return err
			}

			resp, err := c.PromptResolution.ResolvePrompt(cmd.Context(), req)
			if err != nil {
				return err
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				d := resp.GetData()
				if d != nil {
					content := applyVariables(d.GetResolvedContent(), variables)
					fmt.Fprintf(w, "Resolved prompt:\n---\n%s\n---\n", content)
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

func parseVariables(vars []string, varsFile string) (map[string]string, error) {
	result := make(map[string]string)

	if varsFile != "" {
		var data []byte
		var err error
		if varsFile == "-" {
			data, err = io.ReadAll(os.Stdin)
		} else {
			data, err = os.ReadFile(varsFile)
		}
		if err != nil {
			return nil, fmt.Errorf("reading vars file: %w", err)
		}
		var m map[string]string
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("parsing vars file (expected JSON object of string→string): %w", err)
		}
		for k, v := range m {
			result[k] = v
		}
	}

	for _, v := range vars {
		k, val, ok := strings.Cut(v, "=")
		if !ok {
			return nil, fmt.Errorf("invalid --var %q: expected key=value", v)
		}
		result[k] = val
	}

	return result, nil
}

// applyVariables substitutes {{name}} tokens in content with values.
// Unknown variables are left in place so the user can see what is missing.
func applyVariables(content string, vars map[string]string) string {
	if len(vars) == 0 {
		return content
	}
	for k, v := range vars {
		content = strings.ReplaceAll(content, "{{"+k+"}}", v)
		content = strings.ReplaceAll(content, "{{ "+k+" }}", v)
	}
	return content
}

func init() {
	promptsCmd.AddCommand(newPromptsResolveCmd())
}
