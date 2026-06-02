// Package hooks manages Claude Code settings.json files and the PromptVM
// hook tracker sidecar (.promptvm-hooks.json). It provides read/write helpers
// used by the hooks CLI commands.
//
// The core types and functions are split across:
//   - settings.go: Settings, Scope, ReadSettings, MergeHook, RemoveHook, Write, Checksum
//   - tracker.go:  Tracker, TrackedHook, LoadTracker, TrackerFilePath
package hooks
