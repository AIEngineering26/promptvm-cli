package errors

import (
	"errors"
	"strings"
	"testing"
)

func TestCLIError_Error(t *testing.T) {
	e := &CLIError{
		Message:    "prompt not found",
		Hint:       "Run promptvm prompts list",
		StatusCode: 404,
	}
	got := e.Error()
	if !strings.Contains(got, "prompt not found") {
		t.Errorf("expected message in error, got: %s", got)
	}
	if !strings.Contains(got, "(404)") {
		t.Errorf("expected status code in error, got: %s", got)
	}
	if !strings.Contains(got, "Hint:") {
		t.Errorf("expected hint in error, got: %s", got)
	}
}

func TestCLIError_NoHint(t *testing.T) {
	e := &CLIError{Message: "something failed"}
	got := e.Error()
	if strings.Contains(got, "Hint:") {
		t.Errorf("expected no hint, got: %s", got)
	}
}

func TestFromHTTP(t *testing.T) {
	tests := []struct {
		code    int
		wantMsg string
	}{
		{401, "Not authenticated"},
		{403, "Permission denied"},
		{404, "Resource not found"},
		{429, "Rate limited"},
		{500, "Server error"},
		{503, "Server error"},
	}
	for _, tt := range tests {
		e := FromHTTP(tt.code, "")
		if !strings.Contains(e.Message, tt.wantMsg) {
			t.Errorf("FromHTTP(%d).Message = %q, want containing %q", tt.code, e.Message, tt.wantMsg)
		}
		if e.StatusCode != tt.code {
			t.Errorf("FromHTTP(%d).StatusCode = %d", tt.code, e.StatusCode)
		}
	}
}

func TestFromHTTP_WithResource(t *testing.T) {
	e := FromHTTP(404, "pmt_abc123")
	if !strings.Contains(e.Message, "pmt_abc123") {
		t.Errorf("expected resource ID in message, got: %s", e.Message)
	}
}

func TestNew(t *testing.T) {
	e := New("something broke", "try again")
	if e.Message != "something broke" {
		t.Errorf("unexpected message: %s", e.Message)
	}
	if e.Hint != "try again" {
		t.Errorf("unexpected hint: %s", e.Hint)
	}
}

func TestFromSDK_Nil(t *testing.T) {
	if FromSDK(nil) != nil {
		t.Error("expected nil for nil error")
	}
}

func TestFromSDK_GenericError(t *testing.T) {
	e := FromSDK(errors.New("connection refused"))
	if e == nil {
		t.Fatal("expected non-nil CLIError")
	}
	if !strings.Contains(e.Message, "connection refused") {
		t.Errorf("expected original message, got: %s", e.Message)
	}
}
