package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// Format returns the output format from the command's --output flag.
func Format(cmd *cobra.Command) string {
	f, _ := cmd.Flags().GetString("output")
	if f == "" {
		f = "table"
	}
	return f
}

// Print outputs data in the format specified by the --output flag.
// For json/yaml, it marshals the value directly. For table, it calls the tableFn.
func Print(cmd *cobra.Command, data interface{}, tableFn func(w io.Writer) error) error {
	switch Format(cmd) {
	case "json":
		return printJSON(cmd.OutOrStdout(), data)
	case "yaml":
		return printYAML(cmd.OutOrStdout(), data)
	default:
		return tableFn(cmd.OutOrStdout())
	}
}

// Table creates a tabwriter and calls fn to write rows.
func Table(w io.Writer, headers []string, fn func(tw *tabwriter.Writer)) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	fn(tw)
	return tw.Flush()
}

// Confirm prompts the user for y/N confirmation. Returns true if user confirms.
func Confirm(prompt string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N]: ", prompt)
	var response string
	fmt.Scanln(&response)
	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
}

func printJSON(w io.Writer, data interface{}) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

func printYAML(w io.Writer, data interface{}) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	defer enc.Close()
	return enc.Encode(data)
}
