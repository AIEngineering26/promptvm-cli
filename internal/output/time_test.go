package output

import (
	"testing"
	"time"
)

func TestHumanTime_Zero(t *testing.T) {
	got := HumanTime(time.Time{})
	if got != "-" {
		t.Errorf("HumanTime(zero) = %q, want \"-\"", got)
	}
}

func TestHumanTime_Recent(t *testing.T) {
	got := HumanTime(time.Now().Add(-5 * time.Minute))
	if got == "-" || got == "" {
		t.Errorf("expected relative time, got: %q", got)
	}
}

func TestHumanTime_Old(t *testing.T) {
	old := time.Now().Add(-60 * 24 * time.Hour) // 60 days ago
	got := HumanTime(old)
	// Should be an absolute date like 2024-01-15
	if len(got) != 10 { // YYYY-MM-DD
		t.Errorf("expected absolute date for old time, got: %q", got)
	}
}
