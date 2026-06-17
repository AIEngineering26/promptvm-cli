package agentskill

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AIEngineering26/promptvm-cli/internal/config"
)

// configDir resolves the directory hosting the tracker marker. It is indirected
// through a variable so tests can redirect it without touching the real config
// home.
var configDir = config.Dir

// Status values for the tracker marker.
const (
	// StatusInstalled means the skill was written to disk.
	StatusInstalled = "installed"
	// StatusSkipped means the user opted out (reserved; the env opt-out
	// currently short-circuits before writing a marker).
	StatusSkipped = "skipped"
	// StatusNotInstalled is reported by `agent status` when no marker exists.
	// It is never persisted.
	StatusNotInstalled = "not-installed"
)

// TrackedTarget records one installed target location.
type TrackedTarget struct {
	Key  string `json:"key"`
	Path string `json:"path"`
}

// Tracker is the on-disk marker recording what the CLI installed. Its presence
// is what makes first-run auto-install idempotent.
type Tracker struct {
	Name        string          `json:"name"`
	Version     int             `json:"version"`
	Checksum    string          `json:"checksum"`
	Status      string          `json:"status"`
	Targets     []TrackedTarget `json:"targets,omitempty"`
	InstalledAt string          `json:"installed_at"`

	path string
}

// TrackerPath returns the marker path (config.Dir()/agent-skill.json).
func TrackerPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "agent-skill.json"), nil
}

// LoadTracker reads the marker. Returns (nil, nil) when absent.
func LoadTracker() (*Tracker, error) {
	path, err := TrackerPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading agent-skill marker: %w", err)
	}
	var t Tracker
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("parsing agent-skill marker: %w", err)
	}
	t.path = path
	return &t, nil
}

// Exists reports whether the marker file is present (any status).
func Exists() (bool, error) {
	path, err := TrackerPath()
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// Save atomically writes the marker (tmp + rename).
func (t *Tracker) Save() error {
	if t.path == "" {
		p, err := TrackerPath()
		if err != nil {
			return err
		}
		t.path = p
	}
	if err := os.MkdirAll(filepath.Dir(t.path), 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling marker: %w", err)
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

// Clear removes the marker file. A missing file is not an error.
func Clear() error {
	path, err := TrackerPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing agent-skill marker: %w", err)
	}
	return nil
}
