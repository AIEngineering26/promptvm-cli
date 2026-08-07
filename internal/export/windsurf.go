package export

import "fmt"

// windsurfTarget writes Windsurf rules.
//
// Format (Windsurf "Rules"): one markdown file per rule under
// `.windsurf/rules/<name>.md`. Windsurf rules support optional YAML frontmatter
// with an activation `trigger` and a `description`:
//
//	---
//	trigger: model_decision
//	description: <one-line description>
//	---
//
// `trigger: model_decision` lets the model decide when to apply the rule based
// on the description (the closest analogue to an on-demand skill). The
// frontmatter is emitted whenever a description is present; a description-less
// prompt is written as plain markdown with no frontmatter.
type windsurfTarget struct{}

func (windsurfTarget) Name() string { return "windsurf" }

func (windsurfTarget) Path(name string) string {
	return ".windsurf/rules/" + name + ".md"
}

func (w windsurfTarget) Transform(b Bundle) ([]FileWrite, error) {
	if b.Name == "" {
		return nil, fmt.Errorf("windsurf export requires a name")
	}
	var content string
	if b.Description != "" {
		content = "---\n" +
			"trigger: model_decision\n" +
			"description: " + b.Description + "\n" +
			"---\n" +
			ensureTrailingNewline(b.Body)
	} else {
		content = ensureTrailingNewline(b.Body)
	}

	writes := []FileWrite{{
		RelPath: w.Path(b.Name),
		Content: []byte(content),
	}}
	writes = append(writes, bundleSubdir(".windsurf/rules", b.Name, b.Files)...)
	return writes, nil
}
