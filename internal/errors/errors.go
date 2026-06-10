package errors

import (
	"fmt"
	"net/http"
	"strings"

	sdkcore "github.com/AIEngineering26/promptvm-go-sdk/core"
)

// CLIError is a user-facing error with an optional hint.
type CLIError struct {
	Message    string
	Hint       string
	StatusCode int
}

func (e *CLIError) Error() string {
	var b strings.Builder
	b.WriteString("Error: ")
	b.WriteString(e.Message)
	if e.StatusCode > 0 {
		fmt.Fprintf(&b, " (%d)", e.StatusCode)
	}
	if e.Hint != "" {
		b.WriteString("\nHint: ")
		b.WriteString(e.Hint)
	}
	return b.String()
}

// New creates a CLIError with the given message and optional hint.
func New(message, hint string) *CLIError {
	return &CLIError{Message: message, Hint: hint}
}

// FromHTTP maps an HTTP status code to a user-friendly CLIError.
func FromHTTP(statusCode int, resourceID string) *CLIError {
	switch statusCode {
	case http.StatusUnauthorized:
		return &CLIError{
			Message:    "Not authenticated",
			Hint:       "Run `promptvm auth login` to sign in.",
			StatusCode: statusCode,
		}
	case http.StatusForbidden:
		return &CLIError{
			Message:    "Permission denied",
			Hint:       "Your API key lacks the required scope.",
			StatusCode: statusCode,
		}
	case http.StatusNotFound:
		msg := "Resource not found"
		if resourceID != "" {
			msg = fmt.Sprintf("Resource not found: %s", resourceID)
		}
		return &CLIError{
			Message:    msg,
			Hint:       "Run the corresponding list command to see available resources.",
			StatusCode: statusCode,
		}
	case http.StatusTooManyRequests:
		return &CLIError{
			Message:    "Rate limited",
			Hint:       "Wait a moment and try again.",
			StatusCode: statusCode,
		}
	default:
		if statusCode >= 500 {
			return &CLIError{
				Message:    "Server error",
				Hint:       "Try again later or check https://status.promptvm.com",
				StatusCode: statusCode,
			}
		}
		return &CLIError{
			Message:    fmt.Sprintf("Request failed (%d)", statusCode),
			StatusCode: statusCode,
		}
	}
}

// FromSDK converts an SDK API error into a CLIError.
func FromSDK(err error) *CLIError {
	if err == nil {
		return nil
	}
	if apiErr, ok := err.(*sdkcore.APIError); ok {
		return FromHTTP(apiErr.StatusCode, "")
	}
	return &CLIError{Message: err.Error()}
}
