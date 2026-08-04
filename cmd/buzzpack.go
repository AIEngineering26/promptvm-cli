package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// Buzz persona-pack install target (`promptvm add <ref> --buzz-pack <dir>`).
//
// A Buzz persona pack (verified against `buzz pack validate` v0.5.1) is a
// directory:
//
//	<pack>/
//	  .plugin/plugin.json        manifest: required {id,name,version}; unknown keys warn-only
//	  agents/<name>.persona.md   frontmatter (deny_unknown_fields) + markdown body (system prompt)
//	  skills/<name>/SKILL.md     IDENTICAL to a PromptVM skill bundle
//	  .mcp.json                  IDENTICAL to a PromptVM .mcp.json ({mcpServers:{…}})
//
// A pack must contain >=1 persona referenced by manifest `personas[]` or it
// fails validation, so an empty target is scaffolded with a minimal, functional
// pack. Buzz skill/.mcp.json formats are byte-identical to what the existing
// installers already emit, so those installers are reused unchanged — only the
// target root, the tracker suppression, and the managed-persona skills list are
// buzz-specific.

// buzzManagedSentinel marks the persona whose `skills:` list `promptvm add`
// maintains. It lives in the persona BODY (an HTML comment) because Buzz persona
// frontmatter is deny_unknown_fields — an unknown frontmatter key is a hard
// validation error (probed against the real binary), so the marker cannot live
// there.
const buzzManagedSentinel = "<!-- promptvm-managed:"

// buzzManagedSentinelLine is the full comment written into a scaffolded persona.
const buzzManagedSentinelLine = "<!-- promptvm-managed: skills list maintained by `promptvm add`. Edit the body freely. -->"

// kebabPackName lowercases s and collapses every run of non-[a-z0-9] into a
// single hyphen, trimming leading/trailing hyphens and capping at 64 chars — the
// charset Buzz enforces for a persona `name` ([a-zA-Z0-9_-], max 64). Falls back
// to "promptvm-pack" when the result would be empty (e.g. ".", unicode-only, or
// an all-symbol basename), guaranteeing a non-empty manifest id/persona name.
func kebabPackName(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 64 {
		out = strings.Trim(out[:64], "-")
	}
	if out == "" {
		return "promptvm-pack"
	}
	return out
}

// resolveBuzzPackRoot returns the absolute, symlink-resolved pack root for dir.
// It resolves symlinks on the deepest existing ancestor (the pack dir itself may
// not exist yet), then re-joins the not-yet-created remainder, so containment
// checks compare real paths. This is the single trusted root every subsequent
// pack write is checked against.
func resolveBuzzPackRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("cannot resolve pack directory %q: %w", dir, err)
	}
	// Find the deepest existing ancestor and EvalSymlinks it.
	existing := abs
	var remainder []string
	for {
		if _, statErr := os.Lstat(existing); statErr == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break // reached the filesystem root
		}
		remainder = append([]string{filepath.Base(existing)}, remainder...)
		existing = parent
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		resolved = existing // best effort; existing was Lstat-able
	}
	return filepath.Join(append([]string{resolved}, remainder...)...), nil
}

// buzzPackContained verifies target resolves to a path inside root, defending
// against a hostile/malformed name escaping the pack directory. Skill names are
// already slug-validated and bundled files pass through skills.SafeJoin, so this
// is defense in depth for the directory names this package builds directly.
func buzzPackContained(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("refusing to write outside the pack: %s", target)
	}
	return nil
}

// buzzPackRefuseSymlinkedSubdir errors if <root>/<sub> exists and is a symlink,
// which would let writes/rewrites land outside the pack. Called before touching
// skills/ and agents/.
func buzzPackRefuseSymlinkedSubdir(root, sub string) error {
	p := filepath.Join(root, sub)
	if info, err := os.Lstat(p); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to write into %q: it is a symlink", p)
	}
	return nil
}

// buzzPluginManifest is the minimal .plugin/plugin.json we scaffold. Buzz allows
// (warn-only) extra keys, so this stays intentionally small.
type buzzPluginManifest struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Personas    []string `json:"personas"`
}

// ensureBuzzPackScaffold makes <root> a valid, functional persona pack when it
// has no manifest yet: it writes .plugin/plugin.json plus one promptvm-managed
// persona (a pack with zero personas fails validation, and skills are inert
// without a persona referencing them). It is idempotent — an existing manifest
// is left untouched. On --dry-run it prints the planned scaffold and writes
// nothing.
//
// Guard rails: if the managed persona file already exists but the manifest does
// not (a half-built pack), it refuses without --force rather than clobber it; an
// unparseable existing manifest is a hard error (never overwritten).
func ensureBuzzPackScaffold(cmd *cobra.Command, root string, dryRun, force bool) error {
	manifestPath := filepath.Join(root, ".plugin", "plugin.json")
	if data, err := os.ReadFile(manifestPath); err == nil {
		// Manifest exists — verify it parses, then leave the pack alone.
		var m map[string]json.RawMessage
		if jsonErr := json.Unmarshal(data, &m); jsonErr != nil {
			return fmt.Errorf("%s exists but is not valid JSON: %w", manifestPath, jsonErr)
		}
		return nil
	}

	packName := kebabPackName(filepath.Base(root))
	display := filepath.Base(root)
	if strings.TrimSpace(display) == "" || display == "." || display == string(os.PathSeparator) {
		display = packName
	}
	personaRel := filepath.ToSlash(filepath.Join("agents", packName+".persona.md"))
	personaPath := filepath.Join(root, "agents", packName+".persona.md")

	if err := buzzPackContained(root, personaPath); err != nil {
		return err
	}

	// Half-built pack: persona already present without a manifest.
	if _, err := os.Lstat(personaPath); err == nil && !force {
		return newNeedsForce("%s already exists without a manifest. Pass --force to scaffold over it.", personaPath) //nolint:staticcheck // PRD-mandated user-facing message
	}

	manifest := buzzPluginManifest{
		ID:          packName,
		Name:        display,
		Version:     "0.1.0",
		Description: "PromptVM pack",
		Personas:    []string{personaRel},
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	manifestBytes = append(manifestBytes, '\n')

	persona := "---\n" +
		"name: " + packName + "\n" +
		"display_name: " + display + "\n" +
		"description: Agent equipped with PromptVM skills\n" +
		"skills: []\n" +
		"---\n" +
		"You are an agent equipped with skills installed from PromptVM. Use them to help the team.\n\n" +
		buzzManagedSentinelLine + "\n"

	if dryRun {
		fmt.Fprintf(cmd.OutOrStdout(), "Dry-run: would scaffold Buzz pack at %s\n", root)
		fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", manifestPath)
		fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", personaPath)
		return nil
	}

	if err := buzzPackRefuseSymlinkedSubdir(root, "agents"); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		return mapWriteError(err, manifestPath)
	}
	if err := os.WriteFile(manifestPath, manifestBytes, 0o644); err != nil {
		return mapWriteError(err, manifestPath)
	}
	if err := os.MkdirAll(filepath.Dir(personaPath), 0o755); err != nil {
		return mapWriteError(err, personaPath)
	}
	if err := os.WriteFile(personaPath, []byte(persona), 0o644); err != nil {
		return mapWriteError(err, personaPath)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Scaffolded Buzz pack at %s\n", root)
	return nil
}

// regenerateManagedPersonaSkills refreshes the promptvm-managed persona's
// `skills:` frontmatter list after a skill install. It:
//   - finds the persona whose body carries the buzzManagedSentinel (expects
//     exactly one; 0 → prints a hint and returns; >1 → error);
//   - rebuilds the list as the sorted, de-duplicated union of every
//     skills/<d>/SKILL.md on disk plus any pre-existing entries the user
//     hand-added that don't live under skills/ (foreign entries are preserved);
//   - splices ONLY the `skills:` block, leaving every other frontmatter byte
//     (user model/temperature/triggers/…) and the body untouched, because Buzz
//     frontmatter is deny_unknown_fields and a full re-serialize would reorder or
//     drop keys.
//
// justInstalled is the pack-relative path of the skill just added, used only for
// the no-managed-persona hint.
func regenerateManagedPersonaSkills(cmd *cobra.Command, root, justInstalled string) error {
	agentsDir := filepath.Join(root, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		// No agents dir (e.g. pre-existing pack with personas elsewhere): hint.
		fmt.Fprintf(cmd.OutOrStdout(), "Added %s — add it to a persona's `skills:` list to activate it.\n", justInstalled)
		return nil
	}

	var managed []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".persona.md") {
			continue
		}
		p := filepath.Join(agentsDir, e.Name())
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			continue
		}
		if personaHasSentinel(string(data)) {
			managed = append(managed, p)
		}
	}

	switch len(managed) {
	case 0:
		fmt.Fprintf(cmd.OutOrStdout(), "Added %s — add it to a persona's `skills:` list to activate it.\n", justInstalled)
		return nil
	case 1:
		// proceed
	default:
		sort.Strings(managed)
		return fmt.Errorf("multiple promptvm-managed personas found (%s); resolve to one before installing", strings.Join(managed, ", ")) //nolint:staticcheck // PRD-mandated user-facing message
	}

	personaPath := managed[0]
	if info, err := os.Lstat(personaPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to rewrite %q: it is a symlink", personaPath)
	}

	data, err := os.ReadFile(personaPath)
	if err != nil {
		return err
	}
	skillDirs, err := listPackSkillDirs(root)
	if err != nil {
		return err
	}
	updated, changed, err := spliceSkillsList(string(data), skillDirs)
	if err != nil {
		return fmt.Errorf("updating %s: %w", personaPath, err)
	}
	if !changed {
		return nil
	}
	if err := os.WriteFile(personaPath, []byte(updated), 0o644); err != nil {
		return mapWriteError(err, personaPath)
	}
	return nil
}

// personaHasSentinel reports whether the persona's body (after the frontmatter)
// contains the managed sentinel. It only inspects lines after the closing `---`
// so a stray match inside frontmatter can't happen.
func personaHasSentinel(content string) bool {
	lines := strings.Split(content, "\n")
	inFrontmatter := false
	frontmatterClosed := false
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if i == 0 && t == "---" {
			inFrontmatter = true
			continue
		}
		if inFrontmatter && !frontmatterClosed {
			if t == "---" {
				frontmatterClosed = true
			}
			continue
		}
		if strings.HasPrefix(t, buzzManagedSentinel) {
			return true
		}
	}
	return false
}

// listPackSkillDirs returns the pack-relative "skills/<d>" path for every
// skills/<d>/SKILL.md present on disk, sorted.
func listPackSkillDirs(root string) ([]string, error) {
	skillsDir := filepath.Join(root, "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(skillsDir, e.Name(), "SKILL.md")); statErr == nil {
			out = append(out, "skills/"+e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// spliceSkillsList replaces ONLY the `skills:` block within the frontmatter of a
// persona file, preserving every other byte. The new list is the sorted,
// de-duplicated union of skillDirs (from disk) and any existing entries that do
// NOT live under skills/ (user-added foreign refs are kept, not dropped). Returns
// the new content and whether it changed. Errors if no frontmatter is found.
func spliceSkillsList(content string, skillDirs []string) (string, bool, error) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return content, false, fmt.Errorf("persona has no YAML frontmatter")
	}
	// Locate the frontmatter close.
	fmEnd := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			fmEnd = i
			break
		}
	}
	if fmEnd == -1 {
		return content, false, fmt.Errorf("persona frontmatter is not closed")
	}

	// Find the `skills:` key line within [1, fmEnd) and the extent of its block
	// (the key line plus any following indented list items / continuations up to
	// the next top-level key or the frontmatter close).
	skillsStart, skillsEnd := -1, -1
	var existing []string
	for i := 1; i < fmEnd; i++ {
		if isYAMLKey(lines[i], "skills") {
			skillsStart = i
			existing = append(existing, parseInlineList(lines[i])...)
			j := i + 1
			for j < fmEnd {
				lt := lines[j]
				trimmed := strings.TrimSpace(lt)
				// A block list item or an indented continuation belongs to skills:.
				if strings.HasPrefix(trimmed, "- ") {
					existing = append(existing, strings.TrimSpace(strings.TrimPrefix(trimmed, "-")))
					j++
					continue
				}
				// Next top-level key (no leading whitespace, has a colon) ends the block.
				break
			}
			skillsEnd = j
			break
		}
	}

	// Preserve foreign entries (anything not under skills/).
	set := map[string]bool{}
	var union []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || set[v] {
			return
		}
		set[v] = true
		union = append(union, v)
	}
	for _, d := range skillDirs {
		add(d)
	}
	for _, e := range existing {
		if !strings.HasPrefix(e, "skills/") {
			add(e)
		}
	}
	sort.Strings(union)

	block := renderSkillsBlock(union)

	if skillsStart == -1 {
		// No skills: key present — insert one just before the frontmatter close.
		newLines := append([]string{}, lines[:fmEnd]...)
		newLines = append(newLines, block...)
		newLines = append(newLines, lines[fmEnd:]...)
		out := strings.Join(newLines, "\n")
		return out, out != content, nil
	}

	newLines := append([]string{}, lines[:skillsStart]...)
	newLines = append(newLines, block...)
	newLines = append(newLines, lines[skillsEnd:]...)
	out := strings.Join(newLines, "\n")
	return out, out != content, nil
}

// renderSkillsBlock renders the frontmatter `skills:` lines. Empty → inline
// `skills: []`; otherwise a block list with two-space indented `- ` items.
func renderSkillsBlock(items []string) []string {
	if len(items) == 0 {
		return []string{"skills: []"}
	}
	out := []string{"skills:"}
	for _, it := range items {
		out = append(out, "  - "+it)
	}
	return out
}

// isYAMLKey reports whether line is a top-level (unindented) YAML mapping for
// key, i.e. `key:` or `key: …`.
func isYAMLKey(line, key string) bool {
	if line != strings.TrimLeft(line, " \t") {
		return false // indented — not a top-level key
	}
	rest, ok := strings.CutPrefix(line, key)
	if !ok {
		return false
	}
	return strings.HasPrefix(rest, ":")
}

// parseInlineList extracts entries from an inline flow list on a `skills:` line
// (`skills: [a, b]`). Returns nil for block form (`skills:`) or empty (`[]`).
func parseInlineList(line string) []string {
	_, after, ok := strings.Cut(line, ":")
	if !ok {
		return nil
	}
	after = strings.TrimSpace(after)
	if !strings.HasPrefix(after, "[") || !strings.HasSuffix(after, "]") {
		return nil
	}
	inner := strings.TrimSpace(after[1 : len(after)-1])
	if inner == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(inner, ",") {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}
