package export

import "fmt"

// cursorTarget writes Cursor MDC rules.
//
// Format (Cursor "Project Rules"): one `.mdc` file per rule under
// `.cursor/rules/<name>.mdc`. An MDC file is YAML frontmatter followed by the
// markdown body. The frontmatter fields Cursor reads are:
//
//	---
//	description: <one-line description>
//	globs:
//	alwaysApply: false
//	---
//
// `globs` is intentionally left blank (the rule is surfaced by description /
// agent decision, not auto-attached to a file glob) and `alwaysApply` is false
// so the rule is applied on demand rather than injected into every request.
type cursorTarget struct{}

func (cursorTarget) Name() string { return "cursor" }

func (cursorTarget) Path(name string) string {
	return ".cursor/rules/" + name + ".mdc"
}

func (c cursorTarget) Transform(b Bundle) ([]FileWrite, error) {
	if b.Name == "" {
		return nil, fmt.Errorf("cursor export requires a name")
	}
	content := "---\n" +
		"description: " + b.Description + "\n" +
		"globs: \n" +
		"alwaysApply: false\n" +
		"---\n" +
		ensureTrailingNewline(b.Body)

	writes := []FileWrite{{
		RelPath: c.Path(b.Name),
		Content: []byte(content),
	}}
	writes = append(writes, bundleSubdir(".cursor/rules", b.Name, b.Files)...)
	return writes, nil
}

// ensureTrailingNewline guarantees body ends with exactly one newline so
// concatenations don't run the frontmatter close and body together and files
// end cleanly. An empty body yields "".
func ensureTrailingNewline(body string) string {
	if body == "" {
		return ""
	}
	if body[len(body)-1] == '\n' {
		return body
	}
	return body + "\n"
}
