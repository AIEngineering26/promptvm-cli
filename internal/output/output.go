package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// TableRenderable can be implemented by SDK response types for automatic table rendering.
type TableRenderable interface {
	TableHeaders() []string
	TableRows() [][]string
}

// TableFunc is a function that writes table output for a given data value.
type TableFunc func(w io.Writer) error

// Format returns the output format from the command's --output flag.
func Format(cmd *cobra.Command) string {
	f, _ := cmd.Flags().GetString("output")
	if f == "" {
		f = "table"
	}
	return f
}

// Print outputs data in the format specified by the --output flag.
// For json/yaml it marshals data directly. For table it calls tableFn.
// If tableFn is nil and data implements TableRenderable, the table is built automatically.
func Print(cmd *cobra.Command, data interface{}, tableFn TableFunc) error {
	w := cmd.OutOrStdout()
	switch Format(cmd) {
	case "json":
		compact, _ := cmd.Flags().GetBool("compact")
		return PrintJSON(w, data, compact)
	case "yaml":
		return PrintYAML(w, data)
	default:
		if tableFn != nil {
			return tableFn(w)
		}
		if tr, ok := data.(TableRenderable); ok {
			noHeader, _ := cmd.Flags().GetBool("no-header")
			wide, _ := cmd.Flags().GetBool("wide")
			return PrintTable(w, tr.TableHeaders(), tr.TableRows(), &TableOptions{
				NoHeader: noHeader,
				Wide:     wide,
			})
		}
		// Fallback: pretty-print as JSON
		return PrintJSON(w, data, false)
	}
}

// PrintJSON writes data as JSON.
func PrintJSON(w io.Writer, data interface{}, compact bool) error {
	enc := json.NewEncoder(w)
	if !compact {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(data)
}

// PrintYAML writes data as YAML.
func PrintYAML(w io.Writer, data interface{}) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	defer enc.Close()
	return enc.Encode(data)
}

// IsInteractiveStdin reports whether stdin is connected to a TTY.
// Destructive commands check this before showing an interactive
// prompt so they can fail with a clear error in CI rather than
// silently aborting on EOF.
//
// We resolve this through a package-level variable so tests can swap
// in a fixed value without touching real file descriptors.
var IsInteractiveStdin = func() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())
}

// Confirm prompts the user for y/N confirmation via stderr.
func Confirm(prompt string) bool {
	style := lipgloss.NewStyle()
	if ColorEnabled() {
		style = style.Foreground(lipgloss.Color("11")) // yellow
	}
	fmt.Fprint(os.Stderr, style.Render("⚠ "+prompt)+" [y/N]: ")
	var response string
	fmt.Scanln(&response)
	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
}
