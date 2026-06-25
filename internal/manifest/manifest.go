// Package manifest reads and writes the PromptVM context-sync manifest that
// declares which workspace/directory a repo syncs to and what gets captured.
//
// Three scopes mirror Claude Code's settings hierarchy:
//
//	local   → .promptvm/config.local.json   (gitignored, machine-specific)
//	project → .promptvm/config.json         (committable, shared with the team)
//	user    → <os.UserConfigDir()>/promptvm/config.json (global default)
//
// Resolution precedence is local → project → user (most specific wins). Scalar
// fields take the most-specific non-empty value. Capture-policy arrays
// (events, excludePaths) REPLACE rather than concat — the most-specific scope
// that sets them wins, so a repo can drop PreCompact (DX-5). Only the scope
// that explicitly declares an array overrides; an absent array (JSON `null`)
// inherits from the next-broader scope.
package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AIEngineering26/promptvm-cli/internal/config"
)

// Scope identifies one of the three manifest tiers.
type Scope string

const (
	ScopeLocal   Scope = "local"
	ScopeProject Scope = "project"
	ScopeUser    Scope = "user"
)

// SchemaVersion is the current manifest schema version.
const SchemaVersion = "1.0"

// Default capture policy values, applied when no scope sets them.
var (
	DefaultEvents       = []string{"SessionEnd", "PreCompact"}
	DefaultMode         = "summary"
	DefaultExcludePaths = []string{"**/.env*", "secrets/**", ".git/**"}
	DefaultGovernance   = "inherit"
)

// Manifest is the on-disk shape of a single scope's config file. Pointer and
// nil-slice fields let Resolve distinguish "unset" (inherit) from "set".
type Manifest struct {
	SchemaVersion string   `json:"schemaVersion,omitempty"`
	Workspace     string   `json:"workspace,omitempty"`
	Directory     string   `json:"directory,omitempty"`
	Capture       *Capture `json:"capture,omitempty"`
}

// Capture is the capture-policy block of a manifest.
type Capture struct {
	Enabled      *bool    `json:"enabled,omitempty"`
	Events       []string `json:"events,omitempty"`
	Mode         string   `json:"mode,omitempty"`
	ExcludePaths []string `json:"excludePaths,omitempty"`
	Redact       *bool    `json:"redact,omitempty"`
	Governance   string   `json:"governance,omitempty"`
}

// Resolved is the fully-defaulted, flattened view used by the runtime after
// merging all scopes. Every field carries a concrete value.
type Resolved struct {
	Workspace    string
	Directory    string
	Enabled      bool
	Events       []string
	Mode         string
	ExcludePaths []string
	Redact       bool
	Governance   string
}

// EventSelected reports whether the resolved policy captures on the given
// Claude Code hook event name.
func (r *Resolved) EventSelected(event string) bool {
	for _, e := range r.Events {
		if e == event {
			return true
		}
	}
	return false
}

// Path returns the manifest file path for the given scope. The project and
// local paths are anchored at repoRoot (or cwd when repoRoot is "").
func Path(scope Scope, repoRoot string) (string, error) {
	switch scope {
	case ScopeProject, ScopeLocal:
		base := repoRoot
		if base == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return "", err
			}
			base = cwd
		}
		name := "config.json"
		if scope == ScopeLocal {
			name = "config.local.json"
		}
		return filepath.Join(base, ".promptvm", name), nil
	case ScopeUser:
		dir, err := config.Dir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "config.json"), nil
	default:
		return "", fmt.Errorf("unknown manifest scope: %s", scope)
	}
}

// Read loads a single manifest file. A missing file yields (nil, nil) so
// callers can treat "absent" as "inherit".
func Read(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading manifest %s: %w", path, err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest %s: %w", path, err)
	}
	return &m, nil
}

// ReadScope loads the manifest for a scope, anchored at repoRoot. Returns
// (nil, nil) when the file does not exist.
func ReadScope(scope Scope, repoRoot string) (*Manifest, error) {
	path, err := Path(scope, repoRoot)
	if err != nil {
		return nil, err
	}
	return Read(path)
}

// Write atomically writes a manifest to path (tmp + rename), creating parent
// directories as needed.
func Write(path string, m *Manifest) error {
	if m.SchemaVersion == "" {
		m.SchemaVersion = SchemaVersion
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}
	data = append(data, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing temp manifest: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming temp manifest: %w", err)
	}
	return nil
}

// WriteScope writes a manifest for the given scope anchored at repoRoot.
func WriteScope(scope Scope, repoRoot string, m *Manifest) (string, error) {
	path, err := Path(scope, repoRoot)
	if err != nil {
		return "", err
	}
	return path, Write(path, m)
}

// Resolve merges the user → project → local scopes (broad to specific) into a
// fully-defaulted Resolved. repoRoot anchors the project/local files. Missing
// files are skipped. Capture-policy arrays REPLACE (most-specific wins).
func Resolve(repoRoot string) (*Resolved, error) {
	merged := &Manifest{Capture: &Capture{}}

	// Apply broad → specific so the most-specific value lands last.
	for _, scope := range []Scope{ScopeUser, ScopeProject, ScopeLocal} {
		m, err := ReadScope(scope, repoRoot)
		if err != nil {
			return nil, err
		}
		if m == nil {
			continue
		}
		overlay(merged, m)
	}

	return finalize(merged), nil
}

// overlay applies non-empty fields from src onto dst. Arrays REPLACE when src
// declares them (non-nil); bools and scalars overwrite when set.
func overlay(dst, src *Manifest) {
	if src.Workspace != "" {
		dst.Workspace = src.Workspace
	}
	if src.Directory != "" {
		dst.Directory = src.Directory
	}
	if src.Capture == nil {
		return
	}
	if dst.Capture == nil {
		dst.Capture = &Capture{}
	}
	sc, dc := src.Capture, dst.Capture
	if sc.Enabled != nil {
		v := *sc.Enabled
		dc.Enabled = &v
	}
	if sc.Events != nil { // explicit array → REPLACE (DX-5)
		dc.Events = append([]string(nil), sc.Events...)
	}
	if sc.Mode != "" {
		dc.Mode = sc.Mode
	}
	if sc.ExcludePaths != nil { // explicit array → REPLACE (DX-5)
		dc.ExcludePaths = append([]string(nil), sc.ExcludePaths...)
	}
	if sc.Redact != nil {
		v := *sc.Redact
		dc.Redact = &v
	}
	if sc.Governance != "" {
		dc.Governance = sc.Governance
	}
}

// finalize fills defaults for any field still unset after merging.
func finalize(m *Manifest) *Resolved {
	c := m.Capture
	if c == nil {
		c = &Capture{}
	}
	r := &Resolved{
		Workspace:    m.Workspace,
		Directory:    m.Directory,
		Enabled:      true,
		Events:       DefaultEvents,
		Mode:         DefaultMode,
		ExcludePaths: DefaultExcludePaths,
		Redact:       true,
		Governance:   DefaultGovernance,
	}
	if c.Enabled != nil {
		r.Enabled = *c.Enabled
	}
	if c.Events != nil {
		r.Events = c.Events
	}
	if c.Mode != "" {
		r.Mode = c.Mode
	}
	if c.ExcludePaths != nil {
		r.ExcludePaths = c.ExcludePaths
	}
	if c.Redact != nil {
		r.Redact = *c.Redact
	}
	if c.Governance != "" {
		r.Governance = c.Governance
	}
	if r.Directory == "" {
		r.Directory = "captures"
	}
	return r
}
