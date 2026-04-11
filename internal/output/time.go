package output

import (
	"time"

	"github.com/dustin/go-humanize"
)

// HumanTime formats a timestamp as a human-friendly relative time.
// Times older than 30 days are shown as absolute dates.
func HumanTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	if time.Since(t) > 30*24*time.Hour {
		return t.Format("2006-01-02")
	}
	return humanize.Time(t)
}
