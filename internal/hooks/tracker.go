package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// TrackerFilePath returns path to the sidecar file for the given scope.
func TrackerFilePath(scope Scope) (string, error) {
	switch scope {
	case ScopeProject:
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return filepath.Join(cwd, ".claude", ".promptvm-hooks.json"), nil
	case ScopeLocal:
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return filepath.Join(cwd, ".claude", ".promptvm-hooks.local.json"), nil
	case ScopeUser:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".claude", ".promptvm-hooks.json"), nil
	default:
		return "", fmt.Errorf("unknown scope: %s", scope)
	}
}

// TrackedHook represents a managed hook entry.
type TrackedHook struct {
	Slug        string   `json:"slug"`
	Version     int      `json:"version"`
	SourceURL   string   `json:"source_url,omitempty"`
	InstalledAt string   `json:"installed_at"`
	Events      []string `json:"events"`
	Checksum    string   `json:"checksum"`
}

// Tracker manages the sidecar file.
type Tracker struct {
	Hooks []TrackedHook `json:"hooks"`
	path  string
}

// LoadTracker reads the sidecar file. Returns empty tracker if not found.
func LoadTracker(scope Scope) (*Tracker, error) {
	path, err := TrackerFilePath(scope)
	if err != nil {
		return nil, err
	}
	return LoadTrackerFromPath(path)
}

// LoadTrackerFromPath reads a tracker from a specific file path.
func LoadTrackerFromPath(path string) (*Tracker, error) {
	t := &Tracker{
		Hooks: []TrackedHook{},
		path:  path,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return t, nil
		}
		return nil, fmt.Errorf("reading tracker file: %w", err)
	}

	if err := json.Unmarshal(data, t); err != nil {
		return nil, fmt.Errorf("parsing tracker file: %w", err)
	}
	t.path = path

	return t, nil
}

// Add adds or updates a tracked hook entry. If a hook with the same slug
// already exists, it is replaced.
func (t *Tracker) Add(hook TrackedHook) {
	for i, h := range t.Hooks {
		if h.Slug == hook.Slug {
			t.Hooks[i] = hook
			return
		}
	}
	t.Hooks = append(t.Hooks, hook)
}

// Remove removes a tracked hook by slug. Returns true if found.
func (t *Tracker) Remove(slug string) bool {
	for i, h := range t.Hooks {
		if h.Slug == slug {
			t.Hooks = append(t.Hooks[:i], t.Hooks[i+1:]...)
			return true
		}
	}
	return false
}

// Get returns a tracked hook by slug, or nil.
func (t *Tracker) Get(slug string) *TrackedHook {
	for i := range t.Hooks {
		if t.Hooks[i].Slug == slug {
			return &t.Hooks[i]
		}
	}
	return nil
}

// Save atomically writes the tracker file.
func (t *Tracker) Save() error {
	dir := filepath.Dir(t.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling tracker: %w", err)
	}
	data = append(data, '\n')

	tmpPath := t.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}

	if err := os.Rename(tmpPath, t.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming temp file: %w", err)
	}

	return nil
}

// EventsForSlug returns the event keys associated with a slug.
func (t *Tracker) EventsForSlug(slug string) []string {
	h := t.Get(slug)
	if h == nil {
		return nil
	}
	return h.Events
}
