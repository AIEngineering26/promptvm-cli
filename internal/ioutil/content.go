package ioutil

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// ReadContent resolves prompt content from --content, --file, or stdin.
// Priority: --content flag > --file flag (with "-" meaning stdin).
func ReadContent(cmd *cobra.Command) (string, error) {
	content, _ := cmd.Flags().GetString("content")
	if content != "" {
		return content, nil
	}

	file, _ := cmd.Flags().GetString("file")
	if file == "" {
		return "", fmt.Errorf("one of --content or --file is required")
	}

	if file == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		return string(data), nil
	}

	data, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("reading file %q: %w", file, err)
	}
	return string(data), nil
}
