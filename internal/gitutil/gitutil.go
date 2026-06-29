// Package gitutil provides the minimal git introspection the context-sync
// configurator needs: repo root, origin remote, current branch, HEAD sha, plus
// an idempotent .gitignore helper anchored at the repo root (DX-10).
//
// It shells out to the `git` binary (already a hard dependency of the workflow)
// and degrades gracefully: every function returns a usable zero value when not
// in a repo or when git is unavailable, so callers never crash a hook.
package gitutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Repo describes the git context of a directory.
type Repo struct {
	Root      string
	RemoteURL string
	Branch    string
	HeadSha   string
}

// run executes a git command in dir and returns trimmed stdout, or "" on error.
func run(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Detect inspects dir (or cwd when dir is "") and returns its git context. The
// boolean reports whether dir is inside a git work tree.
func Detect(dir string) (*Repo, bool) {
	if dir == "" {
		if cwd, err := os.Getwd(); err == nil {
			dir = cwd
		}
	}
	root := run(dir, "rev-parse", "--show-toplevel")
	if root == "" {
		return &Repo{}, false
	}
	return &Repo{
		Root:      root,
		RemoteURL: run(dir, "config", "--get", "remote.origin.url"),
		Branch:    run(dir, "rev-parse", "--abbrev-ref", "HEAD"),
		HeadSha:   run(dir, "rev-parse", "HEAD"),
	}, true
}

// Slug normalizes a git remote URL to a canonical "owner/repo" project slug
// (FR-7): the last two path segments, with any ".git" suffix and surrounding
// noise stripped. It collapses the common remote shapes to the same value:
//
//	git@github.com:owner/repo.git        → owner/repo
//	https://github.com/owner/repo.git    → owner/repo
//	https://github.com/owner/repo        → owner/repo
//	ssh://git@host:22/owner/repo.git     → owner/repo
//
// Returns "" when no usable owner/repo can be derived (callers fall back to the
// repo-root basename for the "Local / no remote" bucket, FR-10).
func Slug(remoteURL string) string {
	s := strings.TrimSpace(remoteURL)
	if s == "" {
		return ""
	}

	// Drop a scheme:// prefix (https://, ssh://, git://).
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	// Drop user@ credentials (git@github.com:..., user@host/...).
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	// Normalize the scp-style host:owner/repo separator to a slash.
	s = strings.Replace(s, ":", "/", 1)
	// Strip any trailing slashes and a trailing .git.
	s = strings.TrimSuffix(strings.TrimRight(s, "/"), ".git")

	parts := strings.Split(s, "/")
	// Keep only non-empty segments.
	clean := parts[:0]
	for _, p := range parts {
		if p != "" {
			clean = append(clean, p)
		}
	}
	if len(clean) < 2 {
		return ""
	}
	owner := clean[len(clean)-2]
	repo := clean[len(clean)-1]
	// Skip a bare host with no owner (e.g. "github.com/repo" → not owner/repo).
	if strings.Contains(owner, ".") && len(clean) == 2 {
		return ""
	}
	if owner == "" || repo == "" {
		return ""
	}
	return owner + "/" + repo
}

// EnsureGitignore makes sure pattern appears in <root>/.gitignore. It reads the
// file (creating it if absent), checks for an exact-line match, and appends the
// pattern atomically only if missing. Returns true if it added the pattern.
func EnsureGitignore(root, pattern string) (bool, error) {
	path := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == pattern {
			return false, nil // already present
		}
	}

	var b strings.Builder
	b.Write(data)
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		b.WriteByte('\n')
	}
	if len(data) == 0 {
		b.WriteString("# PromptVM context-sync (machine-local)\n")
	}
	b.WriteString(pattern)
	b.WriteByte('\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return false, err
	}
	return true, nil
}
