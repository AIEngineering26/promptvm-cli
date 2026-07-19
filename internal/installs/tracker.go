// Package installs manages a generic sidecar tracker for every kind of
// marketplace content installed via `promptvm add` (skill, agent, command,
// prompt, hook, mcp, settings). It records provenance (name, canonical ref,
// kind, target path, timestamp) so a future `promptvm list/remove/update` has a
// stable, kind-agnostic index. Hooks continue to also use the hook-specific
// .promptvm-hooks.json sidecar for settings-merge ownership; this file is
// additive and never authoritative for uninstall of merged config.
package installs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// FileName is the sidecar filename, written inside the .claude root.
const FileName = ".promptvm-installs.json"

// Entry is one recorded install.
type Entry struct {
	Name        string `json:"name"`
	Ref         string `json:"ref"`
	Kind        string `json:"kind"`
	Target      string `json:"target"`
	InstalledAt string `json:"installed_at"`
}

// Tracker is the on-disk sidecar shape.
type Tracker struct {
	Installs []Entry `json:"installs"`
	path     string
}

// PathIn returns the sidecar path inside the given .claude root.
func PathIn(claudeRoot string) string {
	return filepath.Join(claudeRoot, FileName)
}

// Load reads the sidecar at path; a missing file yields an empty tracker.
func Load(path string) (*Tracker, error) {
	t := &Tracker{Installs: []Entry{}, path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return t, nil
		}
		return nil, fmt.Errorf("reading install tracker: %w", err)
	}
	if len(data) == 0 {
		return t, nil
	}
	if err := json.Unmarshal(data, t); err != nil {
		return nil, fmt.Errorf("parsing install tracker: %w", err)
	}
	t.path = path
	return t, nil
}

// Add inserts or replaces the entry with the same Ref (or, absent a ref, the
// same Kind+Name) so re-installs update in place instead of duplicating.
func (t *Tracker) Add(e Entry) {
	for i, existing := range t.Installs {
		if e.Ref != "" && existing.Ref == e.Ref {
			t.Installs[i] = e
			return
		}
		if e.Ref == "" && existing.Kind == e.Kind && existing.Name == e.Name {
			t.Installs[i] = e
			return
		}
	}
	t.Installs = append(t.Installs, e)
}

// Save atomically writes the sidecar (tmp + rename).
func (t *Tracker) Save() error {
	dir := filepath.Dir(t.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling install tracker: %w", err)
	}
	data = append(data, '\n')
	tmp := t.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := os.Rename(tmp, t.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}

// Record is a convenience: load the sidecar in claudeRoot, upsert the entry, and
// save. It is best-effort at the call site (the caller swallows errors).
func Record(claudeRoot string, e Entry) error {
	path := PathIn(claudeRoot)
	t, err := Load(path)
	if err != nil {
		return err
	}
	t.Add(e)
	return t.Save()
}
