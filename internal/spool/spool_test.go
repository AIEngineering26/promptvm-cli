package spool

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AIEngineering26/promptvm-cli/internal/capture"
)

func newEntry(session, hash string) *Entry {
	return &Entry{
		ClaudeSessionID: session,
		WorkspaceID:     "ws_1",
		CaptureMode:     capture.ModeSummary,
		ContentHash:     hash,
		Payload:         &capture.IngestRequest{ClaudeSessionID: session, ContentHash: hash},
	}
}

func TestSpoolAddListRemovePermsAndSelfContained(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	path, err := Add(newEntry("sess1", "hashaaaa11112222"))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("spool file perms = %v, want 0600", fi.Mode().Perm())
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Errorf("spool dir perms = %v, want 0700", di.Mode().Perm())
	}

	entries, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("List len = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.Payload == nil || e.Payload.ClaudeSessionID != "sess1" {
		t.Errorf("entry not self-contained: %+v", e)
	}

	if err := e.Remove(); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	n, _ := Count()
	if n != 0 {
		t.Errorf("Count after remove = %d, want 0", n)
	}
}

func TestSpoolReSpoolSameKeyDoesNotDuplicate(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	if _, err := Add(newEntry("s", "hhhhhhhhhhhhhhhh")); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(newEntry("s", "hhhhhhhhhhhhhhhh")); err != nil {
		t.Fatal(err)
	}
	n, _ := Count()
	if n != 1 {
		t.Errorf("re-spool produced %d entries, want 1 (idempotent file name)", n)
	}
}

func TestSpoolPrunesExpired(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	e := newEntry("old", "expiredhashvalue0")
	e.CreatedAt = time.Now().Add(-TTL - time.Hour)
	if _, err := Add(e); err != nil {
		t.Fatal(err)
	}
	entries, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expired entry not pruned, got %d", len(entries))
	}
}

func TestSpoolPrunesPoison(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	e := newEntry("poison", "poisonhashvalue00")
	e.Attempts = MaxAttempts
	if _, err := Add(e); err != nil {
		t.Fatal(err)
	}
	entries, _ := List()
	if len(entries) != 0 {
		t.Errorf("poison entry not pruned, got %d", len(entries))
	}
}

func TestLedgerMarkHasSave(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	l, err := LoadLedger()
	if err != nil {
		t.Fatal(err)
	}
	if l.Has("x") {
		t.Errorf("fresh ledger should not have x")
	}
	l.Mark("x")
	if err := l.Save(); err != nil {
		t.Fatal(err)
	}
	l2, err := LoadLedger()
	if err != nil {
		t.Fatal(err)
	}
	if !l2.Has("x") {
		t.Errorf("ledger did not persist mark")
	}
}
