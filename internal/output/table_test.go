package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintTable_Basic(t *testing.T) {
	// Disable color for test predictability
	noColor = true
	defer func() { noColor = false }()

	var buf bytes.Buffer
	headers := []string{"ID", "NAME"}
	rows := [][]string{
		{"1", "Alice"},
		{"2", "Bob"},
	}

	if err := PrintTable(&buf, headers, rows, nil); err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if !strings.Contains(got, "ID") {
		t.Errorf("expected header ID, got: %s", got)
	}
	if !strings.Contains(got, "Alice") {
		t.Errorf("expected row Alice, got: %s", got)
	}
}

func TestPrintTable_NoHeader(t *testing.T) {
	noColor = true
	defer func() { noColor = false }()

	var buf bytes.Buffer
	headers := []string{"ID", "NAME"}
	rows := [][]string{{"1", "Alice"}}

	if err := PrintTable(&buf, headers, rows, &TableOptions{NoHeader: true}); err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	if strings.Contains(got, "ID") {
		t.Errorf("expected no header, got: %s", got)
	}
	if !strings.Contains(got, "Alice") {
		t.Errorf("expected row data, got: %s", got)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"short", 10, "short"},
		{"this is a long string", 10, "this is..."},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc"},
		{"", 5, ""},
	}
	for _, tt := range tests {
		got := truncate(tt.input, tt.max)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
		}
	}
}

func TestSummary(t *testing.T) {
	noColor = true
	defer func() { noColor = false }()

	var buf bytes.Buffer
	Summary(&buf, 20, 147)
	got := buf.String()
	if !strings.Contains(got, "Showing 20 of 147 results") {
		t.Errorf("unexpected summary: %s", got)
	}
}

func TestSummary_Zero(t *testing.T) {
	var buf bytes.Buffer
	Summary(&buf, 0, 0)
	if buf.Len() != 0 {
		t.Errorf("expected no output for zero total, got: %s", buf.String())
	}
}
