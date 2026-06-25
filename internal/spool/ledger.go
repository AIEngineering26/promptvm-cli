package spool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/AIEngineering26/promptvm-cli/internal/config"
)

// LedgerMax bounds the captured-session ledger so it cannot grow without limit.
const LedgerMax = 2000

// Ledger records which claude_session_ids have already been captured, so the
// SessionStart reconcile (which is transcript-driven, HOOK-1) can skip sessions
// already uploaded and only flush genuinely-missing ones.
type Ledger struct {
	Captured map[string]time.Time `json:"captured"`
	path     string
}

func ledgerPath() (string, error) {
	base, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "captured-sessions.json"), nil
}

// LoadLedger reads the ledger, returning an empty one if absent.
func LoadLedger() (*Ledger, error) {
	path, err := ledgerPath()
	if err != nil {
		return nil, err
	}
	l := &Ledger{Captured: map[string]time.Time{}, path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return l, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, l); err != nil {
		// Corrupt ledger: start fresh rather than failing a hook.
		return &Ledger{Captured: map[string]time.Time{}, path: path}, nil
	}
	if l.Captured == nil {
		l.Captured = map[string]time.Time{}
	}
	l.path = path
	return l, nil
}

// Has reports whether a session has already been captured.
func (l *Ledger) Has(sessionID string) bool {
	_, ok := l.Captured[sessionID]
	return ok
}

// Mark records a session as captured and trims the ledger to LedgerMax,
// dropping the oldest entries.
func (l *Ledger) Mark(sessionID string) {
	if l.Captured == nil {
		l.Captured = map[string]time.Time{}
	}
	l.Captured[sessionID] = time.Now().UTC()
	if len(l.Captured) > LedgerMax {
		l.trim()
	}
}

func (l *Ledger) trim() {
	type kv struct {
		id string
		t  time.Time
	}
	all := make([]kv, 0, len(l.Captured))
	for id, t := range l.Captured {
		all = append(all, kv{id, t})
	}
	// Sort oldest-first and drop the overflow.
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[j].t.Before(all[i].t) {
				all[i], all[j] = all[j], all[i]
			}
		}
	}
	drop := len(all) - LedgerMax
	for i := 0; i < drop; i++ {
		delete(l.Captured, all[i].id)
	}
}

// Save atomically writes the ledger to disk.
func (l *Ledger) Save() error {
	if l.path == "" {
		p, err := ledgerPath()
		if err != nil {
			return err
		}
		l.path = p
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, l.path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
