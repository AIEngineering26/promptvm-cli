package output

import (
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

// noColor caches the color-disabled state. Set via InitColor.
var noColor bool

// InitColor sets up color state from the --no-color flag. Call once from root PersistentPreRun.
func InitColor(noColorFlag bool) {
	noColor = noColorFlag || os.Getenv("NO_COLOR") != "" || !isTerminal()
}

// ColorEnabled returns true if colored output is allowed.
func ColorEnabled() bool {
	return !noColor
}

func isTerminal() bool {
	return isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
}

// Style helpers — return styled strings, falling back to plain when color is off.

func styleWith(s string, style lipgloss.Style) string {
	if !ColorEnabled() {
		return s
	}
	return style.Render(s)
}

// Success renders text in green with a ✓ prefix.
func Success(msg string) string {
	return styleWith("✓ "+msg, lipgloss.NewStyle().Foreground(lipgloss.Color("2")))
}

// Warn renders text in yellow with a ⚠ prefix.
func Warn(msg string) string {
	return styleWith("⚠ "+msg, lipgloss.NewStyle().Foreground(lipgloss.Color("11")))
}

// Error renders text in red with a ✗ prefix.
func Error(msg string) string {
	return styleWith("✗ "+msg, lipgloss.NewStyle().Foreground(lipgloss.Color("1")))
}

// Info renders text in cyan.
func Info(msg string) string {
	return styleWith(msg, lipgloss.NewStyle().Foreground(lipgloss.Color("6")))
}

// Dim renders text in gray.
func Dim(msg string) string {
	return styleWith(msg, lipgloss.NewStyle().Foreground(lipgloss.Color("8")))
}

// Bold renders text in bold.
func Bold(msg string) string {
	return styleWith(msg, lipgloss.NewStyle().Bold(true))
}
