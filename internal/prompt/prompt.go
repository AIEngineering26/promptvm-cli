package prompt

import (
	"errors"

	"github.com/charmbracelet/huh"
)

// ErrCancelled is returned when the user cancels a prompt.
var ErrCancelled = errors.New("prompt cancelled")

// Confirm asks for a yes/no answer. Default is no.
func Confirm(message string) (bool, error) {
	var confirmed bool
	err := huh.NewConfirm().
		Title(message).
		Affirmative("Yes").
		Negative("No").
		Value(&confirmed).
		Run()
	if err != nil {
		return false, wrapCancel(err)
	}
	return confirmed, nil
}

// Select presents a list of options and returns the selected index and value.
func Select(label string, items []string) (int, string, error) {
	if len(items) == 0 {
		return -1, "", errors.New("no items to select from")
	}

	opts := make([]huh.Option[string], len(items))
	for i, item := range items {
		opts[i] = huh.NewOption(item, item)
	}

	var selected string
	err := huh.NewSelect[string]().
		Title(label).
		Options(opts...).
		Value(&selected).
		Run()
	if err != nil {
		return -1, "", wrapCancel(err)
	}

	for i, item := range items {
		if item == selected {
			return i, selected, nil
		}
	}
	return -1, selected, nil
}

// Input prompts for a single-line text value with an optional default.
func Input(label string, defaultVal string) (string, error) {
	var value string
	field := huh.NewInput().
		Title(label).
		Value(&value)
	if defaultVal != "" {
		value = defaultVal
	}

	err := field.Run()
	if err != nil {
		return "", wrapCancel(err)
	}
	return value, nil
}

func wrapCancel(err error) error {
	if errors.Is(err, huh.ErrUserAborted) {
		return ErrCancelled
	}
	return err
}
