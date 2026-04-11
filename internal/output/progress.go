package output

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// ProgressWriter wraps an io.Writer and prints a progress bar to stderr.
type ProgressWriter struct {
	Writer  io.Writer
	Total   int64
	written int64
	label   string
}

// NewProgressWriter returns a writer that displays upload/download progress.
func NewProgressWriter(w io.Writer, total int64, label string) *ProgressWriter {
	return &ProgressWriter{Writer: w, Total: total, label: label}
}

func (pw *ProgressWriter) Write(p []byte) (int, error) {
	n, err := pw.Writer.Write(p)
	pw.written += int64(n)
	pw.render()
	return n, err
}

func (pw *ProgressWriter) render() {
	if pw.Total <= 0 {
		return
	}
	pct := float64(pw.written) / float64(pw.Total)
	if pct > 1 {
		pct = 1
	}

	barWidth := 30
	filled := int(pct * float64(barWidth))
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	label := pw.label
	if label == "" {
		label = "Progress"
	}

	fmt.Fprintf(os.Stderr, "\r%s %s %3.0f%% (%s/%s)",
		label, bar, pct*100,
		humanBytes(pw.written), humanBytes(pw.Total),
	)

	if pw.written >= pw.Total {
		fmt.Fprintln(os.Stderr)
	}
}

func humanBytes(b int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
