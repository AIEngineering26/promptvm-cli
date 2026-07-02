// Package agentskill bundles the canonical "promptvm" Agent Skill with the CLI
// and installs it into the local agent skills directories for Claude Code and
// Codex, so any agent session already knows how to drive PromptVM.
//
// Both agents read the same folder-shaped Agent Skill format
// (<skills-dir>/<name>/SKILL.md with YAML frontmatter):
//   - Claude Code: ~/.claude/skills (user) or ./.claude/skills (project)
//   - Codex:       $CODEX_HOME/skills else ~/.agents/skills (user),
//     or ./.agents/skills (project)
package agentskill

import (
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed all:data/promptvm
var content embed.FS

const (
	// Name is the skill folder + frontmatter name (valid kebab per
	// internal/skills.ValidateName).
	Name = "promptvm"

	// Version is the bundled skill revision. Bump it whenever the embedded
	// data/promptvm content changes so `agent status` and first-run can detect
	// that an update is available.
	Version = 2

	// embedRoot is the path of the embedded skill folder inside content.
	embedRoot = "data/promptvm"
)

// Scope selects user-global vs project-local installation.
type Scope string

const (
	// ScopeUser installs into the user's home agent directories.
	ScopeUser Scope = "user"
	// ScopeProject installs into the current working directory.
	ScopeProject Scope = "project"
)

// Target is one agent's skills location.
type Target struct {
	Key   string // "claude" | "codex"
	Label string // human-readable label
}

// AllTargets returns the known install targets.
func AllTargets() []Target {
	return []Target{
		{Key: "claude", Label: "Claude Code"},
		{Key: "codex", Label: "Codex"},
	}
}

// TargetByKey returns the target with the given key.
func TargetByKey(key string) (Target, bool) {
	for _, t := range AllTargets() {
		if t.Key == key {
			return t, true
		}
	}
	return Target{}, false
}

// BaseDir returns the skills base directory for this target and scope (the
// folder that will contain the promptvm/ skill folder).
func (t Target) BaseDir(scope Scope) (string, error) {
	switch t.Key {
	case "claude":
		root, err := scopeRoot(scope)
		if err != nil {
			return "", err
		}
		return filepath.Join(root, ".claude", "skills"), nil
	case "codex":
		if scope == ScopeUser {
			// Honor CODEX_HOME only when it is an absolute path; a relative
			// value would resolve against the process cwd and silently scatter
			// installs, so we fall back to ~/.agents in that case.
			if ch := strings.TrimSpace(os.Getenv("CODEX_HOME")); ch != "" && filepath.IsAbs(ch) {
				return filepath.Join(ch, "skills"), nil
			}
		}
		root, err := scopeRoot(scope)
		if err != nil {
			return "", err
		}
		return filepath.Join(root, ".agents", "skills"), nil
	default:
		return "", fmt.Errorf("unknown target %q", t.Key)
	}
}

// DestDir returns the install folder for this target/scope: <baseDir>/promptvm.
func (t Target) DestDir(scope Scope) (string, error) {
	base, err := t.BaseDir(scope)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, Name), nil
}

func scopeRoot(scope Scope) (string, error) {
	switch scope {
	case ScopeProject:
		return os.Getwd()
	case ScopeUser:
		return os.UserHomeDir()
	default:
		return "", fmt.Errorf("unknown scope %q", scope)
	}
}

// Checksum returns the sha256 of the bundled SKILL.md, for change detection.
func Checksum() string {
	data, err := content.ReadFile(embedRoot + "/SKILL.md")
	if err != nil {
		return ""
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

// Files returns the bundled file paths relative to the skill folder root, with
// forward slashes, sorted.
func Files() ([]string, error) {
	var out []string
	err := fs.WalkDir(content, embedRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		out = append(out, relPath(p))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// InstalledTarget records where the skill was written.
type InstalledTarget struct {
	Key  string `json:"key"`
	Path string `json:"path"`
}

// Install writes the bundled skill into each target's skill folder for the
// given scope and returns the per-target install locations.
//
// When a target's promptvm folder already exists and force is false, Install
// behaves as follows based on the installed SKILL.md:
//   - checksum matches the bundle      → idempotent no-op
//   - folder has no readable SKILL.md  → treated as not installed; repaired
//   - checksum differs                 → error (use force to overwrite)
//
// On error the returned slice contains the targets installed before the
// failure (Install processes targets sequentially and stops at the first one
// that errors).
func Install(scope Scope, targets []Target, force bool) ([]InstalledTarget, error) {
	results := make([]InstalledTarget, 0, len(targets))
	for _, t := range targets {
		dest, err := t.DestDir(scope)
		if err != nil {
			return results, err
		}
		if _, statErr := os.Stat(dest); statErr == nil && !force {
			switch installedChecksum(dest) {
			case Checksum():
				// Already up to date; nothing to do.
				results = append(results, InstalledTarget{Key: t.Key, Path: dest})
				continue
			case "":
				// Folder exists but has no readable SKILL.md (interrupted
				// write, manual mkdir). Fall through and repair it — the
				// per-file atomic writes below are themselves idempotent.
			default:
				return results, fmt.Errorf("skill already installed at %s; use --force to overwrite", dest)
			}
		}
		if err := writeEmbedded(dest); err != nil {
			return results, fmt.Errorf("installing %s skill: %w", t.Label, err)
		}
		results = append(results, InstalledTarget{Key: t.Key, Path: dest})
	}
	return results, nil
}

// InstallBestEffort installs each target independently, skipping (rather than
// aborting on) any target that errors, and returns the targets that did
// install. Used by first-run auto-install so a pre-existing/conflicting folder
// for one agent never blocks installing the other — or permanently wedges the
// first-run path before a marker can be written.
func InstallBestEffort(scope Scope, targets []Target) []InstalledTarget {
	installed := make([]InstalledTarget, 0, len(targets))
	for _, t := range targets {
		res, err := Install(scope, []Target{t}, false)
		if err != nil {
			continue
		}
		installed = append(installed, res...)
	}
	return installed
}

// Uninstall removes the given promptvm skill folders. The paths come from the
// tracker (an on-disk, user-editable file), so it defends against a corrupted
// or hand-edited marker: it cleans each path, removes only folders whose final
// element is the skill name, and refuses to follow a symlinked leaf. A missing
// folder is not an error.
func Uninstall(paths []string) error {
	for _, p := range paths {
		if p == "" {
			continue
		}
		clean := filepath.Clean(p)
		if filepath.Base(clean) != Name {
			continue
		}
		// Never traverse a symlinked leaf — remove the link itself at most.
		if fi, err := os.Lstat(clean); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if err := os.RemoveAll(clean); err != nil {
			return fmt.Errorf("removing %s: %w", clean, err)
		}
	}
	return nil
}

// writeEmbedded walks the embedded skill folder and writes every file under
// dest using the atomic tmp+rename pattern.
func writeEmbedded(dest string) error {
	return fs.WalkDir(content, embedRoot, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		target := filepath.Join(dest, filepath.FromSlash(relPath(p)))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := content.ReadFile(p)
		if err != nil {
			return err
		}
		return atomicWrite(target, data)
	})
}

// relPath converts an embedded path to a forward-slash path relative to the
// skill folder root ("" for the root itself).
func relPath(p string) string {
	rel := strings.TrimPrefix(p, embedRoot)
	return strings.TrimPrefix(rel, "/")
}

// atomicWrite writes data to path via a sibling .tmp file + rename.
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}

// installedChecksum returns the sha256 of an installed SKILL.md, or "".
func installedChecksum(dest string) string {
	data, err := os.ReadFile(filepath.Join(dest, "SKILL.md"))
	if err != nil {
		return ""
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}
