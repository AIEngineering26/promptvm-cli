// Package skills contains the filesystem and validation logic for the
// folder-shaped Agent Skills format (agentskills.io): SKILL.md frontmatter
// validation, bundled-file discovery, and safe path handling for downloads.
package skills

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// MaxSkillMDBytes is the backend limit for the literal SKILL.md payload.
	MaxSkillMDBytes = 1 << 20 // 1 MB
	// MaxFileBytes is the backend per-file limit for bundled resources.
	MaxFileBytes = 100 << 20 // 100 MB
)

// nameRe is the backend frontmatter name rule: kebab-case, starting with a
// lowercase letter or digit, max 64 characters.
var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ValidateName checks the SKILL.md frontmatter `name` field against the
// backend rule and returns a friendly error when it does not conform.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("SKILL.md frontmatter is missing a `name` field")
	}
	if !nameRe.MatchString(name) {
		return fmt.Errorf("invalid skill name %q: must be kebab-case — lowercase letters, digits, and hyphens, starting with a letter or digit, max 64 characters (e.g. \"pdf-tools\")", name)
	}
	return nil
}

// Frontmatter holds the SKILL.md fields the CLI surfaces. Parsing never
// rewrites the markdown — the literal bytes are uploaded as-is.
type Frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// ParseFrontmatter extracts the YAML frontmatter block from raw SKILL.md
// bytes. The file must start with a `---` line closed by another `---` line.
func ParseFrontmatter(md []byte) (*Frontmatter, error) {
	lines := bytes.SplitAfter(md, []byte("\n"))
	if len(lines) == 0 || strings.TrimRight(string(lines[0]), "\r\n") != "---" {
		return nil, fmt.Errorf("SKILL.md must start with a `---` YAML frontmatter block")
	}

	var block bytes.Buffer
	closed := false
	for _, line := range lines[1:] {
		if strings.TrimRight(string(line), "\r\n") == "---" {
			closed = true
			break
		}
		block.Write(line)
	}
	if !closed {
		return nil, fmt.Errorf("SKILL.md frontmatter is not closed with a `---` line")
	}

	var fm Frontmatter
	if err := yaml.Unmarshal(block.Bytes(), &fm); err != nil {
		return nil, fmt.Errorf("parsing SKILL.md frontmatter: %w", err)
	}
	return &fm, nil
}

// File is one bundled asset discovered inside a skill folder.
type File struct {
	// Path is the manifest path: relative to the skill folder root, with
	// forward slashes.
	Path string
	// AbsPath is the on-disk location of the file.
	AbsPath string
	// Size is the file size in bytes.
	Size int64
}

// Walk discovers the bundled files of a skill folder. It skips the root
// SKILL.md (uploaded separately as the skill body), dotfiles, and dot
// directories (.git, .DS_Store, …), and returns entries sorted by path.
func Walk(root string) ([]File, error) {
	var files []File
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "SKILL.md" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > MaxFileBytes {
			return fmt.Errorf("%s is %d bytes — bundled skill files are limited to 100 MB each", rel, info.Size())
		}
		files = append(files, File{Path: rel, AbsPath: p, Size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// SafeJoin joins a server-supplied manifest path onto a local destination
// directory, refusing absolute paths, backslashes, and any path that would
// escape dir.
func SafeJoin(dir, relPath string) (string, error) {
	if relPath == "" {
		return "", fmt.Errorf("empty path in skill manifest")
	}
	if strings.Contains(relPath, "\\") {
		return "", fmt.Errorf("unsafe path %q in skill manifest: backslashes are not allowed", relPath)
	}
	cleaned := path.Clean(relPath)
	if path.IsAbs(cleaned) ||
		cleaned == "." ||
		cleaned == ".." ||
		strings.HasPrefix(cleaned, "../") ||
		filepath.VolumeName(cleaned) != "" {
		return "", fmt.Errorf("unsafe path %q in skill manifest: escapes the destination directory", relPath)
	}
	return filepath.Join(dir, filepath.FromSlash(cleaned)), nil
}

// ReadSkillMD reads the literal SKILL.md bytes from a skill folder root,
// returning friendly errors for the common failure modes.
func ReadSkillMD(folder string) ([]byte, error) {
	mdPath := filepath.Join(folder, "SKILL.md")
	md, err := os.ReadFile(mdPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%s not found — a skill folder must contain SKILL.md at its root", mdPath)
		}
		return nil, err
	}
	if len(md) == 0 {
		return nil, fmt.Errorf("%s is empty", mdPath)
	}
	if len(md) > MaxSkillMDBytes {
		return nil, fmt.Errorf("%s is %d bytes — SKILL.md is limited to 1 MB", mdPath, len(md))
	}
	return md, nil
}
