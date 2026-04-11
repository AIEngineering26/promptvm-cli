package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	sdk "github.com/AIEngineering26/promptvm-go-sdk"
	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/spf13/cobra"
)

func newPromptsExportCmd() *cobra.Command {
	var (
		format     string
		outputPath string
	)

	cmd := &cobra.Command{
		Use:   "export <prompt-id>",
		Short: "Export prompt to file",
		Long:  "Exports a prompt in Markdown, JSON, or XML format.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			f, err := sdk.NewExportPromptRequestFormatFromString(format)
			if err != nil {
				return err
			}

			resp, err := c.PromptExport.ExportPrompt(cmd.Context(), &sdk.ExportPromptRequest{
				PromptID: args[0],
				Format:   f,
			})
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}

			d := resp.GetData()
			if d == nil {
				return fmt.Errorf("empty export response")
			}

			if outputPath != "" {
				dest := outputPath
				info, err := os.Stat(dest)
				if err == nil && info.IsDir() {
					dest = filepath.Join(dest, d.GetFilename())
				}
				if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
					return fmt.Errorf("creating output directory: %w", err)
				}
				if err := os.WriteFile(dest, []byte(d.GetContent()), 0644); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Exported %s to %s\n", args[0], dest)
				return nil
			}

			// Write to stdout
			_, err = io.WriteString(cmd.OutOrStdout(), d.GetContent())
			return err
		},
	}

	cmd.Flags().StringVar(&format, "format", "json", "Export format: json, md, xml")
	cmd.Flags().StringVarP(&outputPath, "output-path", "O", "", "Output directory or file path")

	return cmd
}

func init() {
	promptsCmd.AddCommand(newPromptsExportCmd())
}
