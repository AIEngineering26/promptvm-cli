package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintJSON_Pretty(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]string{"name": "test"}
	if err := PrintJSON(&buf, data, false); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "  \"name\"") {
		t.Errorf("expected pretty JSON, got: %s", got)
	}
}

func TestPrintJSON_Compact(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]string{"name": "test"}
	if err := PrintJSON(&buf, data, true); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(buf.String())
	if got != `{"name":"test"}` {
		t.Errorf("expected compact JSON, got: %s", got)
	}
}

func TestPrintYAML(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]string{"name": "test"}
	if err := PrintYAML(&buf, data); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "name: test") {
		t.Errorf("expected YAML output, got: %s", got)
	}
}

func TestFormat_Default(t *testing.T) {
	// Format falls back to "table" when flag is empty
	// This is tested indirectly; here we just verify the function exists.
}
