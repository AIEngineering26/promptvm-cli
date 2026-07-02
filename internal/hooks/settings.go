package hooks

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Scope determines which settings file to target.
type Scope string

const (
	ScopeProject Scope = "project"
	ScopeUser    Scope = "user"
	// ScopeLocal targets .claude/settings.local.json — the per-machine,
	// gitignored project settings tier Claude Code reads with highest
	// precedence. Added for context-sync (DX-2).
	ScopeLocal Scope = "local"
)

// SettingsFilePath returns the path to the Claude Code settings.json for the
// given scope, anchoring project/local scopes at the current working directory.
func SettingsFilePath(scope Scope) (string, error) {
	return SettingsFilePathAt(scope, "")
}

// SettingsFilePathAt returns the settings.json path for the given scope,
// anchoring project/local scopes at root. When root is "" it falls back to the
// current working directory. Callers that know the git repo root (e.g.
// `sync init`) MUST pass it so the settings file lands next to the manifest at
// the repo root rather than wherever the command happened to be invoked from.
func SettingsFilePathAt(scope Scope, root string) (string, error) {
	switch scope {
	case ScopeProject, ScopeLocal:
		base := root
		if base == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return "", err
			}
			base = cwd
		}
		name := "settings.json"
		if scope == ScopeLocal {
			name = "settings.local.json"
		}
		return filepath.Join(base, ".claude", name), nil
	case ScopeUser:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".claude", "settings.json"), nil
	default:
		return "", fmt.Errorf("unknown scope: %s", scope)
	}
}

// Settings represents a Claude Code settings.json file.
// We use map[string]interface{} to preserve unknown keys.
type Settings struct {
	raw map[string]interface{}
}

// ReadSettings reads and parses the settings file. Returns empty settings if file doesn't exist.
func ReadSettings(path string) (*Settings, error) {
	s := &Settings{raw: make(map[string]interface{})}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("reading settings file: %w", err)
	}

	if err := json.Unmarshal(data, &s.raw); err != nil {
		return nil, fmt.Errorf("parsing settings file: %w", err)
	}

	return s, nil
}

// Hooks returns the hooks map from settings, or empty map if none.
func (s *Settings) Hooks() map[string]interface{} {
	hooks, ok := s.raw["hooks"]
	if !ok {
		return make(map[string]interface{})
	}
	hooksMap, ok := hooks.(map[string]interface{})
	if !ok {
		return make(map[string]interface{})
	}
	return hooksMap
}

// setHooks sets the hooks map in the raw settings.
func (s *Settings) setHooks(hooks map[string]interface{}) {
	s.raw["hooks"] = hooks
}

// MergeHook adds a hook's event entries to the settings. For each event:
//   - If event doesn't exist, create it with the matchers.
//   - If event exists, append matchers (dedup by checksum).
//   - If force, replace any existing entries from the same slug first.
//
// The fragment is a map of event names to arrays of matcher objects. Each
// matcher object may contain a "_slug" field used for identification.
func (s *Settings) MergeHook(fragment map[string]interface{}, slug string, force bool) {
	hooks := s.Hooks()

	for eventName, newMatchers := range fragment {
		newMatchersList, ok := toSlice(newMatchers)
		if !ok {
			continue
		}

		existingMatchers, hasEvent := hooks[eventName]
		if !hasEvent {
			hooks[eventName] = newMatchersList
			continue
		}

		existingList, ok := toSlice(existingMatchers)
		if !ok {
			hooks[eventName] = newMatchersList
			continue
		}

		// If force, remove existing entries that came from this slug.
		if force {
			existingList = filterOutSlug(existingList, slug)
		}

		// Append new matchers, deduplicating by checksum of the matcher object.
		existingChecksums := make(map[string]bool)
		for _, m := range existingList {
			existingChecksums[matcherChecksum(m)] = true
		}

		for _, nm := range newMatchersList {
			cs := matcherChecksum(nm)
			if !existingChecksums[cs] {
				existingList = append(existingList, nm)
				existingChecksums[cs] = true
			}
		}

		hooks[eventName] = existingList
	}

	s.setHooks(hooks)
}

// RemoveHook removes all entries associated with a slug (using the tracker
// to identify them). It removes matcher entries whose "_slug" field matches
// the given slug and cleans up empty arrays. Returns true if any entries
// were removed.
func (s *Settings) RemoveHook(tracker *Tracker, slug string) bool {
	tracked := tracker.Get(slug)
	if tracked == nil {
		return false
	}

	hooks := s.Hooks()
	removed := false

	for _, eventName := range tracked.Events {
		matchers, ok := hooks[eventName]
		if !ok {
			continue
		}
		matcherList, ok := toSlice(matchers)
		if !ok {
			continue
		}

		filtered := filterOutSlug(matcherList, slug)
		if len(filtered) < len(matcherList) {
			removed = true
		}

		if len(filtered) == 0 {
			delete(hooks, eventName)
		} else {
			hooks[eventName] = filtered
		}
	}

	if removed {
		if len(hooks) == 0 {
			delete(s.raw, "hooks")
		} else {
			s.setHooks(hooks)
		}
	}

	return removed
}

// Write atomically writes the settings file (tmp + rename pattern).
func (s *Settings) Write(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(s.raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling settings: %w", err)
	}
	data = append(data, '\n')

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming temp file: %w", err)
	}

	return nil
}

// Checksum computes sha256 of the JSON-encoded event entries for a hook.
// Keys are sorted for deterministic output.
func Checksum(events map[string]interface{}) string {
	keys := make([]string, 0, len(events))
	for k := range events {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	ordered := make([]interface{}, 0, len(events)*2)
	for _, k := range keys {
		ordered = append(ordered, k, events[k])
	}

	data, err := json.Marshal(ordered)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

// matcherChecksum computes a checksum for a single matcher entry.
func matcherChecksum(m interface{}) string {
	data, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}

// toSlice converts an interface{} to []interface{} if possible.
func toSlice(v interface{}) ([]interface{}, bool) {
	s, ok := v.([]interface{})
	return s, ok
}

// filterOutSlug removes matcher entries that contain a "_slug" field
// matching the given slug.
func filterOutSlug(matchers []interface{}, slug string) []interface{} {
	result := make([]interface{}, 0, len(matchers))
	for _, m := range matchers {
		mMap, ok := m.(map[string]interface{})
		if !ok {
			result = append(result, m)
			continue
		}
		s, ok := mMap["_slug"].(string)
		if ok && s == slug {
			continue
		}
		result = append(result, m)
	}
	return result
}
