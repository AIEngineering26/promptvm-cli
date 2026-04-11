package output

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/lipgloss"
)

// TableOptions controls table rendering behavior.
type TableOptions struct {
	NoHeader bool // Skip the header row
	Wide     bool // Show all columns (unused by default; commands define wide sets)
	MaxWidth int  // Max column width before truncation (0 = 40)
}

// PrintTable renders rows as a tab-aligned table with optional colored headers.
func PrintTable(w io.Writer, headers []string, rows [][]string, opts *TableOptions) error {
	if opts == nil {
		opts = &TableOptions{}
	}
	maxW := opts.MaxWidth
	if maxW == 0 {
		maxW = 40
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	if !opts.NoHeader && len(headers) > 0 {
		styled := make([]string, len(headers))
		for i, h := range headers {
			if ColorEnabled() {
				styled[i] = lipgloss.NewStyle().Bold(true).Render(h)
			} else {
				styled[i] = h
			}
		}
		fmt.Fprintln(tw, strings.Join(styled, "\t"))
	}

	for _, row := range rows {
		truncated := make([]string, len(row))
		for i, v := range row {
			truncated[i] = truncate(v, maxW)
		}
		fmt.Fprintln(tw, strings.Join(truncated, "\t"))
	}

	return tw.Flush()
}

// Table creates a tabwriter, writes headers, calls fn for rows, and flushes.
// This preserves backward-compatibility with existing commands.
func Table(w io.Writer, headers []string, fn func(tw *tabwriter.Writer)) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if len(headers) > 0 {
		if ColorEnabled() {
			styled := make([]string, len(headers))
			for i, h := range headers {
				styled[i] = lipgloss.NewStyle().Bold(true).Render(h)
			}
			fmt.Fprintln(tw, strings.Join(styled, "\t"))
		} else {
			fmt.Fprintln(tw, strings.Join(headers, "\t"))
		}
	}
	fn(tw)
	return tw.Flush()
}

// Summary prints a result-count summary line, e.g. "Showing 20 of 147 results".
func Summary(w io.Writer, shown, total int) {
	if total <= 0 {
		return
	}
	msg := fmt.Sprintf("Showing %d of %d results", shown, total)
	fmt.Fprintln(w, Dim(msg))
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
