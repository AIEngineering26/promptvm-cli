// Package ctxblock writes the local context-export managed block (CEO-1): the
// thin v1 payoff that closes the capture loop without Phase-2 retrieval. After
// a capture is promoted/auto-published, the CLI refreshes a fenced block in a
// project context file (CLAUDE.md or .promptvm/context.md) so the next session
// benefits. The block is replaced in place, never duplicated.
package ctxblock

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	startMarker = "<!-- promptvm:context start -->"
	endMarker   = "<!-- promptvm:context end -->"
)

// Render builds the managed block body from a list of promoted-capture summary
// lines. The markers are always included so a later run can find and replace it.
func Render(lines []string) string {
	var b strings.Builder
	b.WriteString(startMarker)
	b.WriteByte('\n')
	b.WriteString("## Recent PromptVM context\n")
	b.WriteString("_Auto-maintained by `promptvm sync export`. Do not edit between the markers._\n\n")
	if len(lines) == 0 {
		b.WriteString("_No promoted captures yet._\n")
	}
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(l)
		b.WriteByte('\n')
	}
	b.WriteString(endMarker)
	return b.String()
}

// Upsert replaces an existing managed block in the file at path, or appends one
// if absent. The file is created if it does not exist. Returns whether an
// existing block was replaced (vs appended/created).
func Upsert(path, block string) (replaced bool, err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	content := string(existing)

	var out string
	if s := strings.Index(content, startMarker); s != -1 {
		e := strings.Index(content, endMarker)
		if e != -1 && e > s {
			e += len(endMarker)
			out = content[:s] + block + content[e:]
			replaced = true
		}
	}
	if !replaced {
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		if content != "" {
			content += "\n"
		}
		out = content + block + "\n"
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(out), 0o644); err != nil {
		return false, fmt.Errorf("writing context block: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return false, fmt.Errorf("renaming context block: %w", err)
	}
	return replaced, nil
}
