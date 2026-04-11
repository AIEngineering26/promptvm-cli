package prompt

import (
	"github.com/charmbracelet/huh"
)

// MaskedInput prompts for a secret value (API key, password) with masked display.
func MaskedInput(label string) (string, error) {
	var value string
	err := huh.NewInput().
		Title(label).
		EchoMode(huh.EchoModePassword).
		Value(&value).
		Run()
	if err != nil {
		return "", wrapCancel(err)
	}
	return value, nil
}
