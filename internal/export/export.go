// Package export adapts a resolved marketplace item (skill or prompt) into the
// native rules format of a non-Claude coding agent (Cursor, Codex, Windsurf).
//
// The default `promptvm add` install target is Claude Code and is unchanged;
// this package is only reached when `add --target <t>` names one of the
// supported adapters. Each adapter implements the Target interface: it decides
// where its rule file lives (Path) and how the item's content maps onto that
// tool's rule format (Transform).
package export

import (
	"fmt"
	"sort"
	"strings"
)

// Bundle is the tool-agnostic input to a Target: the item's identity plus its
// markdown body and any bundled files. For a skill, Body is the SKILL.md body
// with the YAML frontmatter stripped, and Name/Description come from that
// frontmatter (mapped into the target's own frontmatter). For a prompt, Body is
// the raw prompt text and there are no bundled files.
type Bundle struct {
	// Name is the safe on-disk name segment (kebab-case) used for file/section
	// names in the target format.
	Name string
	// Description is the human description surfaced in the target's frontmatter.
	// May be empty.
	Description string
	// Body is the markdown body written into the target rule (frontmatter already
	// stripped for skills).
	Body string
	// Files are bundled resources to write alongside the rule (skills only).
	// Cursor/Windsurf write them under a per-rule subdirectory; Codex skips them.
	Files []BundleFile
}

// BundleFile is one bundled resource: a relative forward-slash Path plus a
// presigned DownloadURL to fetch its bytes.
type BundleFile struct {
	Path        string
	DownloadURL string
	SizeBytes   int64
}

// FileWrite is one file the caller must materialize. RelPath is relative to the
// target root (e.g. the project directory). Exactly one source is set:
//   - Content is inline bytes (the rule file itself) — write verbatim.
//   - DownloadURL is a presigned URL (a bundled resource) — fetch into RelPath.
//   - Append is true when Content must be appended to an existing RelPath
//     (Codex's shared AGENTS.md) rather than replacing it.
type FileWrite struct {
	RelPath     string
	Content     []byte
	DownloadURL string
	// Append requests append-or-create semantics instead of replace. Only the
	// Codex adapter sets it (AGENTS.md accumulates sections).
	Append bool
}

// Target is a per-tool export adapter.
type Target interface {
	// Name is the flag value that selects this target (e.g. "cursor").
	Name() string
	// Path returns the primary rule file's path relative to the target root for
	// an item named name. Used by --dry-run previews.
	Path(name string) string
	// Transform maps a bundle onto the ordered list of files to write. The first
	// entry is always the primary rule file. Bundled resources (DownloadURL set)
	// follow. The returned paths are relative to the target root.
	Transform(b Bundle) ([]FileWrite, error)
}

// registry holds every supported target keyed by its flag name.
var registry = map[string]Target{
	(&cursorTarget{}).Name():   &cursorTarget{},
	(&codexTarget{}).Name():    &codexTarget{},
	(&windsurfTarget{}).Name(): &windsurfTarget{},
}

// Lookup returns the target for a flag value, or an error listing the valid
// targets when the value is unknown.
func Lookup(name string) (Target, error) {
	if t, ok := registry[name]; ok {
		return t, nil
	}
	return nil, fmt.Errorf("unknown --target %q: valid targets are %s", name, strings.Join(Names(), ", "))
}

// Names returns the sorted list of supported target flag values.
func Names() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// StripFrontmatter splits raw SKILL.md-style markdown into its body with the
// leading YAML frontmatter block removed. When the input does not open with a
// `---` fence (or the fence is unclosed) the input is returned unchanged as the
// body — a prompt has no frontmatter and must pass through verbatim. The body is
// returned with a single leading run of blank lines trimmed so the section
// starts cleanly.
func StripFrontmatter(md string) string {
	// Normalize only for detection; slicing uses byte offsets on the original.
	if !strings.HasPrefix(md, "---\n") && !strings.HasPrefix(md, "---\r\n") {
		return md
	}
	// Find the closing fence: a line that is exactly "---" after the opener.
	lines := strings.SplitAfter(md, "\n")
	// lines[0] is the opening "---\n". Scan for the closing fence.
	closeIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r\n") == "---" {
			closeIdx = i
			break
		}
	}
	if closeIdx == -1 {
		// Unclosed frontmatter — treat the whole thing as body (defensive).
		return md
	}
	body := strings.Join(lines[closeIdx+1:], "")
	return strings.TrimLeft(body, "\n\r")
}

// bundleSubdir writes each bundled file under <ruleDir>/<name>/<relpath>. This
// is shared by cursor and windsurf, whose rule dirs (.cursor/rules,
// .windsurf/rules) hold single-file rules, so a skill's bundled assets go into a
// per-rule sibling subfolder rather than polluting the rules dir root.
func bundleSubdir(ruleDir, name string, files []BundleFile) []FileWrite {
	writes := make([]FileWrite, 0, len(files))
	for _, f := range files {
		writes = append(writes, FileWrite{
			RelPath:     ruleDir + "/" + name + "/" + f.Path,
			DownloadURL: f.DownloadURL,
		})
	}
	return writes
}
