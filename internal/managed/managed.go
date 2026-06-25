// Package managed detects OS-level Claude Code managed (enterprise) settings so
// the configurator honors an admin's hook policy (SEC-6). `sync init` warns/
// aborts when hooks are disabled; `sync run` no-ops under disableAllHooks.
package managed

import (
	"encoding/json"
	"os"
	"runtime"
)

// Policy is the subset of managed-settings we care about.
type Policy struct {
	// DisableAllHooks, when true, means no hooks may run — the configurator
	// must not install and the uploader must no-op.
	DisableAllHooks bool `json:"disableAllHooks"`
	// Path is the managed-settings file that was read (empty if none).
	Path string `json:"-"`
	// Present reports whether a managed-settings file exists.
	Present bool `json:"-"`
}

// managedSettingsPath returns the OS managed-settings path for Claude Code.
func managedSettingsPath() string {
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/ClaudeCode/managed-settings.json"
	case "windows":
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return pd + `\ClaudeCode\managed-settings.json`
	default: // linux and others
		return "/etc/claude-code/managed-settings.json"
	}
}

// pathOverride lets tests point at a fixture instead of the real OS path.
var pathOverride string

// Detect reads the managed-settings policy. A missing file is not an error —
// it returns a zero Policy with Present=false.
func Detect() (*Policy, error) {
	path := pathOverride
	if path == "" {
		path = managedSettingsPath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Policy{Path: path, Present: false}, nil
		}
		return &Policy{Path: path}, err
	}
	p := &Policy{Path: path, Present: true}
	// Tolerate unknown keys.
	var raw struct {
		DisableAllHooks bool `json:"disableAllHooks"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return p, err
	}
	p.DisableAllHooks = raw.DisableAllHooks
	return p, nil
}

// HooksDisabled is a convenience that reports whether managed settings forbid
// hooks. Any read error degrades to false (fail-open for detection only; the
// server governance layer is the real authority).
func HooksDisabled() bool {
	p, err := Detect()
	if err != nil {
		return false
	}
	return p.Present && p.DisableAllHooks
}
