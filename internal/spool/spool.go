// Package spool persists capture payloads that could not be uploaded (network
// failure, offline) so they can be retried on the next SessionStart reconcile
// or `sync status`. Each entry is fully self-contained — it stores the RESOLVED
// target (workspace, directory, mode, payload, hash) so a later reconcile works
// even if the manifest changed (DX-7).
//
// Files are 0600 in a 0700 per-user dir, never repo-relative or world-readable
// (SEC-7). Entries are capped, TTL-bounded, and capped on attempts; flushed
// entries are removed.
package spool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/AIEngineering26/promptvm-cli/internal/capture"
	"github.com/AIEngineering26/promptvm-cli/internal/config"
)

const (
	// MaxEntries caps the spool so a persistent outage can't grow unbounded.
	MaxEntries = 200
	// TTL bounds how long an un-flushed entry survives.
	TTL = 14 * 24 * time.Hour
	// MaxAttempts bounds retries before an entry is dropped as poison.
	MaxAttempts = 8
)

// Entry is one spooled capture, self-contained for reconcile (DX-7).
type Entry struct {
	ClaudeSessionID string                 `json:"claudeSessionId"`
	WorkspaceID     string                 `json:"workspaceId"`
	DirectoryID     string                 `json:"directoryId,omitempty"`
	CaptureMode     capture.Mode           `json:"captureMode"`
	RepoURL         string                 `json:"repoUrl,omitempty"`
	ContentHash     string                 `json:"contentHash"`
	Payload         *capture.IngestRequest `json:"payload"`
	CreatedAt       time.Time              `json:"createdAt"`
	Attempts        int                    `json:"attempts"`

	// Reason records why the capture was spooled instead of uploaded (e.g.
	// "no capture credential stored", "upload failed: …", "manifest had no
	// workspace; used config default") so `sync status` / `sync doctor` can
	// explain a growing spool instead of failing silently.
	Reason string `json:"reason,omitempty"`

	// path is the on-disk file, set on load; not serialized.
	path string `json:"-"`
}

// Dir returns the 0700 spool directory under the CLI config dir.
func Dir() (string, error) {
	base, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "spool"), nil
}

// entryFileName derives a stable file name from the idempotency key so a
// re-spool of the same (session, hash) overwrites rather than duplicates.
func entryFileName(sessionID, contentHash string) string {
	safe := func(s string) string {
		return strings.Map(func(r rune) rune {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
				return r
			default:
				return '_'
			}
		}, s)
	}
	h := contentHash
	if len(h) > 16 {
		h = h[:16]
	}
	return fmt.Sprintf("%s-%s.json", safe(sessionID), safe(h))
}

// Add writes (or overwrites) a spool entry for the payload. CreatedAt is set if
// unset. Returns the file path.
func Add(e *Entry) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating spool dir: %w", err)
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	path := filepath.Join(dir, entryFileName(e.ClaudeSessionID, e.ContentHash))
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return "", err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return "", fmt.Errorf("writing spool entry: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("renaming spool entry: %w", err)
	}
	e.path = path
	return path, nil
}

// List returns all live spool entries, pruning expired or poison entries from
// disk as a side effect. Entries are returned oldest-first.
func List() ([]*Entry, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var entries []*Entry
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, f.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var e Entry
		if err := json.Unmarshal(data, &e); err != nil {
			// Unparseable: remove so it can't wedge the spool.
			os.Remove(path)
			continue
		}
		e.path = path
		if time.Since(e.CreatedAt) > TTL || e.Attempts >= MaxAttempts {
			os.Remove(path)
			continue
		}
		entries = append(entries, &e)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CreatedAt.Before(entries[j].CreatedAt)
	})

	// Enforce the cap by dropping the oldest beyond MaxEntries.
	if len(entries) > MaxEntries {
		for _, e := range entries[:len(entries)-MaxEntries] {
			os.Remove(e.path)
		}
		entries = entries[len(entries)-MaxEntries:]
	}
	return entries, nil
}

// Remove deletes the entry's on-disk file (used after a successful flush).
func (e *Entry) Remove() error {
	if e.path == "" {
		return nil
	}
	err := os.Remove(e.path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// MarkAttempt increments the attempt counter and rewrites the entry to disk so
// poison entries eventually age out via MaxAttempts.
func (e *Entry) MarkAttempt() error {
	e.Attempts++
	_, err := Add(e)
	return err
}

// Count returns the number of live spooled entries.
func Count() (int, error) {
	entries, err := List()
	if err != nil {
		return 0, err
	}
	return len(entries), nil
}
