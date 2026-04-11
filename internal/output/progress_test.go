package output

import (
	"bytes"
	"testing"
)

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}
	for _, tt := range tests {
		got := humanBytes(tt.input)
		if got != tt.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestProgressWriter_Write(t *testing.T) {
	var buf bytes.Buffer
	pw := NewProgressWriter(&buf, 100, "Upload")

	data := make([]byte, 50)
	n, err := pw.Write(data)
	if err != nil {
		t.Fatal(err)
	}
	if n != 50 {
		t.Errorf("expected 50 bytes written, got %d", n)
	}
	if pw.written != 50 {
		t.Errorf("expected 50 bytes tracked, got %d", pw.written)
	}
}
