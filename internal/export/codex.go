package export

import "fmt"

// codexTarget writes OpenAI Codex instructions.
//
// Format (Codex): Codex reads project guidance from a single `AGENTS.md` file at
// the project root (the emerging cross-tool convention Codex adopted). Rather
// than one file per item, each exported skill/prompt is appended to AGENTS.md as
// a section headed by `## <name>`:
//
//	## <name>
//
//	<body>
//
// If AGENTS.md already exists the section is appended (Append=true → append-or-
// create); otherwise it is created. Bundled files are NOT written for Codex:
// AGENTS.md is a single flat instruction file with no per-rule directory to hold
// resources, so bundled assets are intentionally skipped (documented in
// decisions.md).
type codexTarget struct{}

func (codexTarget) Name() string { return "codex" }

// Path is the shared instruction file; the item name does not change it (every
// item appends a section to the same AGENTS.md).
func (codexTarget) Path(string) string { return "AGENTS.md" }

func (c codexTarget) Transform(b Bundle) ([]FileWrite, error) {
	if b.Name == "" {
		return nil, fmt.Errorf("codex export requires a name")
	}
	// A leading blank line separates this section from any preceding content when
	// appended to an existing AGENTS.md; the heading + body follow.
	section := "\n## " + b.Name + "\n\n" + ensureTrailingNewline(b.Body)
	return []FileWrite{{
		RelPath: c.Path(b.Name),
		Content: []byte(section),
		Append:  true,
	}}, nil
}
