package output

import (
	"testing"
)

func TestColorEnabled_NoColorFlag(t *testing.T) {
	InitColor(true)
	if ColorEnabled() {
		t.Error("expected color disabled when noColor flag is true")
	}
}

func TestStyleHelpers_NoColor(t *testing.T) {
	noColor = true
	defer func() { noColor = false }()

	tests := []struct {
		name string
		fn   func(string) string
		want string
	}{
		{"Success", Success, "✓ hello"},
		{"Warn", Warn, "⚠ hello"},
		{"Error", Error, "✗ hello"},
		{"Info", Info, "hello"},
		{"Dim", Dim, "hello"},
		{"Bold", Bold, "hello"},
	}
	for _, tt := range tests {
		got := tt.fn("hello")
		if got != tt.want {
			t.Errorf("%s(\"hello\") = %q, want %q", tt.name, got, tt.want)
		}
	}
}
