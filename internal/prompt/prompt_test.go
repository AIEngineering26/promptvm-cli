package prompt

import (
	"errors"
	"testing"

	"github.com/charmbracelet/huh"
)

func TestWrapCancel_UserAborted(t *testing.T) {
	err := wrapCancel(huh.ErrUserAborted)
	if !errors.Is(err, ErrCancelled) {
		t.Errorf("wrapCancel(ErrUserAborted) = %v, want ErrCancelled", err)
	}
}

func TestWrapCancel_Other(t *testing.T) {
	other := errors.New("boom")
	err := wrapCancel(other)
	if errors.Is(err, ErrCancelled) {
		t.Errorf("wrapCancel(other) should not be ErrCancelled")
	}
	if err.Error() != "boom" {
		t.Errorf("wrapCancel(other) = %v, want 'boom'", err)
	}
}

func TestSelect_EmptyItems(t *testing.T) {
	// Select with no items should fail immediately without touching a TTY.
	idx, val, err := Select("pick one", nil)
	if err == nil {
		t.Error("Select(nil) should return an error")
	}
	if idx != -1 || val != "" {
		t.Errorf("Select(nil) returned idx=%d val=%q, want -1 and empty", idx, val)
	}
}
