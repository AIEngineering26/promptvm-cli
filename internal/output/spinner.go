package output

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/huh/spinner"
)

// Spin shows a spinner with the given message while action runs.
// If stdout is not a TTY, it prints the message and runs the action without a spinner.
func Spin(ctx context.Context, message string, action func() error) error {
	if !isTerminal() {
		fmt.Fprintln(os.Stderr, message)
		return action()
	}

	var actionErr error
	err := spinner.New().
		Title(message).
		Context(ctx).
		Action(func() {
			actionErr = action()
		}).
		Run()
	if err != nil {
		return err
	}
	return actionErr
}
